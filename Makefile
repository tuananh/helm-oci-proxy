SOURCE_DIRS = cmd pkg/serve
export GO111MODULE=on

# gcs bucket
BUCKET_NAME=test-bucket-anhtt109
.PHONY: all
all: gofmt build

.PHONY: build
build:
	go build -o bin/helm-oci-proxy ./cmd

# test with argo-cd helm repo
.PHONY: run
run: clean build
	BUCKET=$(BUCKET_NAME) REPO_URL=https://argoproj.github.io/argo-helm ./bin/helm-oci-proxy

.PHONY: gofmt
gofmt:
	@gofmt -l -s ${SOURCE_DIRS} ./

.PHONY: clean
clean:
	@rm -f bin/go-clean-proxy