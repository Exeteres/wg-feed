{
  config,
  lib,
  pkgs,
  ...
}: let
  cfg = config.services.wg-feed;
  inherit (lib) mkEnableOption mkIf mkOption types;

  isAbsolute = p: lib.hasPrefix "/" p;
in {
  options.services.wg-feed = {
    enable = mkEnableOption "wg-feed-daemon";

    package = mkOption {
      type = types.package;
      default = pkgs.wg-feed;
      defaultText = "pkgs.wg-feed";
      description = "The wg-feed package providing wg-feed-daemon.";
    };

    backend = mkOption {
      type = types.enum ["wg-quick" "networkmanager"];
      default = "wg-quick";
      description = "Client backend implementation to use.";
    };

    environmentFile = mkOption {
      type = types.str;
      example = "/run/secrets/wg-feed.env";
      description = ''
        Path to a systemd EnvironmentFile that provides the bootstrap Setup URLs.

        The file MUST define `SETUP_URLS` (comma-separated Setup URLs), for example:

        `SETUP_URLS=https://example.invalid/sub/abc,https://example.invalid/sub/def`

        Treat this file as a secret. You can use nix-sops to manage it.
      '';
    };

    statePath = mkOption {
      type = types.str;
      default = "/var/lib/wg-feed/state.json";
      description = "Path to the wg-feed state JSON file.";
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.environmentFile != "" && isAbsolute cfg.environmentFile;
        message = "services.wg-feed.environmentFile must be a non-empty absolute path";
      }
      {
        assertion = cfg.statePath != "" && isAbsolute cfg.statePath;
        message = "services.wg-feed.statePath must be a non-empty absolute path";
      }
    ];

    systemd.services.wg-feed-daemon = {
      description = "wg-feed daemon";
      wantedBy = ["multi-user.target"];
      after = ["network-online.target"];
      wants = ["network-online.target"];

      # Ensure required helper binaries are in PATH for the service:
      # - wg, wg-quick (wireguard-tools)
      # - nmcli (networkmanager)
      # - ip (iproute2) for route reconciliation
      path = [pkgs.wireguard-tools pkgs.networkmanager pkgs.iproute2];

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/wg-feed-daemon";
        Restart = "always";
        RestartSec = "2s";

        Environment = [
          "BACKEND=${cfg.backend}"
          "STATE_PATH=${cfg.statePath}"
        ];
        EnvironmentFile = cfg.environmentFile;

        # wg-quick route/interface manipulation requires NET_ADMIN.
        AmbientCapabilities = ["CAP_NET_ADMIN"];
        CapabilityBoundingSet = ["CAP_NET_ADMIN"];
        NoNewPrivileges = true;

        # Create /var/lib/wg-feed with correct ownership.
        StateDirectory = "wg-feed";

        PrivateTmp = true;
      };
    };
  };
}
