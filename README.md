# wg-feed

wg-feed is a draft protocol and reference implementation for distributing WireGuard tunnel configurations via setup URLs.

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

| Path                                     | Description                                                         |
| ---------------------------------------- | ------------------------------------------------------------------- |
| [cmd/wg-feed-server](cmd/wg-feed-server) | HTTP server backed by etcd.                                         |
| [cmd/wg-feed-upload](cmd/wg-feed-upload) | Upload helper: computes `revision` and writes feed entries to etcd. |
| [cmd/wg-feed-apply](cmd/wg-feed-apply)   | One-shot client: fetch + reconcile/apply once.                      |
| [cmd/wg-feed-daemon](cmd/wg-feed-daemon) | Long-running client: sync + reconcile over time.                    |
| [docs](docs)                             | Draft spec, JSON schema, and examples.                              |
| [internal](internal)                     | Shared Go packages (not a public API).                              |

## Contributing

Contributions are welcome! Please open issues or pull requests for bug reports, feature requests, or improvements. No special process is required.

## License

The all content of this repository is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.