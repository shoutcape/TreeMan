# TreeMan — wt, wts & wtd
# Git worktree + branch creation with automatic dependency installation,
# interactive worktree switching via fzf, and worktree deletion.
#
# Usage:
#   wt  <branch-name>      Create a new worktree + branch
#   wts [query]            Switch between worktrees (fzf picker)
#   wtd [query]            Delete a worktree and its branch (fzf picker)
#
# Supports: bash, zsh
# Dependencies: git, fzf (for wts and wtd), and whichever package manager your project uses

# ---------------------------------------------------------------------------
# Helpers (prefixed with _ to avoid polluting the user's namespace)
# ---------------------------------------------------------------------------

# Detect the default branch on origin (main or master).
# Prints the branch name to stdout. Returns 1 if neither exists.
_wt_detect_default_branch() {
  local refs
  refs=$(git ls-remote --heads origin main master 2>/dev/null)

  if echo "$refs" | grep -q 'refs/heads/main$'; then
    echo "main"
  elif echo "$refs" | grep -q 'refs/heads/master$'; then
    echo "Warning: no 'main' branch found on origin, using 'master'." >&2
    echo "master"
  else
    echo "Error: could not find 'main' or 'master' on origin." >&2
    return 1
  fi
}

# Return the main worktree root.
_wt_main_root() {
  git worktree list --porcelain | grep '^worktree ' | head -1 | sed 's/^worktree //'
}

# Return git worktree list in normal format.
_wt_worktree_lines() {
  git worktree list 2>/dev/null
}

# Return line count for stdin content passed as $1.
_wt_line_count() {
  printf '%s\n' "$1" | wc -l | tr -d ' '
}

# Build display rows from git worktree list output.
# Expects worktree list lines as $1. Optional main root exclusion as $2.
_wt_display_worktrees() {
  local lines="$1"
  local main_root="${2:-}"

  echo "$lines" | awk -v main="$main_root" '{
    if (main != "" && $1 == main) next
    path = $1
    n = split(path, parts, "/")
    short = (n >= 2) ? parts[n-1] "/" parts[n] : parts[n]
    printf "%-40s  \033[36m%s\033[0m\n", short, $3
  }'
}

# Extract full worktree paths from git worktree list output.
# Expects worktree list lines as $1. Optional main root exclusion as $2.
_wt_full_paths() {
  local lines="$1"
  local main_root="${2:-}"

  echo "$lines" | awk -v main="$main_root" '$1 != main { print $1 }'
}

# Map an fzf selection back to a line number.
_wt_selection_line_num() {
  local display="$1"
  local selection="$2"

  echo "$display" | sed $'s/\033\\[[0-9;]*m//g' | grep -nxF "$selection" | head -1 | cut -d: -f1
}

# Resolve a branch name to its worktree path, if present.
_wt_find_worktree_for_branch() {
  local target_branch="$1"
  local lines current_path current_branch

  lines=$(git worktree list --porcelain 2>/dev/null) || return 1
  current_path=""
  current_branch=""

  while IFS= read -r line; do
    case "$line" in
      worktree\ *)
        current_path=${line#worktree }
        current_branch=""
        ;;
      branch\ refs/heads/*)
        current_branch=${line#branch refs/heads/}
        ;;
      "")
        if [[ -n "$current_path" && "$current_branch" == "$target_branch" ]]; then
          echo "$current_path"
          return 0
        fi
        current_path=""
        current_branch=""
        ;;
    esac
  done <<EOF
$lines
EOF

  if [[ -n "$current_path" && "$current_branch" == "$target_branch" ]]; then
    echo "$current_path"
    return 0
  fi

  return 1
}

# Delete a worktree and its branch with TreeMan guards.
_wt_delete_worktree_and_branch() {
  local dest="$1"
  local branch="$2"
  local main_root default_branch

  main_root=$(_wt_main_root) || return 1
  default_branch=$(_wt_detect_default_branch 2>/dev/null) || default_branch=""

  if [[ -z "$dest" || -z "$branch" ]]; then
    echo "Error: missing worktree delete target." >&2
    return 1
  fi

  if [[ "$dest" == "$main_root" ]]; then
    echo "Error: cannot delete the main worktree." >&2
    return 1
  fi

  if [[ -n "$default_branch" && "$branch" == "$default_branch" ]]; then
    echo "Error: cannot delete the default branch '$branch'." >&2
    return 1
  fi

  if [[ "$(pwd)" == "$dest"* ]]; then
    echo "Currently inside this worktree — switching to main worktree..."
    cd "$main_root" || return 1
  fi

  echo "Removing worktree..."
  git worktree remove "$dest" || {
    echo "Error: failed to remove worktree '$dest'. Use 'git worktree remove --force' to force it." >&2
    return 1
  }

  echo "Deleting branch '$branch'..."
  git branch -D "$branch" 2>/dev/null || {
    echo "Error: branch '$branch' could not be deleted." >&2
    return 1
  }

  echo "Done — worktree and branch removed."
}

# Lazygit entrypoint: delete selected worktree.
_wt_lazygit_delete_worktree() {
  local dest="$1"
  local branch="$2"

  _wt_delete_worktree_and_branch "$dest" "$branch"
}

# Lazygit entrypoint: delete selected branch's worktree.
_wt_lazygit_delete_branch() {
  local branch="$1"
  local dest main_root default_branch

  main_root=$(_wt_main_root) || return 1
  default_branch=$(_wt_detect_default_branch 2>/dev/null) || default_branch=""

  if [[ -z "$branch" ]]; then
    echo "Error: missing branch name." >&2
    return 1
  fi

  if [[ -n "$default_branch" && "$branch" == "$default_branch" ]]; then
    echo "Error: cannot delete the default branch '$branch'." >&2
    return 1
  fi

  dest=$(_wt_find_worktree_for_branch "$branch") || {
    echo "Error: branch '$branch' does not have a removable worktree." >&2
    return 1
  }

  if [[ "$dest" == "$main_root" ]]; then
    echo "Error: cannot delete the main worktree." >&2
    return 1
  fi

  _wt_delete_worktree_and_branch "$dest" "$branch"
}

# Detect the project's lockfile and run the matching install command.
# Expects to be called with the worktree path as $1.
# Returns 1 if the install command fails.
_wt_install_deps() {
  local dir="$1"
  local lockfile rest binary args

  # lockfile : binary : args
  # NOTE: No field may contain a colon character.
  local deps=(
    "pnpm-lock.yaml:pnpm:install"
    "yarn.lock:yarn:install"
    "package-lock.json:npm:install"
    "go.mod:go:mod download"
  )

  for entry in "${deps[@]}"; do
    lockfile="${entry%%:*}"              # first field
    rest="${entry#*:}"
    binary="${rest%%:*}"                 # second field
    args="${rest#*:}"                    # third field (remainder)

    if [[ -f "$dir/$lockfile" ]]; then
      if command -v "$binary" >/dev/null 2>&1; then
        echo "Detected $lockfile — running $binary $args..."
        # Run in a POSIX sh subshell for portable word-splitting of $args.
        # Zsh doesn't word-split by default, so we delegate to sh where
        # "go mod download" correctly becomes: go mod download (3 words).
        # Safe: $binary and $args are hardcoded literals from the deps array.
        (cd "$dir" && sh -c '"$1" $2' _ "$binary" "$args")
        return $?
      else
        echo "Warning: $lockfile found but $binary is not installed, skipping." >&2
      fi
      return
    fi
  done

  # Python projects need manual venv activation, so just inform the user.
  if [[ -f "$dir/requirements.txt" ]] || [[ -f "$dir/pyproject.toml" ]]; then
    echo "Detected Python project — skipping auto-install (activate your venv manually)."
    return
  fi

  echo "No known dependency file detected, skipping install."
}

# Copy .env* files from the main worktree root to the new worktree.
# Silently skips if no env files exist.
_wt_copy_env_files() {
  local src="$1"
  local dest="$2"
  local copied=0

  # Use find instead of a glob to avoid zsh NOMATCH errors when no .env* files exist
  while IFS= read -r f; do
    [ -f "$f" ] || continue
    cp "$f" "$dest/"
    echo "  Copied $(basename "$f")"
    copied=$((copied + 1))
  done < <(find "$src" -maxdepth 1 -name '.env*' -type f 2>/dev/null)

  if [ "$copied" -gt 0 ]; then
    echo "Copied $copied env file(s) from main worktree."
  fi
}

# ---------------------------------------------------------------------------
# Main function
# ---------------------------------------------------------------------------

wt() {
  local branch="$1"

  if [[ -z "$branch" ]]; then
    echo "Usage: wt <branch-name>" >&2
    return 1
  fi

  # Reject branch names with characters that git doesn't allow
  if [[ "$branch" =~ [[:space:]~\^:?\*\[\\] ]]; then
    echo "Error: branch name contains invalid characters." >&2
    return 1
  fi

  if ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "Error: not inside a git repository." >&2
    return 1
  fi

  # Find the main worktree root (works even from inside an existing worktree)
  local main_root
  main_root=$(_wt_main_root)
  local repo_name
  repo_name=$(basename "$main_root")

  # Resolve the default branch
  local default_branch
  default_branch=$(_wt_detect_default_branch) || return 1

  # Guard against duplicate branches
  if git show-ref --verify --quiet "refs/heads/$branch"; then
    echo "Error: branch '$branch' already exists locally." >&2
    return 1
  fi

  # Fetch latest
  echo "Fetching latest $default_branch from origin..."
  git fetch origin "$default_branch" || return 1

  # Build the worktree path: <parent>/<repo>.<branch-with-/-as-->
  local branch_slug="${branch//\//-}"
  local worktree_path
  worktree_path="$(dirname "$main_root")/${repo_name}.${branch_slug}"

  # Guard against existing directory
  if [[ -d "$worktree_path" ]]; then
    echo "Error: directory '$worktree_path' already exists." >&2
    return 1
  fi

  # Create the worktree + branch
  echo "Creating worktree at $worktree_path (branch: $branch)..."
  HUSKY=0 git worktree add --no-track -b "$branch" "$worktree_path" "origin/$default_branch" || return 1

  # Copy env files from main worktree (before dep install, as some hooks may need them)
  _wt_copy_env_files "$main_root" "$worktree_path"

  # Install dependencies (non-fatal — worktree is usable even if install fails)
  echo "Detecting dependencies..."
  _wt_install_deps "$worktree_path" || echo "Warning: dependency installation failed." >&2

  # Done
  echo ""
  echo "Worktree ready:"
  echo "  cd \"$worktree_path\""
}

# ---------------------------------------------------------------------------
# Worktree switcher — interactive fzf picker
# ---------------------------------------------------------------------------

wts() {
  if ! command -v fzf >/dev/null 2>&1; then
    echo "Error: fzf is required for wts. Install it from https://github.com/junegunn/fzf" >&2
    return 1
  fi

  local lines
  lines=$(_wt_worktree_lines)
  if [ -z "$lines" ]; then
    echo "Not in a git repository or no worktrees found."
    return 1
  fi

  local count
  count=$(_wt_line_count "$lines")
  if [ "$count" -eq 1 ]; then
    echo "Only one worktree exists — nothing to switch to."
    return 0
  fi

  local current_dir
  current_dir=$(pwd)

  # Show only the last two path components in fzf for readability,
  # but keep a parallel array of full paths for cd.
  # Add color: path in white, branch in cyan.
  local display full_paths
  display=$(_wt_display_worktrees "$lines")
  full_paths=$(_wt_full_paths "$lines")

  local selection
  selection=$(echo "$display" | fzf \
    --ansi \
    --border-label=" worktrees " \
    --prompt="switch > " \
    --query="${1:-}" \
    --select-1 \
    --exit-0)

  if [ -z "$selection" ]; then
    return 0
  fi

  # Find the selected line number and map back to the full path.
  # Strip ANSI codes from display lines before matching, since fzf --ansi
  # returns plain text.
  local line_num
  line_num=$(_wt_selection_line_num "$display" "$selection")
  local dest
  dest=$(echo "$full_paths" | sed -n "${line_num}p")

  if [ "$dest" = "$current_dir" ]; then
    echo "Already in this worktree."
    return 0
  fi

  cd "$dest" || return 1
  local short="${dest%/}"
  short="${short##*/}"
  echo "cd → …/$short"
}

# ---------------------------------------------------------------------------
# Worktree deleter — interactive fzf picker with confirmation
# ---------------------------------------------------------------------------

wtd() {
  if ! command -v fzf >/dev/null 2>&1; then
    echo "Error: fzf is required for wtd. Install it from https://github.com/junegunn/fzf" >&2
    return 1
  fi

  if ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "Error: not inside a git repository." >&2
    return 1
  fi

  local lines
  lines=$(_wt_worktree_lines)
  if [ -z "$lines" ]; then
    echo "No worktrees found."
    return 1
  fi

  local count
  count=$(_wt_line_count "$lines")
  if [ "$count" -eq 1 ]; then
    echo "Only one worktree exists — nothing to delete."
    return 0
  fi

  # Find the main worktree root so we can exclude it from deletion
  local main_root
  main_root=$(_wt_main_root)

  # Build display list excluding the main worktree
  local display full_paths branches
  display=$(_wt_display_worktrees "$lines" "$main_root")
  full_paths=$(_wt_full_paths "$lines" "$main_root")
  branches=$(echo "$lines" | awk -v main="$main_root" '$1 != main { gsub(/[\[\]]/, "", $3); print $3 }')

  if [ -z "$display" ]; then
    echo "No deletable worktrees — only the main worktree exists."
    return 0
  fi

  local selection
  selection=$(echo "$display" | fzf \
    --ansi \
    --border-label=" delete worktree " \
    --prompt="delete > " \
    --query="${1:-}" \
    --select-1 \
    --exit-0)

  if [ -z "$selection" ]; then
    return 0
  fi

  # Map selection back to full path and branch
  local line_num
  line_num=$(_wt_selection_line_num "$display" "$selection")
  local dest branch
  dest=$(echo "$full_paths" | sed -n "${line_num}p")
  branch=$(echo "$branches" | sed -n "${line_num}p")

  # Confirm deletion
  local short="${dest%/}"
  short="${short##*/}"
  echo "About to delete:"
  echo "  Worktree: $dest"
  echo "  Branch:   $branch"
  echo ""
  printf "Are you sure? [y/N] "
  read -r confirm
  if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
    echo "Cancelled."
    return 0
  fi

  _wt_delete_worktree_and_branch "$dest" "$branch"
}

# ---------------------------------------------------------------------------
# Lazygit wrapper — auto-cd after worktree switch
# ---------------------------------------------------------------------------

lg() {
  if ! command -v lazygit >/dev/null 2>&1; then
    echo "Warning: lazygit is not installed." >&2
    return 1
  fi

  local newdir_file="$HOME/.lazygit/newdir"
  mkdir -p "$HOME/.lazygit"

  LAZYGIT_NEW_DIR_FILE="$newdir_file" lazygit "$@"

  if [[ -f "$newdir_file" ]]; then
    local target
    target=$(<"$newdir_file")
    rm -f "$newdir_file"
    if [[ -n "$target" && "$target" != "$(pwd)" ]]; then
      cd "$target" || return 1
    fi
  fi
}
