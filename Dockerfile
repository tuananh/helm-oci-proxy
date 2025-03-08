FROM --platform=$BUILDPLATFORM cgr.dev/chainguard/go:latest-dev AS builder

ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-w -s" -o helm-oci-proxy ./cmd

# Final stage
FROM cgr.dev/chainguard/static:latest

WORKDIR /app
COPY --from=builder /app/helm-oci-proxy /app

EXPOSE 5000

ENTRYPOINT ["/app/helm-oci-proxy"]