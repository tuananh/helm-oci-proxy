package serve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"cloud.google.com/go/storage"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	ocitypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/tuananh/helm-oci-proxy/pkg/types"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/googleapi"
)

// GCSStorage implements the StorageBackend interface for Google Cloud Storage
type GCSStorage struct {
	bucket   string
	endpoint string
	client   *storage.Client
}

// New creates a new GCSStorage instance
func (s *GCSStorage) New(ctx context.Context, config types.StorageConfig) (StorageBackend, error) {
	// Set default endpoint if not specified
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://storage.googleapis.com"
	}

	// Validate bucket name
	if config.Bucket == "" {
		return nil, fmt.Errorf("bucket name is required for GCS storage")
	}

	// Initialize GCS client
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCS client: %w", err)
	}

	// Return a new GCSStorage instance
	return &GCSStorage{
		bucket:   config.Bucket,
		endpoint: endpoint,
		client:   client,
	}, nil
}

// Blob redirects to the blob in GCS
func (s *GCSStorage) Blob(w http.ResponseWriter, r *http.Request, name string) {
	url := fmt.Sprintf("https://storage.googleapis.com/%s/blobs/%s", s.bucket, name)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// BlobExists checks if a blob exists in GCS
func (s *GCSStorage) BlobExists(ctx context.Context, name string) (v1.Descriptor, error) {
	obj, err := s.client.Bucket(s.bucket).Object(fmt.Sprintf("blobs/%s", name)).Attrs(ctx)
	if err != nil {
		return v1.Descriptor{}, err
	}
	var h v1.Hash
	if d := obj.Metadata["Docker-Content-Digest"]; d != "" {
		h, err = v1.NewHash(d)
		if err != nil {
			return v1.Descriptor{}, err
		}
	}

	return v1.Descriptor{
		Digest:    h,
		MediaType: ocitypes.MediaType(obj.ContentType),
		Size:      obj.Size,
	}, nil
}

// WriteObject writes a string object to GCS
func (s *GCSStorage) WriteObject(ctx context.Context, name, contents string) error {
	w := s.client.Bucket(s.bucket).Object(fmt.Sprintf("blobs/%s", name)).
		If(storage.Conditions{DoesNotExist: true}).
		NewWriter(ctx)
	if _, err := fmt.Fprintln(w, contents); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("fmt.Fprintln: %v", err)
	}
	if err := w.Close(); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("w.Close: %v", err)
	}
	return nil
}

// WriteBlob writes a blob to GCS
func (s *GCSStorage) WriteBlob(ctx context.Context, name string, h v1.Hash, rc io.ReadCloser, contentType string) error {
	start := time.Now()
	defer func() { slog.InfoContext(ctx, "gcsWriteBlob(%q) took %s", name, time.Since(start)) }()

	// The DoesNotExist precondition can be hit when writing or flushing
	// data, which can happen any of three places. Anywhere it happens,
	// just ignore the error since that means the blob already exists.
	w := s.client.Bucket(s.bucket).Object(fmt.Sprintf("blobs/%s", name)).
		If(storage.Conditions{DoesNotExist: true}).
		NewWriter(ctx)
	w.ObjectAttrs.ContentType = contentType
	w.Metadata = map[string]string{"Docker-Content-Digest": h.String()}

	if _, err := io.Copy(w, rc); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("copy: %v", err)
	}
	if err := rc.Close(); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("rc.Close: %v", err)
	}
	if err := w.Close(); err != nil {
		if herr, ok := err.(*googleapi.Error); ok && herr.Code == http.StatusPreconditionFailed {
			return nil
		}
		return fmt.Errorf("w.Close: %v", err)
	}
	return nil
}

// WriteImage writes the layer blobs, config blob and manifest to GCS
func (s *GCSStorage) WriteImage(ctx context.Context, img v1.Image, also ...string) error {
	// Write config blob for later serving.
	ch, err := img.ConfigName()
	if err != nil {
		return err
	}
	cb, err := img.RawConfigFile()
	if err != nil {
		return err
	}
	if err := s.WriteBlob(ctx, ch.String(), ch, io.NopCloser(bytes.NewReader(cb)), "application/json"); err != nil {
		return err
	}

	// Write layer blobs for later serving.
	layers, err := img.Layers()
	if err != nil {
		return err
	}
	var g errgroup.Group
	for _, l := range layers {
		l := l
		g.Go(func() error {
			rc, err := l.Compressed()
			if err != nil {
				return err
			}
			lh, err := l.Digest()
			if err != nil {
				return err
			}
			mt, err := l.MediaType()
			if err != nil {
				return err
			}
			return s.WriteBlob(ctx, lh.String(), lh, rc, string(mt))
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Write the manifest as a blob.
	b, err := img.RawManifest()
	if err != nil {
		return err
	}
	mt, err := img.MediaType()
	if err != nil {
		return err
	}
	digest, err := img.Digest()
	if err != nil {
		return err
	}
	if err := s.WriteBlob(ctx, digest.String(), digest, io.NopCloser(bytes.NewReader(b)), string(mt)); err != nil {
		return err
	}
	for _, a := range also {
		a := a
		g.Go(func() error {
			return s.WriteBlob(ctx, a, digest, io.NopCloser(bytes.NewReader(b)), string(mt))
		})
	}
	return g.Wait()
}

// ServeManifest writes config and layer blobs for the image, then writes and
// redirects to the image manifest contents pointing to those blobs.
func (s *GCSStorage) ServeManifest(w http.ResponseWriter, r *http.Request, img v1.Image, also ...string) error {
	ctx := r.Context()

	if err := s.WriteImage(ctx, img, also...); err != nil {
		return err
	}

	digest, err := img.Digest()
	if err != nil {
		return err
	}

	// If it's just a HEAD request, serve that.
	if r.Method == http.MethodHead {
		mt, err := img.MediaType()
		if err != nil {
			return err
		}
		size, err := img.Size()
		if err != nil {
			return err
		}
		w.Header().Set("Docker-Content-Digest", digest.String())
		w.Header().Set("Content-Type", string(mt))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", size))
		return nil
	}

	// Redirect to manifest blob.
	s.Blob(w, r, digest.String())
	return nil
}
