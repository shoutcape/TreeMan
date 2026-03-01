# TreeMan — wt
# Git worktree + branch creation with automatic dependency installation.
#
# Usage:
#   wt <branch-name>
#   git wt <branch-name>   (after: git config --global alias.wt '!wt')
#
# Supports: bash, zsh
# Dependencies: git, and whichever package manager your project uses

wt() {
  local branch="$1"

  # 1. Require a branch name
  if [[ -z "$branch" ]]; then
    echo "Usage: wt <branch-name>" >&2
    return 1
  fi

  # 2. Must be inside a git repo
  if ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "Error: not inside a git repository." >&2
    return 1
  fi

  # 3. Find the main worktree root (works even from inside an existing worktree)
  local main_root
  main_root=$(git worktree list --porcelain | grep '^worktree ' | head -1 | cut -d' ' -f2)

  local repo_name
  repo_name=$(basename "$main_root")

  # 4. Determine the default branch (main → master fallback)
  local default_branch
  if git ls-remote --exit-code --heads origin main >/dev/null 2>&1; then
    default_branch="main"
  elif git ls-remote --exit-code --heads origin master >/dev/null 2>&1; then
    default_branch="master"
    echo "Warning: no 'main' branch found on origin, using 'master'."
  else
    echo "Error: could not find 'main' or 'master' on origin." >&2
    return 1
  fi

  # 5. Check branch doesn't already exist locally
  if git show-ref --verify --quiet "refs/heads/$branch"; then
    echo "Error: branch '$branch' already exists locally." >&2
    return 1
  fi

  # 6. Fetch latest
  echo "Fetching latest $default_branch from origin..."
  git fetch origin "$default_branch" || return 1

  # 7. Build the worktree path: <parent>/<repo>.<branch-with-/-as-->
  local branch_slug="${branch//\//-}"
  local worktree_path
  worktree_path="$(dirname "$main_root")/${repo_name}.${branch_slug}"

  # 8. Create the worktree + branch
  echo "Creating worktree at $worktree_path (branch: $branch)..."
  git worktree add -b "$branch" "$worktree_path" "origin/$default_branch" || return 1

  # 9. Detect and install dependencies
  echo "Detecting dependencies..."
  (
    cd "$worktree_path" || return 1

    if [[ -f "pnpm-lock.yaml" ]]; then
      if command -v pnpm >/dev/null 2>&1; then
        echo "Detected pnpm-lock.yaml — running pnpm install..."
        pnpm install
      else
        echo "Warning: pnpm-lock.yaml found but pnpm is not installed, skipping."
      fi
    elif [[ -f "yarn.lock" ]]; then
      if command -v yarn >/dev/null 2>&1; then
        echo "Detected yarn.lock — running yarn install..."
        yarn install
      else
        echo "Warning: yarn.lock found but yarn is not installed, skipping."
      fi
    elif [[ -f "package-lock.json" ]]; then
      if command -v npm >/dev/null 2>&1; then
        echo "Detected package-lock.json — running npm install..."
        npm install
      else
        echo "Warning: package-lock.json found but npm is not installed, skipping."
      fi
    elif [[ -f "go.mod" ]]; then
      if command -v go >/dev/null 2>&1; then
        echo "Detected go.mod — running go mod download..."
        go mod download
      else
        echo "Warning: go.mod found but go is not installed, skipping."
      fi
    elif [[ -f "requirements.txt" ]] || [[ -f "pyproject.toml" ]]; then
      echo "Detected Python project — skipping auto-install (activate your venv manually)."
    else
      echo "No known dependency file detected, skipping install."
    fi
  )

  # 10. Print result
  echo ""
  echo "Worktree ready:"
  echo "  cd $worktree_path"
}
