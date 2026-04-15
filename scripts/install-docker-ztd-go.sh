#!/usr/bin/env bash
set -euo pipefail

REPO="ku9nov/docker-compose-ztd-plugin"
PLUGIN_DIR="${HOME}/.docker/cli-plugins"
PLUGIN_PATH="${PLUGIN_DIR}/docker-ztd"

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "${os}" in
    linux|darwin)
      printf '%s\n' "${os}"
      ;;
    *)
      echo "Unsupported OS: ${os}. Supported: linux, darwin." >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "${arch}" in
    x86_64|amd64)
      printf 'amd64\n'
      ;;
    arm64|aarch64)
      printf 'arm64\n'
      ;;
    *)
      echo "Unsupported architecture: ${arch}. Supported: amd64, arm64." >&2
      exit 1
      ;;
  esac
}

main() {
  local os arch asset_name asset_url tmp_file

  os="$(detect_os)"
  arch="$(detect_arch)"
  asset_name="docker-ztd-${os}-${arch}"
  asset_url="https://github.com/${REPO}/releases/latest/download/${asset_name}"
  tmp_file="$(mktemp)"

  echo "Installing docker-ztd for ${os}/${arch}..."
  mkdir -p "${PLUGIN_DIR}"

  if command -v curl >/dev/null 2>&1; then
    curl -fL "${asset_url}" -o "${tmp_file}"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "${tmp_file}" "${asset_url}"
  else
    echo "Neither curl nor wget is available. Install one of them and retry." >&2
    exit 1
  fi

  mv "${tmp_file}" "${PLUGIN_PATH}"
  chmod +x "${PLUGIN_PATH}"

  echo "Installed: ${PLUGIN_PATH}"
  echo "Run: docker ztd --help"
}

main "$@"
