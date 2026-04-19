{
  lib,
  buildGoModule,
  go_1_25,
  src ? ../.,
  version ? "0.0.0",
  vendorHash ? "sha256-wtmLjM2kD8PnfO9L/vVZWnG0jpE/GfOWBMYs3X9fJUQ=",
}:
buildGoModule {
  pname = "wg-feed";
  inherit version src vendorHash;

  # go.mod requires Go 1.25.x
  go = go_1_25;

  subPackages = [
    "cmd/wg-feed-apply"
    "cmd/wg-feed-daemon"
    "cmd/wg-feed-server"
    "cmd/wg-feed-upload"
  ];

  ldflags = ["-s" "-w"];

  meta = with lib; {
    description = "wg-feed reference implementation";
    license = licenses.mit;
    platforms = platforms.unix;
  };
}
