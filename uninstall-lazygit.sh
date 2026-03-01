#!/usr/bin/env bash
# TreeMan — lazygit integration uninstaller
#
# Removes all TreeMan keybindings (W, D) from lazygit's config.yml.
# Also removes the customCommands key and header comment if it becomes empty.
#
# Usage:
#   bash uninstall-lazygit.sh

set -e

tmp=""
trap '[ -n "$tmp" ] && rm -f "$tmp" "${tmp}.2" "${tmp}.3" 2>/dev/null' EXIT

MARKER="# TreeMan"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

info()    { echo "$1"; }
success() { echo "$1"; }
die()     { echo "Error: $1" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------

if ! command -v lazygit >/dev/null 2>&1; then
  die "lazygit is not installed or not in PATH."
fi

# ---------------------------------------------------------------------------
# Locate the lazygit config file
# ---------------------------------------------------------------------------

config_dir=$(lazygit -cd 2>/dev/null) || die "Could not determine lazygit config directory."
config_file="$config_dir/config.yml"

if [ ! -f "$config_file" ]; then
  info "No lazygit config found at $config_file — nothing to remove."
  exit 0
fi

info "Lazygit config: $config_file"

# ---------------------------------------------------------------------------
# Check the marker is present
# ---------------------------------------------------------------------------

if ! grep -q "$MARKER" "$config_file" 2>/dev/null; then
  info "TreeMan lazygit integration is not installed — nothing to remove."
  exit 0
fi

# ---------------------------------------------------------------------------
# Pass 1: Remove the TreeMan entry block
#
# Walk line by line. When we hit the line containing the TreeMan marker
# (the '  - key: W # TreeMan' line), skip lines until we reach the next
# sibling list entry ('  - ') or a top-level key or EOF.
# ---------------------------------------------------------------------------

tmp=$(mktemp)

awk -v marker="$MARKER" '
  BEGIN { skipping = 0 }

  # Start or continue skipping when we see the marker
  index($0, marker) { skipping = 1; next }

  skipping {
    # NOTE: Assumes 2-space indentation for customCommands entries (lazygit default)
    if (/^  - / || /^[a-zA-Z]/) { skipping = 0 }
    else { next }
  }

  { print }
' "$config_file" > "$tmp"

# ---------------------------------------------------------------------------
# Pass 2: Remove orphaned customCommands section if now empty
#
# An empty customCommands block looks like one of:
#   customCommands:\n<blank or top-level key next>
#
# We also remove the preceding comment line if it contains the TreeMan marker.
# ---------------------------------------------------------------------------

awk '
  {
    lines[NR] = $0
    original[NR] = $0  # keep a copy to distinguish real blanks
    suppress[NR] = 0
  }
  END {
    for (i = 1; i <= NR; i++) {
      if (lines[i] ~ /^customCommands:$/) {
        j = i + 1
        while (j <= NR && lines[j] ~ /^[[:space:]]*$/) j++

        if (j > NR || lines[j] ~ /^[a-zA-Z]/) {
          # Mark the customCommands line and trailing blanks for suppression
          for (k = i; k < j; k++) suppress[k] = 1
          # Mark preceding TreeMan comment and its preceding blank
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

# ---------------------------------------------------------------------------
# Strip trailing blank lines and write back
# ---------------------------------------------------------------------------

awk 'NF { last = NR } { lines[NR] = $0 } END { for (i = 1; i <= last; i++) print lines[i] }' "${tmp}.2" > "${tmp}.3"

{ cat "${tmp}.3"; echo; } > "$config_file"

rm -f "$tmp" "${tmp}.2" "${tmp}.3"

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------

success "TreeMan lazygit integration removed."
