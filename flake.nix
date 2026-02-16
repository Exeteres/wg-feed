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
        default = pkgs.wg-feed;
      };

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
