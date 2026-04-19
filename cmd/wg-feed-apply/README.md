# wg-feed-apply

wg-feed-apply is a one-shot wg-feed client.

It loads client configuration from a config file, fetches enabled feeds once, and reconciles tunnels into all configured backends for each enabled feed.

If you need continuous syncing over time, use [wg-feed-daemon](../wg-feed-daemon/README.md).

## Usage

Default config lookup path:

- `/etc/wg-feed/config.yaml`
- `/etc/wg-feed/config.yml`
- `/etc/wg-feed/config.toml`
- `/etc/wg-feed/config.json`

Run with default lookup:

```sh
wg-feed-apply
```

Run with explicit config path:

```sh
wg-feed-apply --config ./config.yaml
```

## Config format

Use the same config schema as daemon.

See [Config format](../wg-feed-daemon/README.md#config-format).

## Encrypted feeds (age)

If the server returns `encrypted=true`, provide the age key fragment in endpoint URLs (`https://...#<fragment>`), as described in [docs/draft-wg-feed-00.md](../../docs/draft-wg-feed-00.md).

## State file

State format is shared with daemon.

See [State file format](../wg-feed-daemon/README.md#state-file-format).
