# wg-feed-upload

wg-feed-upload is a small CLI helper that writes a wg-feed **feed entry** to etcd.

It:
- Reads either a Feed Document JSON object or an ASCII-armored age payload from stdin.
- Computes `revision`.
- Stores a feed entry under `wg-feed/feeds/{feedPath}`.

## Usage

```sh
cat input.txt | go run ./cmd/wg-feed-upload [--ttl 900] <feedPath>
```

Example:

```sh
cat input.txt | go run ./cmd/wg-feed-upload client-a
```

`feedPath` is the URL path segment used by wg-feed-server (`GET /{feedPath}`).

## Environment

| Env Var          | Required | Default | Description                                                              |
| ---------------- | -------: | ------: | ------------------------------------------------------------------------ |
| `ETCD_ENDPOINTS` |      yes |  (none) | Comma-separated list of etcd v3 endpoints, e.g. `http://127.0.0.1:2379`. |

## Input format

stdin must be one of:
- A Feed Document JSON object (unencrypted mode), OR
- An ASCII-armored age payload (encrypted mode), starting with `-----BEGIN AGE ENCRYPTED FILE-----`.

TTL:
- Defaults to 15 minutes (`--ttl 900`).
- Override with `--ttl <seconds>`.

### Unencrypted example (Feed Document JSON)

```json
{
  "id": "11111111-1111-4111-8111-111111111111",
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
```

### Encrypted example (age armored payload)

```text
-----BEGIN AGE ENCRYPTED FILE-----
...
-----END AGE ENCRYPTED FILE-----
```

## Docker usage

```sh
cat input.txt | docker run --rm -i \
  -e ETCD_ENDPOINTS="http://etcd-server:2379" \
  ghcr.io/wg-feed/wg-feed-upload:latest \
  --ttl 900 \
  <feedPath>
```

## Revision calculation

wg-feed-upload computes:
- If stdin is a Feed Document JSON object: `revision = sha256(canonical_json(document))`
- If stdin is an armored payload: `revision = sha256(bytes(armored_payload))`

Where:
- `sha256(...)` is emitted as lowercase hex.
- `canonical_json(data)` is the Go `encoding/json` re-serialization of the decoded JSON value (stable key ordering).

The computed `revision` is what wg-feed-server will expose as HTTP `ETag` and as the wg-feed success response `revision` field.
