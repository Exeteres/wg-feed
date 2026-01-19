# wg-feed Internal Packages

This directory contains non-public Go packages used by the command binaries under [cmd/](../cmd/).

Notable areas:
- `internal/server`: server app + HTTP API implementation
- `internal/etcd`: etcd client/store helpers
- `internal/client`: client fetch/apply logic and backend integrations
- `internal/model`: wg-feed JSON models + validation
