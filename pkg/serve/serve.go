package serve

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/tuananh/helm-oci-proxy/pkg/types"
)

// For backward compatibility
func Blob(w http.ResponseWriter, r *http.Request, name string) {
	bucket := os.Getenv("BUCKET")
	url := fmt.Sprintf("https://storage.googleapis.com/%s/blobs/%s", bucket, name)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

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
