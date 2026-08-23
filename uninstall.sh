#!/usr/bin/env bash
# TreeMan uninstaller
# Usage: curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash

set -e

INSTALL_DIR="${TREEMAN_INSTALL_DIR:-$HOME/.treeman}"
SOURCE_MARKER="# TreeMan"

print_step() { echo "==> $1"; }
print_done() { echo "    done."; }
print_warn() { echo "    warning: $1"; }

remove_managed_block() {
  local rc_file="$1"
  if [[ ! -f "$rc_file" ]]; then
    return
  fi
  if grep -qF '# >>> TreeMan shell integration >>>' "$rc_file" 2>/dev/null; then
    if ! grep -qF '# <<< TreeMan shell integration <<<' "$rc_file" 2>/dev/null; then
      print_warn "TreeMan integration block in $rc_file is malformed; leaving it unchanged."
      return
    fi
    print_step "Removing TreeMan from $rc_file..."
    awk '
      $0 == "# >>> TreeMan shell integration >>>" { removing = 1; next }
      $0 == "# <<< TreeMan shell integration <<<" && removing { removing = 0; next }
      !removing { print }
    ' "$rc_file" > "${rc_file}.tmp" && mv "${rc_file}.tmp" "$rc_file"
    print_done
  fi
}

# Remove exact TreeMan integration blocks from a shell rc file.
# The current block is:
#   # TreeMan
#   export PATH="<install-dir>/bin:$PATH"
#   eval "$(treeman init bash)"
# or:
#   set -gx PATH "<install-dir>/bin" $PATH
#   treeman init fish | source
# Do not remove a marker unless both following lines match the install format.
remove_from_rc() {
  local rc_file="$1"
  if [[ ! -f "$rc_file" ]]; then
    return
  fi
  if grep -qF "$SOURCE_MARKER" "$rc_file" 2>/dev/null; then
    print_step "Removing TreeMan from $rc_file..."
    awk -v marker="$SOURCE_MARKER" -v path_line="export PATH=\"$INSTALL_DIR/bin:\$PATH\"" -v fish_path_line="set -gx PATH \"$INSTALL_DIR/bin\" \$PATH" '
      { lines[NR] = $0 }
      END {
        for (i = 1; i <= NR; i++) {
          if (lines[i] == marker && ((lines[i + 1] == path_line && lines[i + 2] ~ /^eval "\$\(treeman init (bash|zsh)\)"$/) || (lines[i + 1] == fish_path_line && lines[i + 2] == "treeman init fish | source"))) {
            i += 2
            continue
          }
          print lines[i]
        }
      }
    ' "$rc_file" > "${rc_file}.tmp" && mv "${rc_file}.tmp" "$rc_file"
    print_done
  fi
}

if [[ -x "$INSTALL_DIR/bin/treeman" ]]; then
  "$INSTALL_DIR/bin/treeman" shell uninstall --all
else
  remove_managed_block "$HOME/.zshrc"
  remove_managed_block "$HOME/.bashrc"
  remove_managed_block "$HOME/.bash_profile"
  remove_managed_block "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"
fi

remove_from_rc "$HOME/.zshrc"
remove_from_rc "$HOME/.bashrc"
remove_from_rc "$HOME/.bash_profile"
remove_from_rc "${XDG_CONFIG_HOME:-$HOME/.config}/fish/config.fish"

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
