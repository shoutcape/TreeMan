#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

archive="$TMP_DIR/treeman_linux_amd64.tar.gz"
checksums="$TMP_DIR/treeman_checksums.txt"
printf 'release archive\n' > "$archive"
checksum=$(sha256sum "$archive" | awk '{ print $1 }')
printf '%s  %s\n' "$checksum" "$(basename "$archive")" > "$checksums"

run_verify() {
  TREEMAN_INSTALLER_LIB_ONLY=1 bash -c 'source "$1"; verify_checksum "$2" "$3"' _ \
    "$SCRIPT_DIR/install.sh" "$archive" "$checksums"
}

run_verify

printf 'wrong-checksum  %s\n' "$(basename "$archive")" > "$checksums"
if run_verify >/dev/null 2>&1; then
  echo "checksum mismatch unexpectedly passed" >&2
  exit 1
fi

printf 'deadbeef  other.tar.gz\n' > "$checksums"
if run_verify >/dev/null 2>&1; then
  echo "missing checksum entry unexpectedly passed" >&2
  exit 1
fi

echo "PASS"
