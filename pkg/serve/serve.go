package serve

import (
	"context"
	"fmt"
	"os"

	"github.com/tuananh/helm-oci-proxy/pkg/types"
)

// NewStorage creates a storage client using environment variables (legacy method)
func NewStorage(ctx context.Context) (StorageBackend, error) {
	bucket := os.Getenv("BUCKET")
	if bucket == "" {
		return nil, fmt.Errorf("BUCKET environment variable is not set")
	}

	// Use the new storage interface
	return NewStorageWithConfig(ctx, types.StorageConfig{
		Type:   "gcs",
		Bucket: bucket,
	})
}
