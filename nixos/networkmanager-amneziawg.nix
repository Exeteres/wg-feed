{
  lib,
  stdenv,
  fetchFromGitHub,
  cmake,
  pkg-config,
  gettext,
  intltool,
  glib,
  gtk3,
  gtk4,
  libnma,
  libnma-gtk4,
  networkmanager,
}:
stdenv.mkDerivation (finalAttrs: {
  pname = "networkmanager-amneziawg";
  version = "0.9.12";

  src = fetchFromGitHub {
    owner = "vovochka404";
    repo = "network-manager-amneziawg";
    rev = "v${finalAttrs.version}";
    hash = "sha256-bLKrChoGlJMbijdjmeYe8Fdojhi3BA/7fx3eRbo7w7w=";
  };

  # Pending upstream: https://github.com/vovochka404/network-manager-amneziawg/pull/19
  patches = [./networkmanager-amneziawg-multiple-connections.patch];

  nativeBuildInputs = [
    cmake
    pkg-config
    gettext
    intltool
    glib
  ];

  buildInputs = [
    glib
    gtk3
    gtk4
    libnma
    libnma-gtk4
    networkmanager
  ];

  cmakeFlags = [
    "-DCMAKE_INSTALL_PREFIX=${placeholder "out"}"
    "-DCMAKE_INSTALL_LIBDIR=lib"
    "-DNM_PLUGIN_DIR=${placeholder "out"}/lib/NetworkManager"
    "-DNM_VPN_SERVICE_DIR=${placeholder "out"}/lib/NetworkManager/VPN"
    "-DWITH_GTK3=ON"
    "-DWITH_GTK4=ON"
  ];

  postPatch = ''
    patchShebangs scripts
    substituteInPlace nm-amneziawg-service.name.in \
      --replace-fail '@CMAKE_INSTALL_PREFIX@/@CMAKE_INSTALL_LIBEXECDIR@/nm-amneziawg-service' '@CMAKE_INSTALL_FULL_LIBEXECDIR@/nm-amneziawg-service'
    substituteInPlace CMakeLists.txt \
      --replace-fail 'if(NOT DEFINED CMAKE_INSTALL_PREFIX_INITIALIZED_TO_DEFAULT)' 'if(FALSE)'
  '';

  doCheck = true;
  checkPhase = ''
    runHook preCheck
    ctest --output-on-failure --no-tests=error
    runHook postCheck
  '';

  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck
    grep -qx 'supports-multiple-connections=true' "$out/lib/NetworkManager/VPN/nm-amneziawg-service.name"
    "$out/libexec/nm-amneziawg-service" --help | grep -q -- --bus-name
    runHook postInstallCheck
  '';

  passthru = {
    networkManagerPlugin = "VPN/nm-amneziawg-service.name";
  };

  meta = with lib; {
    description = "NetworkManager VPN plugin for AmneziaWG";
    homepage = "https://github.com/vovochka404/network-manager-amneziawg";
    license = licenses.gpl2Plus;
    platforms = platforms.linux;
  };
})
