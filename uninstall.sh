#!/usr/bin/env bash
# TreeMan uninstaller
# Usage: curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/uninstall.sh | bash

set -e

INSTALL_DIR="$HOME/.treeman"
SOURCE_MARKER="# TreeMan"

print_step() { echo "==> $1"; }
print_done() { echo "    done."; }
print_warn() { echo "    warning: $1"; }

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
