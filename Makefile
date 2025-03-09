SOURCE_DIRS = cmd pkg/serve pkg/types pkg/helm

# gcs bucket
.PHONY: all
all: gofmt pre-commit build

.PHONY: build
build:
	go build -o bin/helm-oci-proxy ./cmd

.PHONY: run/s3
run/s3: clean build
	./bin/helm-oci-proxy -config example/s3.yaml

.PHONY: run/gcs
run/gcs: clean build
	./bin/helm-oci-proxy -config example/gcs.yaml

.PHONY: gofmt
gofmt:
	@gofmt -l -s ${SOURCE_DIRS} ./

.PHONY: pre-commit
pre-commit:
	@pre-commit run --all-files

.PHONY: clean
clean:
	@rm -f bin/go-clean-proxy
