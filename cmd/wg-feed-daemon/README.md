# wg-feed-daemon

wg-feed-daemon is a long-running wg-feed client.

It keeps subscriptions up to date (polling and/or SSE) and reconciles local managed tunnels.

Reconciliation is revision-gated: after a successful sync, it reconciles only when the `revision` changes since the last successfully reconciled revision (unless forced to repair local state).

If you only need to apply the feed once, consider using [wg-feed-apply](../wg-feed-apply/README.md) instead.

## Usage

```sh
BACKEND=wg-quick SETUP_URLS=https://example.invalid/sub/abc go run ./cmd/wg-feed-daemon
```

## Environment

| Env Var      | Required |      Default | Description                                                                                                                                                                               |
| ------------ | -------: | -----------: | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `BACKEND`    |      yes |       (none) | One of: `wg-quick`, `networkmanager`, `windows`.                                                                                                                                          |
| `SETUP_URLS` |      yes |       (none) | Comma-separated list of Setup URLs. Treat as secret.                                                                                                                                      |
| `STATE_PATH` |       no | OS-dependent | Path to the wg-feed state file (persists managed tunnel mapping; if the server sends `encrypted_data`, that ciphertext is cached verbatim and may be used during temporary feed outages). |

The state file does not store Setup URLs directly, so secrets in the URL (query / fragment) are not written to disk.

## Encrypted feeds (age)

If the server returns `encrypted=true`, you MUST provide the age secret key via the Setup URL fragment (the portion after `#`), as described in [docs/draft-wg-feed-00.md](../../docs/draft-wg-feed-00.md).

## State file format

The state file is versionless JSON.

Top-level shape:

```json
{
	"setup_url_salt": "<hex>",
	"setup_url_map": {
		"<hmac_sha256(canonical_setup_url_no_fragment)>": "<feed_id>"
	},
	"feeds": {
		"<feed_id>": {
			"last_reconciled_revision": "<revision>",
			"ttl_seconds": 3600,
			"cached_encrypted_data": "-----BEGIN AGE ENCRYPTED FILE-----\n...",
			"tunnels": {
				"<tunnel_id>": { "name": "wg0", "enabled": true }
			}
		}
	}
}
```

Notes:
- The `feeds` map is keyed by the subscription ID (the Feed Document top-level `id`).
- A `feeds[<feed_id>]` entry is created only after a successful fetch/decrypt/validate (if the URL is unreachable, the daemon cannot discover the feed ID and will not create an entry).
- If multiple Setup URLs resolve to the same `id`, the daemon ignores duplicates and logs a warning.
- The state file never stores Setup URLs. Instead, it stores a `setup_url_map` entry keyed by a salted HMAC-SHA256 of the canonicalized Setup URL with the fragment removed.
- If `cached_encrypted_data` is present, the daemon can decrypt it using the Setup URL fragment and learn `endpoints[]` without performing a bootstrap fetch. In that case, it syncs using `endpoints[]` and does not request the Setup URL.
- `cached_encrypted_data` is only stored when the server response is encrypted; the daemon reuses the server-provided `encrypted_data` ciphertext verbatim (it does not re-encrypt locally).
- For unencrypted feeds, the daemon must bootstrap using the Setup URL at least once per process start to learn `endpoints[]` (endpoints are kept in memory, not persisted).
- `tunnels` is keyed by tunnel `id` and stores the backend name and the locally effective enabled state.

## Running in containers

wg-feed-daemon is designed to be a long-running process, so it works well as:
- A host-level service container (Linux host network), or
- A Kubernetes sidecar container in the same Pod as an application container.

### Docker (host network)

Run the daemon in the host network namespace (Linux):

```sh
docker run --rm \
	--network host \
	--cap-add NET_ADMIN \
	-e BACKEND=wg-quick \
	-e SETUP_URLS="https://example.invalid/sub/abc" \
	-e STATE_PATH=/state/state.json \
	-v /var/lib/wg-feed:/state \
	ghcr.io/exeteres/wg-feed/daemon:latest
```

### Docker Compose (sidecar-style with another container)

You can share the daemon’s network namespace with another service:

```yaml
services:
	wg:
		image: ghcr.io/exeteres/wg-feed/daemon:latest
		network_mode: host
		cap_add: ["NET_ADMIN"]
		environment:
			BACKEND: wg-quick
			SETUP_URLS: https://example.invalid/sub/abc
			STATE_PATH: /state/state.json
		volumes:
			- /var/lib/wg-feed:/state

	app:
		image: your-app:latest
		network_mode: "service:wg"
		depends_on: [wg]
```

### Kubernetes (sidecar in a Pod)

This runs wg-feed-daemon alongside an app container in the same Pod network namespace.

```yaml
apiVersion: v1
kind: Pod
metadata:
	name: app-with-wg
spec:
	volumes:
		- name: wgfeed-state
			emptyDir: {}

	containers:
		- name: wg-feed-daemon
			image: ghcr.io/exeteres/wg-feed/daemon:latest
			securityContext:
				capabilities:
					add: ["NET_ADMIN"]
			env:
				- name: BACKEND
					value: wg-quick
				- name: SETUP_URLS
					valueFrom:
						secretKeyRef:
							name: wgfeed
							key: setup_urls
				- name: STATE_PATH
					value: /state/state.json
				volumeMounts:
					- name: wgfeed-state
						mountPath: /state

		- name: app
			image: your-app:latest
```

Notes:
- You may need additional cluster/node configuration if the WireGuard kernel module is not available.
- Treat `SETUP_URLS` as a secret; using a Kubernetes Secret is recommended.
