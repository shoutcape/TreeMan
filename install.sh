#!/usr/bin/env bash
# TreeMan installer
# Usage: curl -fsSL https://raw.githubusercontent.com/you/TreeMan/main/install.sh | bash

set -e

REPO_URL="https://raw.githubusercontent.com/shoutcape/TreeMan/main/wt.sh"
INSTALL_DIR="$HOME/.treeman"
WTP_FILE="$INSTALL_DIR/wt.sh"

# --- Helpers -----------------------------------------------------------------

print_step() { echo "==> $1"; }
print_done() { echo "    done."; }
print_warn() { echo "    warning: $1"; }

if [[ "$1" == "uninstall" ]] || [[ "$1" == "--uninstall" ]]; then
  SOURCE_MARKER="# TreeMan"

  remove_from_rc() {
    local rc_file="$1"
    if [[ ! -f "$rc_file" ]]; then
      return
    fi
    if grep -qF "$SOURCE_MARKER" "$rc_file" 2>/dev/null; then
      print_step "Removing TreeMan from $rc_file..."
      grep -v -A1 "^$SOURCE_MARKER$" "$rc_file" | grep -v "^--$" > "${rc_file}.tmp" && mv "${rc_file}.tmp" "$rc_file"
      print_done
    fi
  }

  remove_from_rc "$HOME/.zshrc"
  remove_from_rc "$HOME/.bashrc"
  remove_from_rc "$HOME/.bash_profile"

  print_step "Removing 'git wt' alias..."
  if git config --global --get alias.wt >/dev/null 2>&1; then
    git config --global --unset alias.wt
    print_done
  else
    print_warn "'git wt' alias not found, skipping."
  fi

  print_step "Removing $INSTALL_DIR..."
  if [[ -d "$INSTALL_DIR" ]]; then
    rm -rf "$INSTALL_DIR"
    print_done
  else
    print_warn "$INSTALL_DIR not found, skipping."
  fi

  echo ""
  echo "TreeMan uninstalled."
  
  if command -v lazygit >/dev/null 2>&1; then
    config_dir=$(lazygit -cd 2>/dev/null)
    if [[ -n "$config_dir" ]]; then
      config_file="$config_dir/config.yml"
      if [[ -f "$config_file" ]] && grep -q "$SOURCE_MARKER" "$config_file" 2>/dev/null; then
        print_step "Removing lazygit integration..."
        tmp=$(mktemp)
        awk -v marker="$SOURCE_MARKER" '
          BEGIN { skipping = 0 }
          index($0, marker) { skipping = 1; next }
          skipping {
            if (/^  - / || /^[a-zA-Z]/) { skipping = 0 }
            else { next }
          }
          { print }
        ' "$config_file" > "$tmp"

        awk '
          {
            lines[NR] = $0
            original[NR] = $0
            suppress[NR] = 0
          }
          END {
            for (i = 1; i <= NR; i++) {
              if (lines[i] ~ /^customCommands:$/) {
                j = i + 1
                while (j <= NR && lines[j] ~ /^[[:space:]]*$/) j++
                if (j > NR || lines[j] ~ /^[a-zA-Z]/) {
                  for (k = i; k < j; k++) suppress[k] = 1
                  if (i > 1 && lines[i-1] ~ /# TreeMan/) {
                    suppress[i-1] = 1
                    if (i > 2 && lines[i-2] ~ /^[[:space:]]*$/) suppress[i-2] = 1
                  }
                }
              }
            }
            for (i = 1; i <= NR; i++) {
              if (!suppress[i]) print lines[i]
            }
          }
        ' "$tmp" > "${tmp}.2"

        awk 'NF { last = NR } { lines[NR] = $0 } END { for (i = 1; i <= last; i++) print lines[i] }' "${tmp}.2" > "${tmp}.3"
        { cat "${tmp}.3"; echo; } > "$config_file"
        rm -f "$tmp" "${tmp}.2" "${tmp}.3" 2>/dev/null
        print_done
      fi
    fi
  fi

  echo "Reload your shell to complete removal:"
  echo "  exec \$SHELL"
  exit 0
fi

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

SHELL_RC="$(detect_shell_rc)"

# --- Download wt.sh ----------------------------------------------------------

print_step "Installing TreeMan to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$REPO_URL" -o "$WTP_FILE"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$WTP_FILE" "$REPO_URL"
else
  echo "Error: curl or wget is required to install TreeMan." >&2
  exit 1
fi

print_done

# --- Add source line to shell config -----------------------------------------

SOURCE_LINE="source \"$WTP_FILE\""
SOURCE_MARKER="# TreeMan"

print_step "Adding TreeMan to $SHELL_RC..."

if grep -qF "$SOURCE_MARKER" "$SHELL_RC" 2>/dev/null; then
  print_warn "TreeMan source line already present in $SHELL_RC, skipping."
else
  printf '\n%s\n%s\n' "$SOURCE_MARKER" "$SOURCE_LINE" >> "$SHELL_RC"
  print_done
fi

# --- Set up git alias --------------------------------------------------------

print_step "Registering 'git wt' alias..."
git config --global alias.wt '!wt'
print_done

# --- Check optional dependencies --------------------------------------------

if ! command -v fzf >/dev/null 2>&1; then
  print_warn "fzf is not installed. The 'wts' command (interactive worktree switcher) requires it."
  echo "    Install it from: https://github.com/junegunn/fzf"
fi

# --- Lazygit integration -----------------------------------------------------

if command -v lazygit >/dev/null 2>&1; then
  print_step "Checking lazygit integration..."
  config_dir=$(lazygit -cd 2>/dev/null) || true
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
      ENTRY="  - key: 'W' # TreeMan
    description: 'Create new worktree (TreeMan)'
    context: 'localBranches'
    output: logWithPty
    command: \"bash -c 'source ~/.treeman/wt.sh && wt {{.Form.BranchName | quote}}'\"
    loadingText: 'Creating worktree...'
    prompts:
      - type: 'input'
        title: 'New branch name:'
        key: 'BranchName'
  - key: 'D' # TreeMan
    description: 'Delete worktree and branch (TreeMan)'
    context: 'worktrees'
    output: logWithPty
    command: \"bash -c '[ {{.SelectedWorktree.IsMain}} = true ] && echo \\\"Error: cannot delete the main worktree.\\\" && exit 1; git worktree remove {{.SelectedWorktree.Path | quote}} && git branch -D {{.SelectedWorktree.Branch | quote}}'\"
    loadingText: 'Removing worktree...'
    prompts:
      - type: 'confirm'
        title: 'Delete worktree and branch?'
        body: 'This will remove the worktree at {{.SelectedWorktree.Path}} and delete branch {{.SelectedWorktree.Branch}}. Continue?'
  - key: 'D' # TreeMan
    description: 'Delete worktree and branch (TreeMan)'
    context: 'localBranches'
    output: logWithPty
    command: \"bash -c 'branch={{.SelectedLocalBranch.Name | quote}}; wt_path=\$(git worktree list --porcelain | grep -B2 \\\"branch refs/heads/\$branch\\\" | head -1 | sed \\\"s/^worktree //\\\"); [ -n \\\"\$wt_path\\\" ] && git worktree remove \\\"\$wt_path\\\"; git branch -D \\\"\$branch\\\"'\"
    loadingText: 'Removing worktree...'
    prompts:
      - type: 'confirm'
        title: 'Delete worktree and branch?'
        body: 'This will remove the worktree and delete branch {{.SelectedLocalBranch.Name}}. Continue?'"

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
echo "  git wt <branch-name> "
echo "  lg                   Run lazygit with auto-cd"
