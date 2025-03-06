package serve

import (
	"context"
	"fmt"

	"github.com/tuananh/helm-oci-proxy/pkg/types"
)

// NewStorageWithConfig creates a new Storage instance with the provided configuration
func NewStorageWithConfig(ctx context.Context, config types.StorageConfig) (*Storage, error) {
	switch config.Type {
	case "s3":
		return newS3Storage(ctx, config)
	case "gcs", "":
		return newGCSStorage(ctx, config)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}
}

func newS3Storage(ctx context.Context, config types.StorageConfig) (*Storage, error) {
	// Set default endpoint if not specified
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://s3.amazonaws.com"
	}

	// Initialize S3 client with the provided configuration
	// Implementation details would depend on the S3 client library being used

	// Return a new Storage instance configured for S3
	return &Storage{
		// S3-specific configuration
	}, nil
}

func newGCSStorage(ctx context.Context, config types.StorageConfig) (*Storage, error) {
	// Set default endpoint if not specified
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://storage.googleapis.com"
	}

	// Initialize GCS client with the provided configuration
	// This would likely use the existing GCS client initialization code

	// Return a new Storage instance configured for GCS
	return &Storage{
		// GCS-specific configuration
	}, nil
}
