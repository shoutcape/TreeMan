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
echo "  git wt <branch-name> "
