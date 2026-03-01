#!/usr/bin/env bash
# TreeMan uninstaller

set -e

INSTALL_DIR="$HOME/.treeman"
SOURCE_MARKER="# TreeMan"

print_step() { echo "==> $1"; }
print_done() { echo "    done."; }
print_warn() { echo "    warning: $1"; }

# --- Remove source lines from shell configs ----------------------------------

remove_from_rc() {
  local rc_file="$1"
  if [[ ! -f "$rc_file" ]]; then
    return
  fi
  if grep -qF "$SOURCE_MARKER" "$rc_file" 2>/dev/null; then
    print_step "Removing TreeMan from $rc_file..."
    # Remove the marker line and the source line that follows it
    grep -v -A1 "^$SOURCE_MARKER$" "$rc_file" | grep -v "^--$" > "${rc_file}.tmp" && mv "${rc_file}.tmp" "$rc_file"
    print_done
  fi
}

remove_from_rc "$HOME/.zshrc"
remove_from_rc "$HOME/.bashrc"
remove_from_rc "$HOME/.bash_profile"

# --- Remove git alias --------------------------------------------------------

print_step "Removing 'git wt' alias..."
if git config --global --get alias.wt >/dev/null 2>&1; then
  git config --global --unset alias.wt
  print_done
else
  print_warn "'git wt' alias not found, skipping."
fi

# --- Remove install directory ------------------------------------------------

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
