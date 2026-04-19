{
  config,
  lib,
  pkgs,
  ...
}: let
  cfg = config.services.wg-feed;
  inherit (lib) mkEnableOption mkIf mkOption types mapAttrsToList flatten optionals;
  yamlFormat = pkgs.formats.yaml {};

  hasNetNSBackend =
    builtins.any (
      feedCfg:
        builtins.any
        (backendCfg: backendCfg.type == "netns")
        (builtins.attrValues feedCfg.backends)
    )
    (builtins.attrValues cfg.feeds);

  isAbsolute = p: lib.hasPrefix "/" p;
  generatedConfigPath = yamlFormat.generate "wg-feed-daemon-config.yaml" {
    inherit (cfg) state_path feeds;
  };
  isEnvPlaceholder = s: let
    n = builtins.stringLength s;
    validName = name: builtins.match "^[A-Za-z_][A-Za-z0-9_]*$" name != null;
  in
    if lib.hasPrefix "${" s && lib.hasSuffix "}" s
    then n > 3 && validName (builtins.substring 2 (n - 3) s)
    else if lib.hasPrefix "$" s
    then n > 1 && validName (builtins.substring 1 (n - 1) s)
    else false;
  isEndpointRef = s: builtins.match "^https://.+" s != null || isEnvPlaceholder s;

  feedEndpointAssertions = flatten (
    mapAttrsToList (
      feedLabel: feedCfg: let
        endpoints = feedCfg.sync.endpoints;
      in
        map (
          endpoint: {
            assertion = isEndpointRef endpoint;
            message = "services.wg-feed.feeds.${feedLabel}.sync.endpoints must contain https URLs or environment placeholders like $SUBSCRIPTION_URL";
          }
        )
        endpoints
    )
    cfg.feeds
  );
in {
  options.services.wg-feed = {
    enable = mkEnableOption "wg-feed-daemon";

    package = mkOption {
      type = types.package;
      default = pkgs.wg-feed;
      defaultText = "pkgs.wg-feed";
      description = "The wg-feed package providing wg-feed-daemon.";
    };

    state_path = mkOption {
      type = types.str;
      default = "/var/lib/wg-feed/state.json";
      description = "Path to the wg-feed state JSON file.";
    };

    logLevel = mkOption {
      type = types.enum ["debug" "info" "warn" "error"];
      default = "info";
      description = "Log level for wg-feed-daemon (exported as LOG_LEVEL).";
    };

    feeds = mkOption {
      type = types.attrsOf (types.submodule {
        options = {
          sync = {
            enabled = mkOption {
              type = types.bool;
              default = true;
              description = "Whether this feed is enabled.";
            };

            mode = mkOption {
              type = types.enum ["sse" "polling"];
              default = "sse";
              description = "Sync mode.";
            };

            polling = {
              interval = mkOption {
                type = types.ints.unsigned;
                default = 0;
                description = "Polling interval in seconds; 0 means use ttl_seconds from server.";
              };
            };

            endpoints = mkOption {
              type = types.listOf types.str;
              description = "Endpoint URLs for this feed.";
            };
          };

          backends = mkOption {
            type = types.attrsOf (types.submodule {
              options = {
                type = mkOption {
                  type = types.enum ["wg-quick" "networkmanager" "netns" "windows"];
                  description = "Backend implementation type.";
                };

                tunnels = mkOption {
                  type = types.attrsOf (types.submodule {
                    options = {
                      enabled = mkOption {
                        type = types.nullOr types.bool;
                        default = null;
                        description = "Optional backend-scoped enabled override for this tunnel id.";
                      };
                    };
                  });
                  default = {};
                  description = "Optional per-tunnel overrides scoped to this backend label.";
                };
              };
            });
            description = "Backends for this feed.";
          };

          tunnels = mkOption {
            type = types.attrsOf (types.submodule {
              options = {
                enabled = mkOption {
                  type = types.nullOr types.bool;
                  default = null;
                  description = "Optional feed-wide enabled override for this tunnel id, used when backend-scoped override is not set.";
                };
              };
            });
            default = {};
            description = "Optional feed-wide per-tunnel overrides by tunnel id.";
          };
        };
      });
      default = {};
      description = ''
        wg-feed feed definitions rendered to YAML in the Nix store and passed
        to wg-feed-daemon via --config.
      '';
    };

    environmentFile = mkOption {
      type = types.str;
      example = "/run/secrets/wg-feed.env";
      description = ''
        Path to a systemd EnvironmentFile.

        This file is loaded into the daemon process environment and is typically
        used for variable expansion in `services.wg-feed.feeds` string values
        such as endpoint URLs.

        Example:
        `SUBSCRIPTION_URL=https://example.invalid/sub#agekey`
      '';
    };

    amnezia = {
      enable = mkOption {
        type = types.bool;
        default = false;
        description = "Whether to include amneziawg-tools in the wg-feed-daemon service PATH.";
      };

      networkManagerPlugin = {
        enable = mkOption {
          type = types.bool;
          default = false;
          description = "Whether to install the NetworkManager AmneziaWG plugin package system-wide.";
        };

        package = mkOption {
          type = types.package;
          default = pkgs.network-manager-amneziawg;
          defaultText = "pkgs.network-manager-amneziawg";
          description = "Package providing the NetworkManager AmneziaWG VPN plugin.";
        };
      };

      useKernelModule = mkOption {
        type = types.bool;
        default = false;
        description = "Whether to enable the amneziawg kernel module in the system.";
      };
    };
  };

  config = mkIf cfg.enable {
    networking.networkmanager.plugins = optionals cfg.amnezia.networkManagerPlugin.enable [cfg.amnezia.networkManagerPlugin.package];

    environment.systemPackages = optionals cfg.amnezia.enable [pkgs.amneziawg-tools pkgs.amneziawg-go];

    assertions =
      [
        {
          assertion = cfg.environmentFile != "" && isAbsolute cfg.environmentFile;
          message = "services.wg-feed.environmentFile must be a non-empty absolute path";
        }
        {
          assertion = cfg.state_path != "" && isAbsolute cfg.state_path;
          message = "services.wg-feed.state_path must be a non-empty absolute path";
        }
        {
          assertion = cfg.feeds != {};
          message = "services.wg-feed.feeds must contain at least one feed";
        }
        {
          assertion = builtins.all (feedCfg: feedCfg.sync.endpoints != []) (builtins.attrValues cfg.feeds);
          message = "services.wg-feed.feeds.<name>.sync.endpoints must contain at least one endpoint";
        }
        {
          assertion = builtins.all (feedCfg: feedCfg.backends != {}) (builtins.attrValues cfg.feeds);
          message = "services.wg-feed.feeds.<name>.backends must contain at least one backend";
        }
        {
          assertion = (!cfg.amnezia.networkManagerPlugin.enable) || cfg.amnezia.enable;
          message = "services.wg-feed.amnezia.networkManagerPlugin.enable requires services.wg-feed.amnezia.enable = true";
        }
        {
          assertion = (!cfg.amnezia.useKernelModule) || cfg.amnezia.enable;
          message = "services.wg-feed.amnezia.useKernelModule requires services.wg-feed.amnezia.enable = true";
        }
      ]
      ++ feedEndpointAssertions;

    systemd.services.wg-feed-daemon = {
      description = "wg-feed daemon";
      wantedBy = ["multi-user.target"];
      after = ["network-online.target"];
      wants = ["network-online.target"];

      # Ensure required helper binaries are in PATH for the service:
      # - wg, wg-quick (wireguard-tools)
      # - nmcli (networkmanager)
      # - ip (iproute2) for route reconciliation
      path = [pkgs.wireguard-tools pkgs.networkmanager pkgs.iproute2] ++ optionals cfg.amnezia.enable [pkgs.amneziawg-tools pkgs.amneziawg-go];

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/wg-feed-daemon --config ${generatedConfigPath}";
        Restart = "always";
        RestartSec = "2s";
        Environment = ["LOG_LEVEL=${cfg.logLevel}"];
        EnvironmentFile = cfg.environmentFile;

        # wg-quick route/interface manipulation requires NET_ADMIN.
        # netns backend additionally needs SYS_ADMIN to manage namespaces.
        AmbientCapabilities = ["CAP_NET_ADMIN"] ++ optionals hasNetNSBackend ["CAP_SYS_ADMIN"];
        CapabilityBoundingSet = ["CAP_NET_ADMIN"] ++ optionals hasNetNSBackend ["CAP_SYS_ADMIN"];
        NoNewPrivileges = true;

        # Create /var/lib/wg-feed with correct ownership.
        StateDirectory = "wg-feed";

        # netns backend requires namespace mounts under /run/netns to be visible
        # from the host mount namespace (for operator tooling and consistency).
        # PrivateTmp creates a separate mount namespace, so disable it for netns.
        PrivateTmp = !hasNetNSBackend;
      };
    };

    boot.kernelModules = optionals cfg.amnezia.useKernelModule ["amneziawg"];
    boot.extraModulePackages = optionals cfg.amnezia.useKernelModule (with config.boot.kernelPackages; [amneziawg]);
  };
}
