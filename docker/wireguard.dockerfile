# Multi-stage build: builds one of the ./cmd/<name> binaries and packages it into
# a small Alpine image that includes wireguard-tools + iproute2.
#
# Usage:
#   docker build -f docker/wireguard.dockerfile --build-arg CMD=wg-feed-daemon -t wg-feed-daemon:local .

ARG GO_VERSION=1.25

FROM golang:${GO_VERSION}-alpine AS builder

ARG CMD=wg-feed-daemon
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

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

FROM alpine:3.20

RUN apk add --no-cache ca-certificates iproute2 wireguard-tools

COPY --from=builder /out/app /usr/local/bin/app

# Keep consistent non-root user with scratch image (65532).
USER 65532:65532

ENTRYPOINT ["/usr/local/bin/app"]
