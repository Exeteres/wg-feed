# Build from prebuilt GoReleaser binaries and package into a small Alpine image
# that includes wireguard-tools, iproute2, iptables, and iputils.
#
# Usage:
#   docker build -f docker/wireguard.dockerfile --build-arg BINARY_PREFIX=dist/wg-feed-daemon_0.5.1_linux -t wg-feed-daemon:local .

FROM alpine:3.20

ARG BINARY_PREFIX
ARG TARGETARCH

RUN apk add --no-cache ca-certificates curl iproute2 iptables iputils wireguard-tools

# Install AmneziaWG tools
RUN curl https://github.com/amnezia-vpn/amneziawg-tools/releases/download/v1.0.20260223/alpine-3.19-amneziawg-tools.zip -L -o /tmp/amneziawg-tools.zip && \
  unzip /tmp/amneziawg-tools.zip -d /tmp/amneziawg-tools && \
  ls /tmp/amneziawg-tools && \
  mv /tmp/amneziawg-tools/alpine-3.19-amneziawg-tools/awg /usr/local/bin/ && \
  mv /tmp/amneziawg-tools/alpine-3.19-amneziawg-tools/awg-quick /usr/local/bin/ && \
  chmod +x /usr/local/bin/awg /usr/local/bin/awg-quick && \
  rm -rf /tmp/amneziawg-tools /tmp/amneziawg-tools.zip

COPY ${BINARY_PREFIX}_${TARGETARCH} /usr/local/bin/app

# Keep consistent non-root user with scratch image (65532).
USER 65532:65532

ENTRYPOINT ["/usr/local/bin/app"]
