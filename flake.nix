{
  description = "wg-feed";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }: let
    overlay = final: prev: {
      wg-feed = prev.callPackage ./nixos/package.nix {
        src = self;
      };
      networkmanager-amneziawg = prev.callPackage ./nixos/networkmanager-amneziawg.nix {};
    };
  in
    (flake-utils.lib.eachDefaultSystem (system: let
      pkgs = import nixpkgs {
        inherit system;
        overlays = [overlay];
      };
    in {
      packages = {
        wg-feed = pkgs.wg-feed;
        networkmanager-amneziawg = pkgs.networkmanager-amneziawg;
        default = pkgs.wg-feed;
      };

      checks = nixpkgs.lib.optionalAttrs (system == "x86_64-linux") (
        builtins.listToAttrs (map (kernel: {
          name = "nixos-module-${kernel}";
          value =
            (nixpkgs.lib.nixosSystem {
              inherit system;
              modules = [
                self.nixosModules.wg-feed
                ({pkgs, ...}: {
                  system.stateVersion = "26.05";
                  boot.kernelPackages =
                    if kernel == "zen"
                    then pkgs.linuxPackages_zen
                    else pkgs.linuxPackages;
                  boot.loader.grub.enable = false;
                  fileSystems."/" = {
                    device = "/dev/vda";
                    fsType = "ext4";
                  };
                  networking.networkmanager.enable = true;
                  services.wg-feed = {
                    enable = true;
                    environmentFile = "/run/secrets/wg-feed.env";
                    amnezia = {
                      enable = true;
                      useKernelModule = true;
                      networkManagerPlugin.enable = true;
                    };
                    feeds.test = {
                      sync.endpoints = ["https://example.invalid/subscription"];
                      backends.default.type = "networkmanager";
                    };
                  };
                })
              ];
            }).config.system.build.toplevel;
        }) ["default" "zen"])
      );

      apps = {
        wg-feed-apply = flake-utils.lib.mkApp {
          drv = pkgs.wg-feed;
          exePath = "/bin/wg-feed-apply";
        };
        wg-feed-daemon = flake-utils.lib.mkApp {
          drv = pkgs.wg-feed;
          exePath = "/bin/wg-feed-daemon";
        };
        wg-feed-server = flake-utils.lib.mkApp {
          drv = pkgs.wg-feed;
          exePath = "/bin/wg-feed-server";
        };
        wg-feed-upload = flake-utils.lib.mkApp {
          drv = pkgs.wg-feed;
          exePath = "/bin/wg-feed-upload";
        };
      };
    }))
    // {
      overlays.default = overlay;
      nixosModules.wg-feed = {...}: {
        imports = [./nixos/module.nix];
        nixpkgs.overlays = [overlay];
      };
    };
}
