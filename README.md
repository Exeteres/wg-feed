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
| [cmd/wg-feed-installer](cmd/wg-feed-installer) | Interactive Linux installer/updater for daemon config and systemd.  |
| [docs](docs)                                   | Draft spec, JSON schema, and examples.                              |
| [internal](internal)                           | Shared Go packages (not a public API).                              |

## Interactive Installer

This repository provides an interactive installer to configure and install `wg-feed-daemon` on Linux distributions that use systemd.

Run it directly from the repository script:

```bash
curl -fsSL https://raw.githubusercontent.com/exeteres/wg-feed/main/scripts/install.sh | bash
```

## Nix / NixOS

This repo provides a flake that builds all four binaries, exports a NetworkManager AmneziaWG plugin package, and a NixOS module for running `wg-feed-daemon` as a systemd service.

See [nixos/README.md](nixos/README.md).

## Contributing

Contributions are welcome! Please open issues or pull requests for bug reports, feature requests, or improvements. No special process is required.

## License

The all content of this repository is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
