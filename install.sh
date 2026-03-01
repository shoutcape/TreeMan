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

# --- Final message -----------------------------------------------------------

echo ""
echo "TreeMan installed successfully."
echo ""
echo "Reload your shell to start using it:"
echo "  source $SHELL_RC"
echo ""
echo "Usage:"
echo "  wt <branch-name>"
echo "  git wt <branch-name>"
