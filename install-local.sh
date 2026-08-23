#!/usr/bin/env bash
# Build and install the TreeMan version from this worktree.

set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

homebrew_treeman_prefix() {
  local prefix
  if command -v brew >/dev/null 2>&1 && prefix=$(brew --prefix treeman 2>/dev/null) && [[ -x "$prefix/bin/treeman" ]]; then
    printf '%s\n' "$prefix"
    return 0
  fi

  return 1
}

local_install_dir() {
  if [[ -n "${TREEMAN_INSTALL_DIR:-}" ]]; then
    printf '%s\n' "$TREEMAN_INSTALL_DIR"
    return
  fi

  local brew_prefix existing binary_dir
  if brew_prefix=$(homebrew_treeman_prefix); then
    printf '%s\n' "$brew_prefix"
    return
  fi

  existing=$(type -P treeman || true)
  if [[ -n "$existing" ]]; then
    binary_dir=$(dirname "$existing")
    if [[ "$(basename "$binary_dir")" == "bin" ]]; then
      dirname "$binary_dir"
      return
    fi
  fi

  printf '%s\n' "$HOME/.treeman"
}

if [[ "${TREEMAN_LOCAL_INSTALLER_LIB_ONLY:-}" == "1" ]]; then
  return 0
fi

make -C "$SCRIPT_DIR" build
install_dir=$(local_install_dir)
if [[ -z "${TREEMAN_INSTALL_DIR:-}" ]] && homebrew_prefix=$(homebrew_treeman_prefix); then
  TREEMAN_LOCAL_BIN="$SCRIPT_DIR/bin/treeman" TREEMAN_INSTALL_DIR="$homebrew_prefix" TREEMAN_SKIP_PATH_SETUP=1 "$SCRIPT_DIR/install.sh"
else
  TREEMAN_LOCAL_BIN="$SCRIPT_DIR/bin/treeman" TREEMAN_INSTALL_DIR="$install_dir" "$SCRIPT_DIR/install.sh"
fi
