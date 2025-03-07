# Development

## Setup MinIO (S3) backend

```bash
docker compose up -d
```

Setup env var for AWS credentials (MinIO)   

```bash
# AWS credentials
export AWS_ACCESS_KEY_ID=testkey
export AWS_SECRET_ACCESS_KEY=testsecret

# AWS S3 endpoint configuration
export AWS_ENDPOINT=http://127.0.0.1:8333
export AWS_REGION=us-east-1  # Can be any region, doesn't matter for MinIO
export AWS_BUCKET=test-bucket

# Force path-style addressing (required for MinIO)
export AWS_S3_FORCE_PATH_STYLE=true
```

## Run Helm OCI Proxy

```bash
./bin/helm-oci-proxy -config s3.yaml
```