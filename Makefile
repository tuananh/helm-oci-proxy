SOURCE_DIRS = cmd pkg/serve pkg/types

# gcs bucket
.PHONY: all
all: gofmt build

.PHONY: build
build:
	go build -o bin/helm-oci-proxy ./cmd

.PHONY: run
run: clean build
	./bin/helm-oci-proxy -config config.yaml.example

.PHONY: gofmt
gofmt:
	@gofmt -l -s ${SOURCE_DIRS} ./

.PHONY: clean
clean:
	@rm -f bin/go-clean-proxy