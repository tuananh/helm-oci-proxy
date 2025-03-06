package serve

import (
	"context"
	"fmt"
	"io"
	"net/http"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/tuananh/helm-oci-proxy/pkg/types"
)

// StorageBackend defines the interface for storage operations
type StorageBackend interface {
	// New creates a new storage instance
	New(ctx context.Context, config types.StorageConfig) (StorageBackend, error)

	// Blob redirects to the blob in the storage
	Blob(w http.ResponseWriter, r *http.Request, name string)

	// BlobExists checks if a blob exists in the storage
	BlobExists(ctx context.Context, name string) (v1.Descriptor, error)

	// WriteObject writes a string object to storage
	WriteObject(ctx context.Context, name, contents string) error

	// WriteBlob writes a blob to storage
	WriteBlob(ctx context.Context, name string, h v1.Hash, rc io.ReadCloser, contentType string) error

	// WriteImage writes the layer blobs, config blob and manifest
	WriteImage(ctx context.Context, img v1.Image, also ...string) error

	// ServeManifest writes config and layer blobs for the image, then writes and
	// redirects to the image manifest contents pointing to those blobs
	ServeManifest(w http.ResponseWriter, r *http.Request, img v1.Image, also ...string) error
}

// NewStorageWithConfig creates a new Storage instance with the provided configuration
func NewStorageWithConfig(ctx context.Context, config types.StorageConfig) (StorageBackend, error) {
	switch config.Type {
	case "s3":
		s3Storage := &S3Storage{}
		return s3Storage.New(ctx, config)
	case "gcs":
		gcsStorage := &GCSStorage{}
		return gcsStorage.New(ctx, config)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}
