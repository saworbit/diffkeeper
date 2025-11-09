#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 4 ]]; then
  echo "usage: $0 <distro> <version> <kernel-release> <arch> [output-dir]" >&2
  echo "example: $0 ubuntu 22.04 5.15.0-92-generic x86_64 ./btf-cache" >&2
  exit 1
fi

DISTRO="$1"
VERSION="$2"
KREL="$3"
ARCH="$4"
OUT="${5:-btf-cache}"
BASE_URL="${BTFHUB_BASE:-https://github.com/aquasecurity/btfhub-archive/raw/main}"

mkdir -p "${OUT}"
TARBALL="${OUT}/${KREL}.btf.tar.xz"
BTFFILE="${OUT}/${KREL}.btf"

echo "[btfhub] downloading ${DISTRO}/${VERSION}/${ARCH}/${KREL}"
curl -fsSL "${BASE_URL}/${DISTRO}/${VERSION}/${ARCH}/${KREL}.btf.tar.xz" -o "${TARBALL}"

echo "[btfhub] extracting ${BTFFILE}"
tar -xf "${TARBALL}" -C "${OUT}"
rm -f "${TARBALL}"

echo "[btfhub] saved ${BTFFILE}"
