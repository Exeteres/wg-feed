{
  lib,
  buildGoModule,
  src ? ../.,
  version ? "0.0.0",
  vendorHash ? "sha256-cXW+njxUyxBkAhqDyLkY3yuFjsQZ/17OdI2s32tUVyM=",
}:
buildGoModule {
  pname = "wg-feed";
  inherit version src vendorHash;

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
