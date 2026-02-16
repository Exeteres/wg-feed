# NixOS module

The module provides:
- A package `pkgs.wg-feed` containing all four binaries (`wg-feed-apply`, `wg-feed-daemon`, `wg-feed-server`, `wg-feed-upload`)
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
          services.wg-feed.enable = true;
          services.wg-feed.backend = "wg-quick"; # or "networkmanager"
          services.wg-feed.environmentFile = "/run/secrets/wg-feed.env";
          # services.wg-feed.statePath = "/var/lib/wg-feed/state.json";

          # You can also do both
        })
      ];
    };
  };
}
```

`services.wg-feed.environmentFile` must be a systemd `EnvironmentFile` containing `SETUP_URLS` (comma-separated):

```sh
# /run/secrets/wg-feed.env
SETUP_URLS=https://example.invalid/sub/abc,https://example.invalid/sub/def
```

You should treat this file as a secret.
