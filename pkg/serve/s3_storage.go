package serve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	ocitypes "github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/tuananh/helm-oci-proxy/pkg/types"
	"golang.org/x/sync/errgroup"
)

// S3Storage implements the StorageBackend interface for S3
type S3Storage struct {
	bucket   string
	endpoint string
	region   string
	client   *s3.S3
}

// New creates a new S3Storage instance
func (s *S3Storage) New(ctx context.Context, config types.StorageConfig) (StorageBackend, error) {
	region := config.Region
	if region == "" {
		region = "us-east-1"
	}

	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", config.Region)
	}

	if config.Bucket == "" {
		return nil, fmt.Errorf("bucket name is required for S3 storage")
	}

	awsConfig := &aws.Config{
		Region: aws.String(region),
	}

	// If custom endpoint is provided, use it
	if endpoint != "" && endpoint != fmt.Sprintf("https://s3.%s.amazonaws.com", region) {
		awsConfig.Endpoint = aws.String(endpoint)
		// For custom endpoints like MinIO, we need to disable S3ForcePathStyle
		awsConfig.S3ForcePathStyle = aws.Bool(true)
	}

	sess, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	s3Client := s3.New(sess)

	return &S3Storage{
		bucket:   config.Bucket,
		endpoint: endpoint,
		region:   region,
		client:   s3Client,
	}, nil
}

// Blob redirects to the blob in S3
func (s *S3Storage) Blob(w http.ResponseWriter, r *http.Request, name string) {
	var url string
	if s.endpoint != "" {
		url = fmt.Sprintf("%s/%s/blobs/%s", s.endpoint, s.bucket, name)
	} else {
		url = fmt.Sprintf("https://%s.s3.%s.amazonaws.com/blobs/%s", s.bucket, s.region, name)
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// BlobExists checks if a blob exists in S3
func (s *S3Storage) BlobExists(ctx context.Context, name string) (v1.Descriptor, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fmt.Sprintf("blobs/%s", name)),
	}

	result, err := s.client.HeadObjectWithContext(ctx, input)
	if err != nil {
		return v1.Descriptor{}, err
	}

	var h v1.Hash
	if d, ok := result.Metadata["Docker-Content-Digest"]; ok && d != nil {
		h, err = v1.NewHash(*d)
		if err != nil {
			return v1.Descriptor{}, err
		}
	}

	return v1.Descriptor{
		Digest:    h,
		MediaType: ocitypes.MediaType(*result.ContentType),
		Size:      *result.ContentLength,
	}, nil
}

// WriteObject writes a string object to S3
func (s *S3Storage) WriteObject(ctx context.Context, name, contents string) error {
	// Check if object already exists
	_, err := s.client.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fmt.Sprintf("blobs/%s", name)),
	})
	if err == nil {
		// Object already exists, no need to write
		return nil
	}

	// Write the object
	_, err = s.client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(fmt.Sprintf("blobs/%s", name)),
		Body:        bytes.NewReader([]byte(contents + "\n")),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		return fmt.Errorf("failed to write object: %v", err)
	}
	return nil
}

// WriteBlob writes a blob to S3
func (s *S3Storage) WriteBlob(ctx context.Context, name string, h v1.Hash, rc io.ReadCloser, contentType string) error {
	start := time.Now()
	defer func() { slog.InfoContext(ctx, "s3WriteBlob(%q) took %s", name, time.Since(start)) }()

	// Check if object already exists
	_, err := s.client.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fmt.Sprintf("blobs/%s", name)),
	})
	if err == nil {
		// Object already exists, no need to write
		return nil
	}

	// Read the entire content
	data, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("failed to read content: %v", err)
	}
	if err := rc.Close(); err != nil {
		return fmt.Errorf("rc.Close: %v", err)
	}

	// Write the object
	_, err = s.client.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(fmt.Sprintf("blobs/%s", name)),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
		Metadata: map[string]*string{
			"Docker-Content-Digest": aws.String(h.String()),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to write blob: %v", err)
	}
	return nil
}

// WriteImage writes the layer blobs, config blob and manifest to S3
func (s *S3Storage) WriteImage(ctx context.Context, img v1.Image, also ...string) error {
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
func (s *S3Storage) ServeManifest(w http.ResponseWriter, r *http.Request, img v1.Image, also ...string) error {
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
