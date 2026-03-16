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
  else
    echo "bash"
  fi
}

SHELL_RC="${TREEMAN_SHELL_RC:-$(detect_shell_rc)}"
SHELL_NAME=$(detect_shell_name)

detect_lazygit_config_dir() {
  if [[ -n "${TREEMAN_LAZYGIT_CONFIG_DIR:-}" ]]; then
    echo "$TREEMAN_LAZYGIT_CONFIG_DIR"
  elif command -v lazygit >/dev/null 2>&1; then
    lazygit -cd 2>/dev/null || true
  fi
}

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

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/$TARBALL"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TMP_DIR/$TARBALL" "$DOWNLOAD_URL"
  else
    echo "Error: curl or wget is required to install TreeMan." >&2
    exit 1
  fi

  tar -xzf "$TMP_DIR/$TARBALL" -C "$TMP_DIR"
  install -m 755 "$TMP_DIR/treeman" "$BINARY"
fi

print_done

# --- Add PATH + eval line to shell config ------------------------------------

SOURCE_MARKER="# TreeMan"
PATH_LINE="export PATH=\"${BIN_DIR}:\$PATH\""
EVAL_LINE="eval \"\$(treeman init ${SHELL_NAME})\""

print_step "Adding TreeMan to $SHELL_RC..."

if grep -qF "$SOURCE_MARKER" "$SHELL_RC" 2>/dev/null; then
  print_warn "TreeMan already present in $SHELL_RC, skipping."
else
  printf '\n%s\n%s\n%s\n' "$SOURCE_MARKER" "$PATH_LINE" "$EVAL_LINE" >> "$SHELL_RC"
  print_done
fi

# --- Check optional dependencies --------------------------------------------

if ! command -v fzf >/dev/null 2>&1; then
  print_warn "fzf is not installed. The 'wts' and 'wtd' commands require it."
  echo "    Install it from: https://github.com/junegunn/fzf"
fi

if ! command -v gh >/dev/null 2>&1; then
  print_warn "gh is not installed. The 'wtpr' and 'wtmr' commands require it for GitHub repos."
  echo "    Install it from: https://cli.github.com/"
fi

# --- Lazygit integration -----------------------------------------------------

if command -v lazygit >/dev/null 2>&1 || [[ -n "${TREEMAN_LAZYGIT_CONFIG_DIR:-}" ]]; then
  print_step "Checking lazygit integration..."
  config_dir=$(detect_lazygit_config_dir)
  if [[ -n "$config_dir" ]]; then
    config_file="$config_dir/config.yml"

    if [ ! -f "$config_file" ]; then
      mkdir -p "$config_dir"
      touch "$config_file"
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
    command: "treeman create {{.Form.BranchName | quote}}"
    loadingText: 'Creating worktree...'
    prompts:
      - type: 'input'
        title: 'New branch name:'
        key: 'BranchName'
  - key: 'D' # TreeMan
    description: 'Delete worktree and branch (TreeMan)'
    context: 'worktrees'
    output: logWithPty
    command: "treeman delete --path {{.SelectedWorktree.Path | quote}} --branch {{.SelectedWorktree.Branch | quote}} --yes"
    loadingText: 'Removing worktree...'
    prompts:
      - type: 'confirm'
        title: 'Delete worktree and branch?'
        body: 'This will remove the worktree at {{.SelectedWorktree.Path}} and delete branch "{{.SelectedWorktree.Branch}}". Continue?'
  - key: 'D' # TreeMan
    description: 'Delete worktree and branch (TreeMan)'
    context: 'localBranches'
    output: logWithPty
    command: "treeman delete --branch {{.SelectedLocalBranch.Name | quote}} --yes"
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
echo "  wt  <branch-name>    Create a new worktree + branch"
echo "  wtpr [pr-number]     Create a review worktree from a GitHub PR"
echo "  wtmr [pr-number]     Create a review worktree from a GitLab MR"
echo "  wts  [query]         Switch between worktrees (requires fzf)"
echo "  wtd  [query]         Delete a worktree and its branch (requires fzf)"
echo "  lg                   Run lazygit with auto-cd"
