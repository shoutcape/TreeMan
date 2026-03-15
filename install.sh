#!/usr/bin/env bash
# TreeMan installer
# Usage: curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash

set -e

REPO="shoutcape/TreeMan"
INSTALL_DIR="${TREEMAN_INSTALL_DIR:-$HOME/.treeman}"
BIN_FILE="$INSTALL_DIR/treeman"
WT_SH_URL="${TREEMAN_WT_SH_URL:-https://raw.githubusercontent.com/$REPO/main/wt.sh}"
WT_SH_FILE="$INSTALL_DIR/wt.sh"

# --- Helpers -----------------------------------------------------------------

print_step() { echo "==> $1"; }
print_done() { echo "    done."; }
print_warn() { echo "    warning: $1"; }

# --- Detect shell config file ------------------------------------------------

detect_shell_rc() {
  if [[ -n "$ZSH_VERSION" ]] || [[ "$SHELL" == */zsh ]]; then
    echo "$HOME/.zshrc"
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

SHELL_RC="${TREEMAN_SHELL_RC:-$(detect_shell_rc)}"

detect_lazygit_config_dir() {
  if [[ -n "${TREEMAN_LAZYGIT_CONFIG_DIR:-}" ]]; then
    echo "$TREEMAN_LAZYGIT_CONFIG_DIR"
  elif command -v lazygit >/dev/null 2>&1; then
    lazygit -cd 2>/dev/null || true
  fi
}

# --- Detect OS and architecture ----------------------------------------------

detect_platform() {
  local os arch

  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$os" in
    linux)  os="linux" ;;
    darwin) os="darwin" ;;
    *)
      echo "Error: unsupported operating system: $os" >&2
      exit 1
      ;;
  esac

  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *)
      echo "Error: unsupported architecture: $arch" >&2
      exit 1
      ;;
  esac

  echo "${os}_${arch}"
}

# --- Download function -------------------------------------------------------

download() {
  local url="$1" dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    echo "Error: curl or wget is required to install TreeMan." >&2
    exit 1
  fi
}

# --- Download binary ---------------------------------------------------------

mkdir -p "$INSTALL_DIR"

if [[ -n "${TREEMAN_BIN_PATH:-}" ]]; then
  # Local install mode (for testing): copy binary from a local path
  print_step "Installing TreeMan binary from $TREEMAN_BIN_PATH..."
  cp "$TREEMAN_BIN_PATH" "$BIN_FILE"
  chmod +x "$BIN_FILE"
else
  PLATFORM=$(detect_platform)
  RELEASE_URL="https://github.com/$REPO/releases/latest/download/treeman_${PLATFORM}.tar.gz"

  print_step "Downloading TreeMan for $PLATFORM..."
  TMP_TAR=$(mktemp)
  download "$RELEASE_URL" "$TMP_TAR"

  tar -xzf "$TMP_TAR" -C "$INSTALL_DIR" treeman 2>/dev/null || {
    # If tarball extraction fails, try direct binary download
    mv "$TMP_TAR" "$BIN_FILE"
  }
  rm -f "$TMP_TAR"
  chmod +x "$BIN_FILE"
fi
print_done

# --- Download wt.sh ----------------------------------------------------------

print_step "Downloading shell adapter (wt.sh)..."
if [[ -n "${TREEMAN_WT_SH_PATH:-}" ]]; then
  cp "$TREEMAN_WT_SH_PATH" "$WT_SH_FILE"
else
  download "$WT_SH_URL" "$WT_SH_FILE"
fi
print_done

# --- Add to shell config -----------------------------------------------------

SOURCE_MARKER="# TreeMan"
PATH_LINE="export PATH=\"$INSTALL_DIR:\$PATH\""
SOURCE_LINE="source \"$WT_SH_FILE\""

rewrite_tree_man_block() {
  local rc_file="$1"
  local tmp

  mkdir -p "$(dirname "$rc_file")"
  touch "$rc_file"

  tmp=$(mktemp)
  awk -v marker="$SOURCE_MARKER" '
    $0 == marker { skip = 2; next }
    skip > 0 { skip--; next }
    { print }
  ' "$rc_file" > "$tmp"
  mv "$tmp" "$rc_file"

  printf '\n%s\n%s\n%s\n' "$SOURCE_MARKER" "$PATH_LINE" "$SOURCE_LINE" >> "$rc_file"
}

print_step "Adding TreeMan to $SHELL_RC..."

if grep -qF "$SOURCE_MARKER" "$SHELL_RC" 2>/dev/null; then
	if grep -qFx "$PATH_LINE" "$SHELL_RC" 2>/dev/null && grep -qFx "$SOURCE_LINE" "$SHELL_RC" 2>/dev/null; then
	  print_warn "TreeMan shell setup already present in $SHELL_RC, skipping."
	else
	  print_step "Repairing TreeMan shell setup in $SHELL_RC..."
	  rewrite_tree_man_block "$SHELL_RC"
	  print_done
	fi
else
	rewrite_tree_man_block "$SHELL_RC"
	print_done
fi

# --- Check optional dependencies --------------------------------------------

if ! command -v fzf >/dev/null 2>&1; then
  print_warn "fzf is not installed. The 'wts' and 'wtd' commands require it."
  echo "    Install it from: https://github.com/junegunn/fzf"
fi

if ! command -v gh >/dev/null 2>&1; then
  print_warn "gh is not installed. The 'wtpr' and 'wtmr' commands require it."
  echo "    Install it from: https://cli.github.com/"
fi

# --- Lazygit integration -----------------------------------------------------

if command -v lazygit >/dev/null 2>&1 || [[ -n "${TREEMAN_LAZYGIT_CONFIG_DIR:-}" ]]; then
  print_step "Checking lazygit integration..."
  config_dir=$(detect_lazygit_config_dir)
  if [[ -n "$config_dir" ]]; then
    config_file="$config_dir/config.yml"
    
    # Create config file if it doesn't exist
    if [ ! -f "$config_file" ]; then
      mkdir -p "$config_dir"
      touch "$config_file"
      print_done
    fi

    if grep -q "$SOURCE_MARKER" "$config_file" 2>/dev/null; then
      print_warn "TreeMan lazygit integration is already installed. Skipping."
    else
      print_step "Installing lazygit integration..."
      ENTRY=$(cat <<EOF
  - key: 'W' # TreeMan
    description: 'Create new worktree (TreeMan)'
    context: 'localBranches'
    output: logWithPty
    command: "treeman worktree create {{.Form.BranchName | quote}}"
    loadingText: 'Creating worktree...'
    prompts:
      - type: 'input'
        title: 'New branch name:'
        key: 'BranchName'
  - key: 'D' # TreeMan
    description: 'Delete worktree and branch (TreeMan)'
    context: 'worktrees'
    output: logWithPty
    command: "treeman worktree delete --path {{.SelectedWorktree.Path | quote}} 2>&1"
    loadingText: 'Removing worktree...'
    prompts:
      - type: 'confirm'
        title: 'Delete worktree and branch?'
        body: 'This will remove the worktree at {{.SelectedWorktree.Path}} and delete branch "{{.SelectedWorktree.Branch}}". Continue?'
  - key: 'D' # TreeMan
    description: 'Delete worktree and branch (TreeMan)'
    context: 'localBranches'
    output: logWithPty
    command: "treeman worktree delete --branch {{.SelectedLocalBranch.Name | quote}} 2>&1"
    loadingText: 'Removing worktree...'
    prompts:
      - type: 'confirm'
        title: 'Delete worktree and branch?'
        body: 'This will remove the worktree and delete branch "{{.SelectedLocalBranch.Name}}". Continue?'
EOF
)

      if ! grep -q '^customCommands:' "$config_file"; then
        tmp=$(mktemp)
        grep -v '^{}$' "$config_file" > "$tmp" && mv "$tmp" "$config_file"
        cat >> "$config_file" << EOF

# TreeMan — worktree keybindings (W: create, D: delete)
customCommands:
$ENTRY
EOF
      elif grep -q '^customCommands: \[\]' "$config_file"; then
        tmp=$(mktemp)
        awk -v entry="$ENTRY" '
          /^customCommands: \[\]/ { print "customCommands:\n" entry; next }
          { print }
        ' "$config_file" > "$tmp" && mv "$tmp" "$config_file"
      else
        tmp=$(mktemp)
        awk -v entry="$ENTRY" '
          BEGIN { in_custom = 0; done = 0 }
          /^customCommands:/ { in_custom = 1; print; next }
          in_custom && !done && /^[a-zA-Z]/ { print entry "\n"; in_custom = 0; done = 1 }
          { print }
          END { if (in_custom && !done) { print "\n" entry } }
        ' "$config_file" > "$tmp" && mv "$tmp" "$config_file"
      fi
      print_done
    fi
  fi
else
  print_warn "lazygit is not installed. Skipping lazygit integration."
fi

# --- Final message -----------------------------------------------------------

echo ""
echo "TreeMan installed successfully."
echo ""
echo "Reload your shell to start using it:"
echo "  source $SHELL_RC"
echo ""
echo "Usage:"
echo "  wt  <branch-name>   Create a new worktree + branch"
echo "  wtpr [pr-number]    Create a review worktree from a PR"
echo "  wtmr [pr-number]    Same as wtpr"
echo "  wts [query]          Switch between worktrees (requires fzf)"
echo "  wtd [query]          Delete a worktree and its branch (requires fzf)"
echo "  lg                   Run lazygit with auto-cd"
echo ""
echo "  treeman runtime up   Start dev server with isolated ports"
echo "  treeman runtime down Stop dev server"
echo "  treeman init         Generate .treeman.yml config"
