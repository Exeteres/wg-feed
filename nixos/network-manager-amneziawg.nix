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
  pname = "network-manager-amneziawg";
  version = "0.9.9";

  src = fetchFromGitHub {
    owner = "vovochka404";
    repo = "network-manager-amneziawg";
    rev = "v${finalAttrs.version}";
    hash = "sha256-kA/IJ78QcMtgMoE4xk3sjyYqBb9V+OT4/qJlV/H4EW8=";
  };

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
      --replace-fail '/usr/bin/glib-compile-resources' 'glib-compile-resources' \
      --replace-fail 'if(NOT DEFINED CMAKE_INSTALL_PREFIX_INITIALIZED_TO_DEFAULT)' 'if(FALSE)'
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
