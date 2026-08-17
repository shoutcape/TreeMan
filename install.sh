#!/usr/bin/env bash
# TreeMan installer
# Usage: curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash

set -e

REPO_OWNER="shoutcape"
REPO_NAME="TreeMan"
RELEASE_BASE="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest/download"
INSTALL_DIR="${TREEMAN_INSTALL_DIR:-$HOME/.treeman}"
BIN_DIR="$INSTALL_DIR/bin"
BINARY="$BIN_DIR/treeman"

# --- Helpers -----------------------------------------------------------------

print_step() { echo "==> $1"; }
print_done() { echo "    done."; }
print_warn() { echo "    warning: $1"; }

# --- Detect OS and architecture ----------------------------------------------

detect_platform() {
  local os arch

  case "$(uname -s)" in
    Linux)  os="linux"  ;;
    Darwin) os="darwin" ;;
    *)
      echo "Error: unsupported OS '$(uname -s)'." >&2
      echo "       Build from source: go install github.com/${REPO_OWNER}/treeman/cmd/treeman@latest" >&2
      exit 1
      ;;
  esac

  case "$(uname -m)" in
    x86_64)          arch="amd64" ;;
    aarch64 | arm64) arch="arm64" ;;
    *)
      echo "Error: unsupported architecture '$(uname -m)'." >&2
      echo "       Build from source: go install github.com/${REPO_OWNER}/treeman/cmd/treeman@latest" >&2
      exit 1
      ;;
  esac

  echo "${os}_${arch}"
}

# --- Detect shell config file ------------------------------------------------

detect_shell_rc() {
  if [[ -n "$ZSH_VERSION" ]] || [[ "$SHELL" == */zsh ]]; then
    echo "$HOME/.zshrc"
  elif [[ "$SHELL" == */fish ]]; then
    echo "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
  elif [[ -n "$BASH_VERSION" ]] || [[ "$SHELL" == */bash ]]; then
    if [[ -f "$HOME/.bashrc" ]]; then
      echo "$HOME/.bashrc"
    else
      echo "$HOME/.bash_profile"
    fi
  else
    # Fallback: check for common rc files
    if [[ -f "$HOME/.zshrc" ]]; then
      echo "$HOME/.zshrc"
    else
      echo "$HOME/.bashrc"
    fi
  fi
}

detect_shell_name() {
  if [[ -n "$ZSH_VERSION" ]] || [[ "$SHELL" == */zsh ]]; then
    echo "zsh"
  elif [[ "$SHELL" == */fish ]]; then
    echo "fish"
  else
    echo "bash"
  fi
}

SHELL_RC="${TREEMAN_SHELL_RC:-$(detect_shell_rc)}"
SHELL_NAME=$(detect_shell_name)

download_file() {
  local url="$1"
  local destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$destination"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$destination" "$url"
  else
    echo "Error: curl or wget is required to install TreeMan." >&2
    exit 1
  fi
}

verify_checksum() {
  local archive="$1"
  local checksums="$2"
  local expected
  local actual

  expected=$(awk -v archive="$(basename "$archive")" '$2 == archive { print $1; exit }' "$checksums")
  if [[ -z "$expected" ]]; then
    echo "Error: checksum for $(basename "$archive") was not found." >&2
    exit 1
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$archive" | awk '{ print $1 }')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$archive" | awk '{ print $1 }')
  else
    echo "Error: sha256sum or shasum is required to verify the download." >&2
    exit 1
  fi

  if [[ "$actual" != "$expected" ]]; then
    echo "Error: checksum verification failed for $(basename "$archive")." >&2
    exit 1
  fi
}

if [[ "${TREEMAN_INSTALLER_LIB_ONLY:-}" == "1" ]]; then
  return 0
fi

# --- Download binary ----------------------------------------------------------

print_step "Installing TreeMan to $BIN_DIR..."
mkdir -p "$BIN_DIR"

if [[ -n "${TREEMAN_LOCAL_BIN:-}" ]]; then
  # Local install path: skip download, copy binary directly (used in tests and
  # local builds: TREEMAN_LOCAL_BIN=/path/to/treeman ./install.sh)
  install -m 755 "$TREEMAN_LOCAL_BIN" "$BINARY"
else
  PLATFORM=$(detect_platform)
  TARBALL="treeman_${PLATFORM}.tar.gz"
  DOWNLOAD_URL="${RELEASE_BASE}/${TARBALL}"

  TMP_DIR=$(mktemp -d)
  trap 'rm -rf "$TMP_DIR"' EXIT

  download_file "$DOWNLOAD_URL" "$TMP_DIR/$TARBALL"
  download_file "${RELEASE_BASE}/treeman_checksums.txt" "$TMP_DIR/treeman_checksums.txt"
  verify_checksum "$TMP_DIR/$TARBALL" "$TMP_DIR/treeman_checksums.txt"

  tar -xzf "$TMP_DIR/$TARBALL" -C "$TMP_DIR"
  install -m 755 "$TMP_DIR/treeman" "$BINARY"
fi

print_done

# --- Add PATH + eval line to shell config ------------------------------------

SOURCE_MARKER="# TreeMan"
PATH_LINE="export PATH=\"${BIN_DIR}:\$PATH\""
EVAL_LINE="eval \"\$(treeman init ${SHELL_NAME})\""
if [[ "$SHELL_NAME" == "fish" ]]; then
  PATH_LINE="set -gx PATH \"${BIN_DIR}\" \$PATH"
  EVAL_LINE="treeman init fish | source"
fi

print_step "Adding TreeMan to $SHELL_RC..."

if grep -qF "$SOURCE_MARKER" "$SHELL_RC" 2>/dev/null; then
  print_warn "TreeMan already present in $SHELL_RC, skipping."
else
  mkdir -p "$(dirname "$SHELL_RC")"
  touch "$SHELL_RC"
  printf '\n%s\n%s\n%s\n' "$SOURCE_MARKER" "$PATH_LINE" "$EVAL_LINE" >> "$SHELL_RC"
  print_done
fi

# --- Check optional dependencies --------------------------------------------

if ! command -v fzf >/dev/null 2>&1; then
  print_warn "fzf is not installed. The 'wts' and 'wtd' commands require it."
  echo "    Install it from: https://github.com/junegunn/fzf"
fi

if ! command -v gh >/dev/null 2>&1; then
  print_warn "gh is not installed. The 'wtb', 'wtpr', and 'wtmr' commands require it for GitHub repos."
  echo "    Install it from: https://cli.github.com/"
fi

# --- Final message -----------------------------------------------------------

echo ""
echo "TreeMan installed successfully."
echo ""
echo "Reload your shell to start using it:"
echo "  source $SHELL_RC"
echo ""
echo "Usage:"
echo "  wt  <branch-name>    Create a new worktree + branch"
echo "  wtb [query]          Check out a remote branch into a worktree"
echo "  wtpr [pr-number]     Create a review worktree from a GitHub PR"
echo "  wtmr [pr-number]     Create a review worktree from a GitLab MR"
echo "  wts  [query]         Switch between worktrees (requires fzf)"
echo "  wtl                  List worktrees and their state"
echo "  wtd  [query]         Delete a worktree and its branch (requires fzf)"
