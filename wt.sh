# TreeMan — wt
# Git worktree + branch creation with automatic dependency installation.
#
# Usage:
#   wt <branch-name>
#   git wt <branch-name>   (after: git config --global alias.wt '!wt')
#
# Supports: bash, zsh
# Dependencies: git, and whichever package manager your project uses

# ---------------------------------------------------------------------------
# Helpers (prefixed with _ to avoid polluting the user's namespace)
# ---------------------------------------------------------------------------

# Detect the default branch on origin (main or master).
# Prints the branch name to stdout. Returns 1 if neither exists.
_wt_detect_default_branch() {
  if git ls-remote --exit-code --heads origin main >/dev/null 2>&1; then
    echo "main"
  elif git ls-remote --exit-code --heads origin master >/dev/null 2>&1; then
    echo "Warning: no 'main' branch found on origin, using 'master'." >&2
    echo "master"
  else
    echo "Error: could not find 'main' or 'master' on origin." >&2
    return 1
  fi
}

# Detect the project's lockfile and run the matching install command.
# Expects to be called with the worktree path as $1.
# Returns 1 if the install command fails.
_wt_install_deps() {
  local dir="$1"
  local lockfile rest binary args label

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
        (cd "$dir" && eval "$binary $args")
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

  for f in "$src"/.env*; do
    [ -f "$f" ] || continue
    cp "$f" "$dest/"
    echo "  Copied $(basename "$f")"
    copied=$((copied + 1))
  done

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

  if ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "Error: not inside a git repository." >&2
    return 1
  fi

  # Find the main worktree root (works even from inside an existing worktree)
  local main_root
  main_root=$(git worktree list --porcelain | grep '^worktree ' | head -1 | sed 's/^worktree //')
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

  # Create the worktree + branch
  echo "Creating worktree at $worktree_path (branch: $branch)..."
  git worktree add -b "$branch" "$worktree_path" "origin/$default_branch" || return 1

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
