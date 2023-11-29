package main

import (
	"bytes"
	"context"
	"crypto/md5"
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
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/tuananh/helm-oci-proxy/pkg/serve"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/registry"
	"k8s.io/apimachinery/pkg/util/json"
)

func main() {
	ctx := context.Background()

	repoURL := os.Getenv("REPO_URL")
	if repoURL == "" {
		slog.ErrorContext(ctx, "Missing REPO_URL env variable")
		return
	}

	st, err := serve.NewStorage(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "serve.NewStorage", "err", err)
		os.Exit(1)
	}

	http.Handle("/v2/", &server{
		info:    log.New(os.Stdout, "I ", log.Ldate|log.Ltime|log.Lshortfile),
		error:   log.New(os.Stderr, "E ", log.Ldate|log.Ltime|log.Lshortfile),
		storage: st,
	})
	http.Handle("/", http.RedirectHandler("https://github.com/tuananh/oci-helm-proxy", http.StatusSeeOther))

	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
		slog.InfoContext(ctx, "Defaulting port", "port", port)
	}
	slog.InfoContext(ctx, "Listening...", "port", port)
	slog.InfoContext(ctx, "Proxy Helm repo:", "repoURL", repoURL)
	slog.ErrorContext(ctx, "ListenAndServe", "err", http.ListenAndServe(fmt.Sprintf(":%s", port), nil))
}

type server struct {
	info, error *log.Logger
	storage     *serve.Storage
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
	cacheKey := []string{chartName, tagOrDigest}
	ck := makeCacheKey(cacheKey)

	// Check if we've already got a manifest for this chart
	if _, err := s.storage.BlobExists(ctx, ck); err == nil {
		slog.InfoContext(ctx, "serving cached manifest:", "cacheKey", ck)
		serve.Blob(w, r, ck)
		return
	}

	// Build the OCI helm chart
	img, err := s.build(ctx, chartName, tagOrDigest)
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
func (s *server) build(ctx context.Context, chartName string, chartVersion string) (v1.Image, error) {
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
			"pull", "argo-cd/argo-cd", "--version", chartVersion,
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
	v1Image = mutate.MediaType(v1Image, types.OCIManifestSchema1)

	slog.InfoContext(ctx, "build OCI helm chart completed")
	return v1Image, nil
}

func makeCacheKey(keys []string) string {
	ck := []byte(strings.Join(keys, ","))
	return fmt.Sprintf("helm-oci-proxy-%x", md5.Sum(ck))
}
