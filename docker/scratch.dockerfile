# Multi-stage build: builds one of the ./cmd/<name> binaries as a static Linux executable
# and packages it into a minimal scratch image.
#
# Usage:
#   docker build -f docker/scratch.dockerfile --build-arg CMD=wg-feed-server -t wg-feed-server:local .
#
# NOTE: scratch images contain only the wg-feed binary + CA certs.

ARG GO_VERSION=1.25

FROM golang:${GO_VERSION}-alpine AS builder

ARG CMD=wg-feed-server
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

# Needed to copy CA certs into scratch for HTTPS clients.
RUN apk add --no-cache ca-certificates

# Cache deps
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -tags netgo,osusergo -ldflags "-s -w" \
      -o /out/app ./cmd/${CMD}

FROM scratch

# CA cert bundle for HTTPS.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /out/app /app

# Run as non-root (numeric UID works in scratch).
USER 65532:65532

ENTRYPOINT ["/app"]
