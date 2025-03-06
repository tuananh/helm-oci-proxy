package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	execute "github.com/alexellis/go-execute/v2"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	v1tar "github.com/google/go-containerregistry/pkg/v1/tarball"

	ocitypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/tuananh/helm-oci-proxy/pkg/serve"
	"github.com/tuananh/helm-oci-proxy/pkg/types"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/registry"
	"k8s.io/apimachinery/pkg/util/json"
)

func main() {
	ctx := context.Background()

	// Define command line flags
	configFile := flag.String("config", "", "Path to config file")
	flag.Parse()

	// Initialize default config
	config := types.Config{
		Port:  "5000",
		Repos: []types.RepoConfig{},
		Storage: types.StorageConfig{
			Type: "gcs", // Default to GCS for backward compatibility
		},
	}

	// Load config from file if provided
	if *configFile != "" {
		if err := loadConfig(*configFile, &config); err != nil {
			slog.ErrorContext(ctx, "Failed to load config file", "err", err)
			os.Exit(1)
		}
		slog.InfoContext(ctx, "Loaded configuration from file", "path", *configFile)
	} else {
		slog.InfoContext(ctx, "No config file provided, using environment variables")

		// For backward compatibility, check environment variables
		if bucket := os.Getenv("BUCKET"); bucket != "" {
			config.Storage.Bucket = bucket
		}

		if repoURL := os.Getenv("REPO_URL"); repoURL != "" {
			config.Repos = append(config.Repos, types.RepoConfig{URL: repoURL, Prefix: ""})
			slog.InfoContext(ctx, "Added repo from environment variable", "repoURL", repoURL)
		}

		if port := os.Getenv("PORT"); port != "" {
			config.Port = port
		}
	}

	// Validate required configuration
	if len(config.Repos) == 0 {
		slog.ErrorContext(ctx, "No repositories configured in config file or environment")
		os.Exit(1)
	}

	// Initialize storage based on configuration
	st, err := serve.NewStorageWithConfig(ctx, config.Storage)
	if err != nil {
		slog.ErrorContext(ctx, "serve.NewStorageWithConfig", "err", err)
		os.Exit(1)
	}

	http.Handle("/v2/", &server{
		info:    log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error:   log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
		storage: st,
		config:  config,
	})
	http.Handle("/", http.RedirectHandler("https://github.com/tuananh/oci-helm-proxy", http.StatusSeeOther))

	slog.InfoContext(ctx, "Listening...", "port", config.Port)
	slog.InfoContext(ctx, "Proxy Helm repo:", "repos", config.Repos)
	slog.InfoContext(ctx, "Storage configuration:", "type", config.Storage.Type, "bucket", config.Storage.Bucket)
	slog.ErrorContext(ctx, "ListenAndServe", "err", http.ListenAndServe(fmt.Sprintf(":%s", config.Port), nil))
}

// loadConfig loads configuration from a YAML file
func loadConfig(filePath string, config *types.Config) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

type server struct {
	info, error *log.Logger
	storage     serve.StorageBackend
	config      types.Config
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.String(), "/v2/")

	switch {
	case path == "":
		// API Version check.
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		return
	case strings.Contains(path, "/blobs/"),
		strings.Contains(path, "/manifests/sha256:"):
		// Extract requested blob digest and redirect to serve it from GCS.
		// If it doesn't exist, this will return 404.
		parts := strings.Split(r.URL.Path, "/")
		digest := parts[len(parts)-1]
		serve.Blob(w, r, digest)
	case strings.Contains(path, "/manifests/"):
		s.serveHelmManifest(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (s *server) serveHelmManifest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	path := strings.TrimPrefix(r.URL.Path, "/v2/")
	parts := strings.Split(path, "/")

	tagOrDigest := parts[len(parts)-1]
	slog.InfoContext(ctx, "serveHelmManifest", "method", r.Method, "URL", r.URL, "parts", parts, "tagOrDigest", tagOrDigest)

	// If request is for image by digest, try to serve it from GCS.
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		desc, err := s.storage.BlobExists(ctx, tagOrDigest)
		if err != nil {
			slog.ErrorContext(ctx, "storage.BlobExists", err)
			serve.Error(w, serve.ErrNotFound)
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Docker-Content-Digest", tagOrDigest)
			w.Header().Set("Content-Type", string(desc.MediaType))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", desc.Size))
			return
		}
		serve.Blob(w, r, tagOrDigest)
		return
	}

	chartName := parts[0]

	// Find the appropriate repo based on prefix
	var repoURL string
	for _, repo := range s.config.Repos {
		if strings.HasPrefix(chartName, repo.Prefix) {
			repoURL = repo.URL
			// If prefix is not empty, remove it from the chart name
			if repo.Prefix != "" {
				chartName = strings.TrimPrefix(chartName, repo.Prefix)
				// Remove leading slash if present
				chartName = strings.TrimPrefix(chartName, "/")
			}
			break
		}
	}

	if repoURL == "" {
		slog.ErrorContext(ctx, "No matching repository found for chart", "chartName", chartName)
		serve.Error(w, serve.ErrNotFound)
		return
	}

	cacheKey := []string{chartName, tagOrDigest}
	ck := makeCacheKey(cacheKey)

	// Check if we've already got a manifest for this chart
	if _, err := s.storage.BlobExists(ctx, ck); err == nil {
		slog.InfoContext(ctx, "serving cached manifest:", "cacheKey", ck)
		serve.Blob(w, r, ck)
		return
	}

	// Build the OCI helm chart
	img, err := s.build(ctx, repoURL, chartName, tagOrDigest)
	if err != nil {
		slog.ErrorContext(ctx, "build: ", "err", err)
		serve.Error(w, err)
		return
	}

	if err := s.storage.ServeManifest(w, r, img, ck); err != nil {
		slog.ErrorContext(ctx, "storage.ServeManifest:", "err", err)
		serve.Error(w, err)
	}
}

// Download the Helm chart and package it into v1.Image
func (s *server) build(ctx context.Context, repoURL string, chartName string, chartVersion string) (v1.Image, error) {
	wd, err := os.MkdirTemp("", "helm-oci-proxy-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create working directory: %w", err)
	}
	// defer os.RemoveAll(wd)

	// TODO: ugly but using Helm downloader is way way too complicated.
	// however, I'm probably going to remove this anyway since I dont want to depend on Helm CLI
	task := execute.ExecTask{
		Command: "helm",
		Args: []string{
			"pull", fmt.Sprintf("%s/%s", repoURL, chartName), "--version", chartVersion,
			"--destination", wd,
		},
		Env:         os.Environ(),
		StreamStdio: true,
	}
	_, err = task.Execute(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to download helm chart: %w", err)
	}

	path := path.Join(wd, fmt.Sprintf("argo-cd-%s.tgz", chartVersion))

	chartBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read chart from file: %w", err)
	}

	ch, err := loader.LoadArchive(bytes.NewReader(chartBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to load chart: %w", err)
	}

	configData, err := json.Marshal(ch.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal chart metadata: %w", err)
	}

	// we create 2 layers: config & chart layer content
	v1Layer, err := v1tar.LayerFromFile(path, v1tar.WithMediaType(registry.ChartLayerMediaType))
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI layer from .tgz: %w", err)
	}

	configLayer := static.NewLayer(configData, registry.ConfigMediaType)
	adds := make([]mutate.Addendum, 0, 2)
	adds = append(adds, mutate.Addendum{
		Layer: configLayer,
		History: v1.History{
			Author:    "Tuan Anh Tran <me@tuananh.org>",
			Comment:   "Proxied via github.com/tuananh/oci-helm-proxy",
			CreatedBy: "github.com/tuananh/oci-helm-proxy",
			Created:   v1.Time{Time: time.Time{}},
		},
	})
	adds = append(adds, mutate.Addendum{
		Layer: v1Layer,
		History: v1.History{
			Author:    "Tuan Anh Tran <me@tuananh.org>",
			Comment:   "Proxied via github.com/tuananh/oci-helm-proxy",
			CreatedBy: "github.com/tuananh/oci-helm-proxy",
			Created:   v1.Time{Time: time.Time{}},
		},
	})

	v1Image, err := mutate.Append(empty.Image, adds...)
	if err != nil {
		return empty.Image, fmt.Errorf("unable to append OCI layer to empty image: %w", err)
	}

	v1Image = mutate.ConfigMediaType(v1Image, registry.ConfigMediaType)
	v1Image = mutate.MediaType(v1Image, ocitypes.OCIManifestSchema1)

	slog.InfoContext(ctx, "build OCI helm chart completed")
	return v1Image, nil
}

func makeCacheKey(keys []string) string {
	ck := []byte(strings.Join(keys, ","))
	return fmt.Sprintf("helm-oci-proxy-%x", md5.Sum(ck))
}
