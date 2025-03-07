FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG BUILDPLATFORM
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-w -s" -o bin/helm-oci-proxy ./cmd

# Final stage
FROM --platform=$TARGETPLATFORM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/bin/helm-oci-proxy /app/bin/

EXPOSE 5000

ENTRYPOINT ["/app/bin/helm-oci-proxy"]