# wg-feed-server

wg-feed-server is a small HTTP server that serves wg-feed subscription responses backed by etcd.

It exposes:
- `GET /{feedPath}` returning a wg-feed JSON success response (or error response)
- SSE when the client sends `Accept: text/event-stream`

This server is designed to run behind your HTTPS termination (reverse proxy / load balancer). The spec requires HTTPS for Setup URLs.

## Launch

### Local

1. Start etcd (locally or via `docker compose up -d` in the repo root).
2. Export env vars or set them in .env file.
3. Run `go run ./cmd/wg-feed-server`.

### Docker

Build the server binary:
 `docker build -f docker/scratch.dockerfile --build-arg CMD=wg-feed-server -t wg-feed-server:local .`

Run:
  -e SERVER_PORT=8080 \
  -e ETCD_ENDPOINTS=http://etcd-host:2379 \
  wg-feed-server:local`

## Configuration

| Env Var          | Required | Default | Description                                                              |
| ---------------- | -------: | ------: | ------------------------------------------------------------------------ |
| `SERVER_PORT`    |       no |  `8080` | TCP port to listen on.                                                   |
| `ETCD_ENDPOINTS` |      yes |  (none) | Comma-separated list of etcd v3 endpoints, e.g. `http://127.0.0.1:2379`. |

## etcd Store Layout

Keys:
- Feed entries are stored under: `wg-feed/feeds/{feedPath}`
- The HTTP path `/{feedPath}` maps directly to this key.

Values:
- The value is a **feed entry** JSON object.
- Each key MUST store exactly one of the following shapes:

Unencrypted entry:
```json
{
  "revision": "<opaque string>",
  "ttl_seconds": 60,
  "encrypted": false,
  "data": {
    "id": "<uuid>",
    "display_info": {"title": "Example"},
    "tunnels": [
      {
        "id": "t1",
        "name": "home",
        "display_info": {"title": "Home"},
        "wg_quick_config": "[Interface]\nPrivateKey = ...\nAddress = ...\n"
      }
    ]
  }
}
```

Encrypted entry:
```json
{
  "revision": "<opaque string>",
  "ttl_seconds": 3600,
  "encrypted": true,
  "encrypted_data": "-----BEGIN AGE ENCRYPTED FILE-----\n...\n-----END AGE ENCRYPTED FILE-----"
}
```

Notes:
- The server sets `ETag` to exactly `revision` and supports `If-None-Match` / `304 Not Modified`.
- The server always includes `supports_sse=true` in success responses.

Use [wg-feed-upload](../wg-feed-upload/README.md) to create feed entries in etcd.