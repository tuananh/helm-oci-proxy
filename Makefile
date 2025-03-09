SOURCE_DIRS = cmd pkg/serve pkg/types pkg/helm

.PHONY: build
build:
	go build -o bin/helm-oci-proxy ./cmd

.PHONY: build/wasm
build/wasm:
	GOOS=wasip1 GOARCH=wasm go build -o bin/helm-oci-proxy.wasm ./cmd

.PHONY: run/s3
run/s3: clean build
	./bin/helm-oci-proxy -config example/s3.yaml

.PHONY: run/gcs
run/gcs: clean build
	./bin/helm-oci-proxy -config example/gcs.yaml

.PHONY: fmt
fmt:
	@gofmt -l -s ${SOURCE_DIRS} ./
	@pre-commit run --all-files

.PHONY: clean
clean:
	@rm -f bin/go-clean-proxy
	@rm -f bin/helm-oci-proxy.wasm
