#!/bin/bash
set -euo pipefail

SEALOS_VERSION=${SEALOS_VERSION:-5.0.1}
SREG_VERSION=${SREG_VERSION:-0.1.7-rc3}
INSTALL_DIR=${INSTALL_DIR:-/usr/local/bin}

download_with_retries() {
  local url="$1"
  local output="$2"
  for _ in 1 2 3 4 5; do
    if curl -fsSL --retry 3 --retry-delay 2 -o "${output}" "${url}"; then
      return 0
    fi
    sleep 3
  done
  return 1
}

install_sealos() {
  if command -v sealos >/dev/null 2>&1; then
    sealos version
    return
  fi

  local archive="sealos_${SEALOS_VERSION}_linux_amd64.tar.gz"
  local url="https://github.com/labring/sealos/releases/download/v${SEALOS_VERSION}/${archive}"
  download_with_retries "${url}" "${archive}"
  tar -zxf "${archive}" sealos
  install -m 0755 sealos "${INSTALL_DIR}/sealos"
  rm -f sealos "${archive}"
  sealos version
}

install_sreg() {
  if command -v sreg >/dev/null 2>&1; then
    sreg version
    return
  fi

  local archive="sreg_${SREG_VERSION}_linux_amd64.tar.gz"
  local url="https://github.com/labring/sreg/releases/download/v${SREG_VERSION}/${archive}"
  download_with_retries "${url}" "${archive}"
  tar -zxf "${archive}" sreg
  install -m 0755 sreg "${INSTALL_DIR}/sreg"
  rm -f sreg "${archive}"
  sreg version
}

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT
cd "${workdir}"

install_sealos
install_sreg
