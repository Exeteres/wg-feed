# wg-feed-apply

wg-feed-apply is a one-shot wg-feed client.

It fetches one or more Setup URLs and applies the tunnels using the selected backend.

wg-feed-apply is a forced reconciliation: if a Setup URL cannot be fetched, the command fails.

If you need to keep tunnels in sync over time, consider using [wg-feed-daemon](../wg-feed-daemon/README.md) instead.

## Usage

```sh
BACKEND=wg-quick SETUP_URLS=https://example.invalid/sub/abc go run ./cmd/wg-feed-apply
```

Multiple URLs:

```sh
BACKEND=wg-quick SETUP_URLS=https://a.example/x,https://b.example/y go run ./cmd/wg-feed-apply
```

## Environment

| Env Var      | Required |      Default | Description                                                                                                                                                                                  |
| ------------ | -------: | -----------: | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `BACKEND`    |      yes |       (none) | One of: `wg-quick`, `networkmanager`, `windows`.                                                                                                                                             |
| `SETUP_URLS` |      yes |       (none) | Comma-separated list of Setup URLs. Treat as secret.                                                                                                                                         |
| `STATE_PATH` |       no | OS-dependent | Path to the wg-feed state file (persists managed tunnel mapping; if the server sends `encrypted_data`, that exact encrypted payload is cached encrypted-at-rest for future daemon fallback). |

The state file does not store Setup URLs directly, so secrets in the URL (query / fragment) are not written to disk.

See [wg-feed-daemon state file format](../wg-feed-daemon/README.md#state-file-format) for the exact JSON shape.

## Encrypted feeds (age)

If the server returns `encrypted=true`, you MUST provide the age secret key via the Setup URL fragment (the portion after `#`), as described in [docs/draft-wg-feed-00.md](../../docs/draft-wg-feed-00.md).

Example:

`https://example.invalid/sub/abc#1kyhr0sl...`

## Running in containers

wg-feed-apply can be used as a one-shot “network setup” step for another container.

### Docker (host network)

Run once on the host network namespace (Linux):

```sh
docker run --rm \
	--network host \
	--cap-add NET_ADMIN \
	-e BACKEND=wg-quick \
	-e SETUP_URLS="https://example.invalid/sub/abc" \
	-e STATE_PATH=/state/state.json \ # optional, but needed if you want to run it multiple times
	-v /var/lib/wg-feed:/state \
	ghcr.io/exeteres/wg-feed/apply:latest
```

### Kubernetes (init container in a Pod)

This applies the feed once before the main app starts (shared network namespace within the Pod).

```yaml
apiVersion: v1
kind: Pod
metadata:
	name: app-with-wg
spec:
	volumes:
		- name: wgfeed-state
			emptyDir: {}

	initContainers:
		- name: wg-feed-apply
			image: ghcr.io/exeteres/wg-feed/apply:latest
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

	containers:
		- name: app
			image: your-app:latest
```

Notes:
- You may need additional cluster/node configuration if the WireGuard kernel module is not available.
- Treat `SETUP_URLS` as a secret; using a Kubernetes Secret is recommended.
