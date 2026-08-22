#!/usr/bin/env bash
# Build and install the TreeMan version from this worktree.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

make -C "$SCRIPT_DIR" build
TREEMAN_LOCAL_BIN="$SCRIPT_DIR/bin/treeman" "$SCRIPT_DIR/install.sh"
