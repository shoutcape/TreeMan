#!/usr/bin/env bash
# TreeMan installer
# Usage: curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash

set -e

REPO_URL="${TREEMAN_REPO_URL:-https://raw.githubusercontent.com/shoutcape/TreeMan/main/wt.sh}"
INSTALL_DIR="${TREEMAN_INSTALL_DIR:-$HOME/.treeman}"
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

# --- Download wt.sh ----------------------------------------------------------

print_step "Installing TreeMan to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$REPO_URL" -o "$WT_SH_FILE"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$WT_SH_FILE" "$REPO_URL"
else
  echo "Error: curl or wget is required to install TreeMan." >&2
  exit 1
fi

print_done

# --- Add source line to shell config -----------------------------------------

SOURCE_LINE="source \"$WT_SH_FILE\""
SOURCE_MARKER="# TreeMan"

print_step "Adding TreeMan to $SHELL_RC..."

if grep -qF "$SOURCE_MARKER" "$SHELL_RC" 2>/dev/null; then
  print_warn "TreeMan source line already present in $SHELL_RC, skipping."
else
  printf '\n%s\n%s\n' "$SOURCE_MARKER" "$SOURCE_LINE" >> "$SHELL_RC"
  print_done
fi

# --- Check optional dependencies --------------------------------------------

if ! command -v fzf >/dev/null 2>&1; then
  print_warn "fzf is not installed. The 'wts' and 'wtd' commands require it."
  echo "    Install it from: https://github.com/junegunn/fzf"
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
    command: "bash -lc 'source \"$WT_SH_FILE\" && wt \"\$1\"' treeman {{.Form.BranchName | quote}}"
    loadingText: 'Creating worktree...'
    prompts:
      - type: 'input'
        title: 'New branch name:'
        key: 'BranchName'
  - key: 'D' # TreeMan
    description: 'Delete worktree and branch (TreeMan)'
    context: 'worktrees'
    output: logWithPty
    command: "bash -lc 'source \"$WT_SH_FILE\" && _wt_lazygit_delete_worktree \"\$1\" \"\$2\"' treeman {{.SelectedWorktree.Path | quote}} {{.SelectedWorktree.Branch | quote}}"
    loadingText: 'Removing worktree...'
    prompts:
      - type: 'confirm'
        title: 'Delete worktree and branch?'
        body: 'This will remove the worktree at {{.SelectedWorktree.Path}} and delete branch "{{.SelectedWorktree.Branch}}". Continue?'
  - key: 'D' # TreeMan
    description: 'Delete worktree and branch (TreeMan)'
    context: 'localBranches'
    output: logWithPty
    command: "bash -lc 'source \"$WT_SH_FILE\" && _wt_lazygit_delete_branch \"\$1\"' treeman {{.SelectedLocalBranch.Name | quote}}"
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
echo "  wts [query]          Switch between worktrees (requires fzf)"
echo "  wtd [query]          Delete a worktree and its branch (requires fzf)"
echo "  lg                   Run lazygit with auto-cd"
