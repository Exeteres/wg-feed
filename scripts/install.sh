#!/usr/bin/env bash
set -euo pipefail

REPO_OWNER="exeteres"
REPO_NAME="wg-feed"

require_cmd() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    echo "error: required command not found: $name" >&2
    exit 1
  fi
}

preflight() {
  if [[ "$(uname -s)" != "Linux" ]]; then
    echo "error: wg-feed installer is only supported on Linux systems with systemd" >&2
    exit 1
  fi

  require_cmd uname
  require_cmd curl
  require_cmd sed
  require_cmd head
  require_cmd awk
  require_cmd mkdir
  require_cmd chmod
  require_cmd rm
  require_cmd systemctl

  if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
    echo "error: neither sha256sum nor shasum is available" >&2
    exit 1
  fi

  if [[ "${EUID}" -ne 0 ]]; then
    require_cmd sudo
  fi
}

detect_arch() {
  local m
  m="$(uname -m)"
  case "$m" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64) printf 'arm64' ;;
    *)
      echo "error: unsupported architecture: $m" >&2
      exit 1
      ;;
  esac
}

latest_tag() {
  local json tag
  json="$(curl -fsSL "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest")"
  tag="$(printf '%s\n' "$json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]\+\)".*/\1/p' | head -n1)"
  if [[ -z "$tag" ]]; then
    echo "error: failed to detect latest release tag" >&2
    exit 1
  fi
  printf '%s' "$tag"
}

checksum_for_asset() {
  local checksums_text="$1"
  local asset_name="$2"
  local sum
  sum="$(printf '%s\n' "$checksums_text" | awk -v asset="$asset_name" '$2 == asset { print $1; exit }')"
  if [[ -z "$sum" ]]; then
    echo "error: checksum for asset not found: $asset_name" >&2
    exit 1
  fi
  printf '%s' "$sum"
}

sha256_file() {
  local path="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$path" | awk '{print $1}'
    return 0
  fi
  echo "error: neither sha256sum nor shasum is available" >&2
  exit 1
}

verify_checksum() {
  local path="$1"
  local expected="$2"
  local actual
  actual="$(sha256_file "$path")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "error: checksum mismatch for $path" >&2
    echo "expected: ${expected}" >&2
    echo "actual:   ${actual}" >&2
    exit 1
  fi
}

main() {
  preflight

  local arch tag tag_version installer_asset daemon_asset installer_url checksums_url checksums_text installer_checksum daemon_checksum tmpdir installer_bin need_download actual_checksum
  arch="$(detect_arch)"
  tag="$(latest_tag)"
  tag_version="${tag#v}"
  installer_asset="wg-feed-installer_${tag_version}_linux_${arch}"
  daemon_asset="wg-feed-daemon_${tag_version}_linux_${arch}"
  installer_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${tag}/${installer_asset}"
  checksums_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${tag}/checksums.txt"

  checksums_text="$(curl -fsSL "$checksums_url")"
  installer_checksum="$(checksum_for_asset "$checksums_text" "$installer_asset")"
  daemon_checksum="$(checksum_for_asset "$checksums_text" "$daemon_asset")"

  tmpdir="/tmp/wg-feed-installer-cache"
  installer_bin="${tmpdir}/${installer_asset}-${tag}"
  mkdir -p "$tmpdir"

  need_download=1
  if [[ -f "$installer_bin" ]]; then
    actual_checksum="$(sha256_file "$installer_bin")"
    if [[ "$actual_checksum" == "$installer_checksum" ]]; then
      need_download=0
      echo "Using cached installer ${installer_asset} (${tag})..."
    else
      echo "Cached installer checksum mismatch, re-downloading..."
      rm -f "$installer_bin"
    fi
  fi

  if [[ "$need_download" -eq 1 ]]; then
    echo "Downloading installer ${installer_asset} (${tag})..."
    curl -fL "$installer_url" -o "$installer_bin"
    chmod 0755 "$installer_bin"
    verify_checksum "$installer_bin" "$installer_checksum"
  fi

  if [[ "${EUID}" -ne 0 ]]; then
    exec sudo env WG_FEED_VERSION="$tag" WG_FEED_DAEMON_CHECKSUM="$daemon_checksum" "$installer_bin" "$@"
  fi
  exec env WG_FEED_VERSION="$tag" WG_FEED_DAEMON_CHECKSUM="$daemon_checksum" "$installer_bin" "$@"
}

main "$@"
