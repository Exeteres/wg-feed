# wg-feed

wg-feed is a draft protocol and reference implementation for distributing WireGuard tunnel configurations via subscription URLs.

The core design goal is to let clients fetch a JSON “feed document” over HTTPS and reconcile locally-managed tunnels (create/update/remove) to match, while keeping the tunnel payload as raw `wg-quick` configuration text to preserve client-specific extensions.

## Protocol

- Draft spec: [docs/draft-wg-feed-00.md](docs/draft-wg-feed-00.md)
- JSON Schema: [docs/wg-feed.schema.json](docs/wg-feed.schema.json)
- Example documents: [docs/examples](docs/examples)

## Architecture

The most complex deployment looks like this:

![Architecture Diagram](docs/assets/arch-light.svg#gh-light-mode-only)
![Architecture Diagram](docs/assets/arch-dark.svg#gh-dark-mode-only)

The SSE and encryption features are optional.

## Repository Layout

| Path                                           | Description                                                         |
| ---------------------------------------------- | ------------------------------------------------------------------- |
| [cmd/wg-feed-server](cmd/wg-feed-server)       | HTTP server backed by etcd.                                         |
| [cmd/wg-feed-upload](cmd/wg-feed-upload)       | Upload helper: computes `revision` and writes feed entries to etcd. |
| [cmd/wg-feed-apply](cmd/wg-feed-apply)         | One-shot client: fetch + reconcile/apply once.                      |
| [cmd/wg-feed-daemon](cmd/wg-feed-daemon)       | Long-running client: sync + reconcile over time.                    |
| [cmd/wg-feed-installer](cmd/wg-feed-installer) | Interactive installer/updater for daemon config and service setup.  |
| [docs](docs)                                   | Draft spec, JSON schema, and examples.                              |
| [internal](internal)                           | Shared Go packages (not a public API).                              |

## Interactive Installer

This repository provides an interactive installer to configure and install `wg-feed-daemon`.

Linux (systemd):

```bash
curl -fsSL https://raw.githubusercontent.com/exeteres/wg-feed/main/scripts/install.sh | bash
```

Windows (run in PowerShell):

```powershell
irm https://raw.githubusercontent.com/exeteres/wg-feed/main/scripts/install.ps1 | iex
```

## Nix / NixOS

This repo provides a flake that builds all four binaries, exports a NetworkManager AmneziaWG plugin package, and a NixOS module for running `wg-feed-daemon` as a systemd service.

See [nixos/README.md](nixos/README.md).

## Artifact Verification

### Binaries

Release checksums are signed with Sigstore cosign during the release workflow.
In addition, GitHub artifact attestations are published for Linux release binaries
for both provenance and SBOM.

To verify release checksums and attestations, use the following commands:

```bash
VERSION=0.5.4
BASE=https://github.com/exeteres/wg-feed/releases/download/v${VERSION}
BIN=wg-feed-daemon_${VERSION}_linux_amd64

# Download checksums and their signature + certificate for verification
curl -fsSLO ${BASE}/checksums.txt
curl -fsSLO ${BASE}/checksums.txt.sig
curl -fsSLO ${BASE}/checksums.txt.pem

# Verify checksum signature and certificate
cosign verify-blob \
	--certificate checksums.txt.pem \
	--signature checksums.txt.sig \
	--certificate-identity-regexp '(?i)https://github.com/exeteres/wg-feed/.github/workflows/release.yaml@refs/tags/v.*' \
	--certificate-oidc-issuer https://token.actions.githubusercontent.com \
	checksums.txt

# Download the binary for attestation verification
curl -fsSLO ${BASE}/${BIN}

# Verify binary provenance attestation
gh attestation verify ${BIN} \
	--repo Exeteres/wg-feed \
	--signer-workflow Exeteres/wg-feed/.github/workflows/release.yaml \
	--source-ref refs/tags/v${VERSION}

# Verify binary SBOM attestation
gh attestation verify ${BIN} \
	--repo Exeteres/wg-feed \
	--signer-workflow Exeteres/wg-feed/.github/workflows/release.yaml \
	--source-ref refs/tags/v${VERSION} \
	--predicate-type https://spdx.dev/Document/v2.3
```

### Images

Container images are signed with cosign keyless signatures, and provenance + SBOM attestations are published with GitHub `actions/attest`.

To verify image signature and attestations, use the following commands:

```bash
VERSION=0.5.4
IMAGE=ghcr.io/exeteres/wg-feed/server:v${VERSION}

# Verify image signature
cosign verify \
	--certificate-identity-regexp '(?i)https://github.com/exeteres/wg-feed/.github/workflows/release.yaml@refs/tags/v.*' \
	--certificate-oidc-issuer https://token.actions.githubusercontent.com \
	${IMAGE}

# Verify image provenance attestation
gh attestation verify oci://${IMAGE} \
	--repo Exeteres/wg-feed \
	--signer-workflow Exeteres/wg-feed/.github/workflows/release.yaml \
	--source-ref refs/tags/v${VERSION} \
	--bundle-from-oci

# Verify image SBOM attestation
gh attestation verify oci://${IMAGE} \
	--repo Exeteres/wg-feed \
	--signer-workflow Exeteres/wg-feed/.github/workflows/release.yaml \
	--source-ref refs/tags/v${VERSION} \
	--predicate-type https://spdx.dev/Document/v2.3 \
	--bundle-from-oci
```

## Contributing

Contributions are welcome! Please open issues or pull requests for bug reports, feature requests, or improvements. No special process is required.

## License

The all content of this repository is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
