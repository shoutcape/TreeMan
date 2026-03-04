#!/usr/bin/env bash
# TreeMan — lazygit integration installer
#
# Adds custom keybindings to lazygit:
#   W  — Create a new worktree + branch (in the branches panel)
#   D  — Delete a worktree and its branch in one step (in the worktrees panel)
#
# Automatically detects the lazygit config location on Linux and macOS.
#
# Usage:
#   bash install-lazygit.sh

set -e

tmp=""
trap '[ -n "$tmp" ] && rm -f "$tmp" "${tmp}.2" "${tmp}.3" 2>/dev/null' EXIT

MARKER="# TreeMan"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

info()    { echo "$1"; }
success() { echo "$1"; }
warn()    { echo "Warning: $1" >&2; }
die()     { echo "Error: $1" >&2; exit 1; }

if [[ "$1" == "uninstall" ]] || [[ "$1" == "--uninstall" ]]; then
  if ! command -v lazygit >/dev/null 2>&1; then
    die "lazygit is not installed or not in PATH."
  fi

  config_dir=$(lazygit -cd 2>/dev/null) || die "Could not determine lazygit config directory."
  config_file="$config_dir/config.yml"

  if [ ! -f "$config_file" ]; then
    info "No lazygit config found at $config_file — nothing to remove."
    exit 0
  fi

  info "Lazygit config: $config_file"

  if ! grep -q "$MARKER" "$config_file" 2>/dev/null; then
    info "TreeMan lazygit integration is not installed — nothing to remove."
    exit 0
  fi

  tmp=$(mktemp)

  awk -v marker="$MARKER" '
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

  success "TreeMan lazygit integration removed."
  exit 0
fi

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------

if ! command -v lazygit >/dev/null 2>&1; then
  die "lazygit is not installed or not in PATH. Install it first: https://github.com/jesseduffield/lazygit"
fi

if [ ! -f "$HOME/.treeman/wt.sh" ]; then
  die "~/.treeman/wt.sh not found. Run install.sh first to set up TreeMan."
fi

# ---------------------------------------------------------------------------
# Locate the lazygit config file
# ---------------------------------------------------------------------------

config_dir=$(lazygit -cd 2>/dev/null) || die "Could not determine lazygit config directory."
config_file="$config_dir/config.yml"

info "Lazygit config: $config_file"

# Create config file if it doesn't exist
if [ ! -f "$config_file" ]; then
  mkdir -p "$config_dir"
  touch "$config_file"
  info "Created $config_file"
fi

# ---------------------------------------------------------------------------
# Idempotency check
# ---------------------------------------------------------------------------

if grep -q "$MARKER" "$config_file" 2>/dev/null; then
  info "TreeMan lazygit integration is already installed. Nothing to do."
  exit 0
fi

# ---------------------------------------------------------------------------
# Build the entry block to inject
# ---------------------------------------------------------------------------

# This is the list entry appended under customCommands.
# The marker is on the first line so the uninstaller can locate it.
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

# ---------------------------------------------------------------------------
# Inject into config
# ---------------------------------------------------------------------------

if ! grep -q '^customCommands:' "$config_file"; then
  # Strip empty YAML document literal ({}) before appending
  tmp=$(mktemp)
  grep -v '^{}$' "$config_file" > "$tmp" && mv "$tmp" "$config_file"

  # Case A: No customCommands key — append the full block
  cat >> "$config_file" << EOF

# TreeMan — worktree keybindings (W: create, D: delete)
customCommands:
$ENTRY
EOF

elif grep -q '^customCommands: \[\]' "$config_file"; then
  # Case B: customCommands is set to empty list literal — replace it
  # Use a temp file to avoid in-place sed portability issues on macOS
  tmp=$(mktemp)
  awk -v entry="$ENTRY" '
    /^customCommands: \[\]/ {
      print "customCommands:"
      print entry
      next
    }
    { print }
  ' "$config_file" > "$tmp" && mv "$tmp" "$config_file"

else
  # Case C: customCommands exists with entries — insert our entry before the
  # next top-level key (a line that starts with a word character followed by
  # a colon), or at end of file if customCommands is the last section.
  tmp=$(mktemp)
  awk -v entry="$ENTRY" '
    BEGIN { in_custom = 0; done = 0 }

    # Detect the customCommands section
    /^customCommands:/ { in_custom = 1; print; next }

    # Once inside customCommands, watch for the next top-level key
    in_custom && !done && /^[a-zA-Z]/ {
      # Insert our entry before this top-level key
      print entry
      print ""
      in_custom = 0
      done = 1
    }

    { print }

    # If we reach EOF still inside customCommands, append at the end
    END { if (in_custom && !done) { print ""; print entry } }
  ' "$config_file" > "$tmp" && mv "$tmp" "$config_file"
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------

success "TreeMan lazygit integration installed!"
success "  W — Create a new worktree (branches panel)"
success "  D — Delete worktree + branch (worktrees panel, branches panel)"
