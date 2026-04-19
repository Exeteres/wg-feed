# Build from prebuilt GoReleaser binaries and package into a minimal scratch image.
#
# Usage:
#   docker build -f docker/scratch.dockerfile --build-arg BINARY_PREFIX=dist/wg-feed-server_0.5.1_linux -t wg-feed-server:local .
#
# NOTE: scratch images contain only the wg-feed binary + CA certs.

FROM alpine:3.20 AS certs

RUN apk add --no-cache ca-certificates

FROM scratch

ARG BINARY_PREFIX
ARG TARGETARCH

# CA cert bundle for HTTPS.
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY ${BINARY_PREFIX}_${TARGETARCH} /app

# Run as non-root (numeric UID works in scratch).
USER 65532:65532

ENTRYPOINT ["/app"]
