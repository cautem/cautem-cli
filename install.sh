#!/bin/sh
# SPDX-FileCopyrightText: Copyright (c) 2026 the cautem authors
# SPDX-License-Identifier: Apache-2.0
#
# Install the cautem CLI from a GitHub release (OpenShell-style one-liner).
#
# Usage:
#   curl -LsSf https://raw.githubusercontent.com/cautem/quarshell-cli/main/install.sh | sh
#
# Environment:
#   CAUTEM_VERSION      Release tag (default: latest published release;
#                           "nightly" is available only when manually published)
#   CAUTEM_INSTALL_DIR  Install directory (default: ~/.local/bin)
#   CAUTEM_REPO         Override owner/name (default: cautem/quarshell-cli)
#   CAUTEM_RELEASE_URL  Base URL holding <tag>/<archive> (mirror / air-gapped;
#                           default: https://github.com/<repo>/releases/download)
#
# Layout (Homebrew-style prefix, derived from the install dir):
#   <prefix>/bin/cautem
#   <prefix>/bin/cautem-gateway            (linux / macOS; `cautem gateway ensure`)
#   <prefix>/bin/cautem-console            (linux / macOS; browser management UI)
#   <prefix>/bin/providers/*.yaml          built-in provider profile catalog
#   <prefix>/libexec/cautem/linux-<arch>/{cautem,cautem-init,cautem-sshd}
# The linux helpers are mounted into sandboxes and proxy sidecars, so no Go
# toolchain, source checkout, or local image build is needed.
#
set -eu

APP_NAME="cautem"
REPO="${CAUTEM_REPO:-cautem/quarshell-cli}"
GITHUB_URL="https://github.com/${REPO}"
API_URL="https://api.github.com/repos/${REPO}"

info() { printf '%s: %s\n' "$APP_NAME" "$*" >&2; }
error() { printf '%s: error: %s\n' "$APP_NAME" "$*" >&2; exit 1; }

has_cmd() { command -v "$1" >/dev/null 2>&1; }

require_cmd() {
  has_cmd "$1" || error "'$1' is required"
}

download() {
  _url="$1"
  _out="$2"
  if has_cmd curl; then
    curl -fLsS --retry 3 --max-redirs 5 -o "$_out" "$_url"
  elif has_cmd wget; then
    wget -q -O "$_out" "$_url"
  else
    error "need curl or wget"
  fi
}

# Map uname to GoReleaser archive names: cautem_<Os>_<Arch>.tar.gz
detect_target() {
  _os="$(uname -s)"
  _arch="$(uname -m)"
  case "$_os" in
    Linux)  _goos="Linux" ;;
    Darwin) _goos="Darwin" ;;
    MINGW*|MSYS*|CYGWIN*) _goos="Windows" ;;
    *) error "unsupported OS: $_os" ;;
  esac
  case "$_arch" in
    x86_64|amd64) _goarch="x86_64" ;;
    aarch64|arm64) _goarch="arm64" ;;
    armv7l) _goarch="armv7" ;;
    i386|i686) _goarch="i386" ;;
    *) error "unsupported arch: $_arch" ;;
  esac
  printf '%s_%s\n' "$_goos" "$_goarch"
}

resolve_version() {
  if [ -n "${CAUTEM_VERSION:-}" ]; then
    printf '%s\n' "$CAUTEM_VERSION"
    return
  fi
  # Latest published release (stable or prerelease marked latest=false — prefer
  # /releases/latest which skips prereleases; fall back to first release list entry).
  _json="$(mktemp)"
  if download "${API_URL}/releases/latest" "$_json" 2>/dev/null; then
    _tag="$(sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$_json" | head -1)"
    rm -f "$_json"
    if [ -n "$_tag" ]; then
      printf '%s\n' "$_tag"
      return
    fi
  fi
  rm -f "$_json"
  # Include prereleases when no stable "latest" exists yet.
  _json="$(mktemp)"
  download "${API_URL}/releases?per_page=5" "$_json" || error "cannot list releases"
  _tag="$(sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$_json" | head -1)"
  rm -f "$_json"
  [ -n "$_tag" ] || error "no releases found on ${REPO}; build from source or wait for CI"
  printf '%s\n' "$_tag"
}

install_dir() {
  if [ -n "${CAUTEM_INSTALL_DIR:-}" ]; then
    printf '%s\n' "$CAUTEM_INSTALL_DIR"
    return
  fi
  printf '%s\n' "${HOME}/.local/bin"
}

is_on_path() {
  case ":${PATH}:" in
    *:"$1":*) return 0 ;;
    *) return 1 ;;
  esac
}

main() {
  require_cmd uname
  require_cmd tar
  require_cmd install
  require_cmd mktemp
  require_cmd sed

  _version="$(resolve_version)"
  _target="$(detect_target)"
  _archive="${APP_NAME}_${_target}.tar.gz"
  if [ "$_target" = "Windows_x86_64" ] || [ "$_target" = "Windows_arm64" ]; then
    _archive="${APP_NAME}_${_target}.zip"
  fi
  _base="${CAUTEM_RELEASE_URL:-${GITHUB_URL}/releases/download}"
  _url="${_base}/${_version}/${_archive}"
  _checksums_url="${_base}/${_version}/checksums.txt"
  _dir="$(install_dir)"

  info "installing ${_version} (${_target}) → ${_dir}"
  _tmpdir="$(mktemp -d)"
  trap 'rm -rf "$_tmpdir"' EXIT

  if ! download "$_url" "${_tmpdir}/${_archive}"; then
    error "download failed: ${_url}"
  fi

  if download "$_checksums_url" "${_tmpdir}/checksums.txt" 2>/dev/null; then
    info "verifying checksum"
    (
      cd "$_tmpdir"
      grep "[ *]${_archive}\$" checksums.txt > archive.sha256 || error "${_archive} missing from checksums.txt"
      if has_cmd sha256sum; then
        sha256sum -c archive.sha256 >/dev/null || error "checksum mismatch for ${_archive}"
      elif has_cmd shasum; then
        shasum -a 256 -c archive.sha256 >/dev/null || error "checksum mismatch for ${_archive}"
      else
        info "sha256 tool missing; skipping checksum verify"
      fi
    )
  else
    info "checksums.txt not found for ${_version}; continuing without verify"
  fi

  case "$_archive" in
    *.zip)
      require_cmd unzip
      unzip -q "${_tmpdir}/${_archive}" -d "$_tmpdir"
      ;;
    *)
      tar -xzf "${_tmpdir}/${_archive}" -C "$_tmpdir"
      ;;
  esac

  _exe="${APP_NAME}"
  [ -f "${_tmpdir}/${APP_NAME}.exe" ] && _exe="${APP_NAME}.exe"
  _bin="${_tmpdir}/${_exe}"
  [ -f "$_bin" ] || error "binary not found in archive"

  _sudo=""
  mkdir -p "$_dir" 2>/dev/null || true
  if [ ! -w "$_dir" ]; then
    info "elevated permissions required for ${_dir}"
    _sudo="sudo"
    sudo mkdir -p "$_dir"
  fi
  $_sudo install -m 755 "$_bin" "${_dir}/${_exe}"
  if [ -f "${_tmpdir}/${APP_NAME}-gateway" ]; then
    $_sudo install -m 755 "${_tmpdir}/${APP_NAME}-gateway" "${_dir}/${APP_NAME}-gateway"
    info "gateway → ${_dir}/${APP_NAME}-gateway"
  fi
  if [ -f "${_tmpdir}/${APP_NAME}-console" ]; then
    $_sudo install -m 755 "${_tmpdir}/${APP_NAME}-console" "${_dir}/${APP_NAME}-console"
    info "console → ${_dir}/${APP_NAME}-console"
  fi

  _providers_src="${_tmpdir}/providers"
  if [ -d "$_providers_src" ]; then
    _providers_dst="${_dir}/providers"
    $_sudo mkdir -p "$_providers_dst"
    for _profile in "${_providers_src}"/*.yaml; do
      [ -f "$_profile" ] || continue
      _profile_name="${_profile##*/}"
      $_sudo install -m 644 "$_profile" "${_providers_dst}/${_profile_name}"
    done
    info "provider profiles → ${_providers_dst}"
  else
    info "warning: ${_version} ships no built-in provider profiles"
  fi

  _helpers_src="${_tmpdir}/libexec/cautem"
  _helpers_dst="$(dirname "$_dir")/libexec/cautem"
  if [ -d "$_helpers_src" ]; then
    $_sudo rm -rf "$_helpers_dst"
    $_sudo mkdir -p "$_helpers_dst"
    $_sudo cp -R "${_helpers_src}/." "${_helpers_dst}/"
    $_sudo chmod -R a+rX,go-w "$_helpers_dst"
    info "linux helpers → ${_helpers_dst}"
  else
    info "warning: ${_version} ships no linux helpers; sandbox create will need a source checkout + Go"
  fi

  info "installed $("${_dir}/${_exe}" version 2>/dev/null || echo "$_version") → ${_dir}/${_exe}"

  if ! is_on_path "$_dir"; then
    info "${_dir} is not on PATH; add it, e.g.:"
    info "  export PATH=\"${_dir}:\$PATH\""
  fi

  info "next:  ${APP_NAME} --help"
  info "       ${APP_NAME} sandbox create --name demo --workspace . --policy …"
}

main "$@"
