# NixOS module

The module provides:

- A package `pkgs.wg-feed` containing all four binaries (`wg-feed-apply`, `wg-feed-daemon`, `wg-feed-server`, `wg-feed-upload`)
- A package `pkgs.networkmanager-amneziawg` for the [NetworkManager AmneziaWG plugin](https://github.com/vovochka404/network-manager-amneziawg), including the GTK editor UI plugin
- A systemd service `wg-feed-daemon` controlled by `services.wg-feed.*`

Example usage:

```nix
{
  inputs.wg-feed.url = "github:exeteres/wg-feed";

  outputs = { self, nixpkgs, wg-feed, ... }: {
    nixosConfigurations.myhost = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        wg-feed.nixosModules.wg-feed
        ({ pkgs, ... }: {
          # Option A: install binaries into PATH
          environment.systemPackages = [pkgs.wg-feed];

          # Option B: run daemon as a service
          services.wg-feed = {
            enable = true;
            environmentFile = "/run/secrets/wg-feed.env";
            state_path = "/var/lib/wg-feed/state.json";
            logLevel = "info";

            amnezia = {
              enable = true;
              networkManagerPlugin.enable = true;
              useKernelModule = true;
            };

            feeds.main = {
              sync = {
                enabled = true;
                mode = "sse";

                polling.interval = 0;

                endpoints = [
                  "$SUBSCRIPTION_URL"
                ];
              };

              backends.default.type = "wg-quick";
              # Or: "networkmanager", "netns", "windows"

              tunnels.demo-primary.enabled = true;
            };
          };

          # You can also do both
        })
      ];
    };
  };
}
```

`services.wg-feed.state_path` and `services.wg-feed.feeds` are validated and rendered automatically to YAML in the Nix store.
The generated path is passed to `wg-feed-daemon --config` by the module.

Use `services.wg-feed.environmentFile` to provide runtime environment variables for
string expansion (for example endpoint URLs with `$VAR`).

Use `services.wg-feed.logLevel` to set daemon log verbosity
(`"debug"`, `"info"`, `"warn"`, `"error"`).

Example environment file:

```sh
# /run/secrets/wg-feed.env
SUBSCRIPTION_URL=https://example.invalid/sub#agekey
```

Generated YAML shape (from `services.wg-feed.state_path` and `services.wg-feed.feeds`) looks like:

```yaml
state_path: /var/lib/wg-feed/state.json

feeds:
  main:
    sync:
      enabled: true
      mode: sse
      polling:
        interval: 0
      endpoints:
        - $SUBSCRIPTION_URL
    backends:
      default:
        type: wg-quick
```

Treat endpoint URLs and environment files as secrets.

Per-tunnel override options:

- `services.wg-feed.feeds.<name>.tunnels.<tunnelId>.enabled`: optional bool.
- `services.wg-feed.feeds.<name>.backends.<backend>.tunnels.<tunnelId>.enabled`: optional bool.
- Enabled precedence is backend-scoped override > feed-wide override > server feed document `tunnels[].enabled`.
- Server `forced` is ignored for enable/disable decisions.

Amnezia options:

- `services.wg-feed.amnezia.enable`: adds `amneziawg-tools` and `amneziawg-go` to daemon service PATH and installs both into `environment.systemPackages`.
- `services.wg-feed.amnezia.networkManagerPlugin.enable`: installs NetworkManager AmneziaWG plugin package into `environment.systemPackages` and registers it in `networking.networkmanager.plugins`.
- `services.wg-feed.amnezia.networkManagerPlugin.package`: override plugin package derivation (defaults to `pkgs.networkmanager-amneziawg`).
- `services.wg-feed.amnezia.useKernelModule`: enables kernel module loading for `amneziawg`.

Flake package exports:

- `packages.<system>.wg-feed`
- `packages.<system>.networkmanager-amneziawg`

When any configured backend uses `type = "netns"`, the module automatically
adds `CAP_SYS_ADMIN` (in addition to `CAP_NET_ADMIN`) to the
`wg-feed-daemon` service so Linux network namespace operations can succeed.

When any configured backend uses `type = "netns"`, the module also disables
`PrivateTmp` for `wg-feed-daemon` so `/run/netns/*` mounts are visible from the
host mount namespace (for example, `ip netns exec <name> ...` from an admin shell).
