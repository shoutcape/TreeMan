# TreeMan — wt, wtpr, wtmr, wts & wtd
# Git worktree creation for branches and PRs/MRs, automatic dependency
# installation, interactive switching via fzf, and worktree deletion.
#
# Usage:
#   wt   <branch-name>     Create a new worktree + branch
#   wtpr [pr-number]       Create a review worktree from a GitHub PR
#   wtmr [pr-number]       Same as wtpr (PR/MR are interchangeable here)
#   wts  [query]           Switch between worktrees (fzf picker)
#   wtd  [query]           Delete a worktree and its branch (fzf picker)
#
# Supports: bash, zsh
# Dependencies: git, gh (for wtpr/wtmr), fzf (for wts/wtd and optional wtpr/wtmr picker),
# and whichever package manager your project uses

# ---------------------------------------------------------------------------
# Helpers (prefixed with _ to avoid polluting the user's namespace)
# ---------------------------------------------------------------------------

# Detect the default branch on origin (main or master).
# Fast path: use local origin/HEAD metadata. Falls back to querying origin.
# Prints the branch name to stdout. Returns 1 if neither exists.
_wt_detect_default_branch() {
  local origin_head refs

  origin_head=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null) || origin_head=""
  origin_head=${origin_head#origin/}

  if [[ "$origin_head" == "main" ]]; then
    echo "main"
    return 0
  fi

  if [[ "$origin_head" == "master" ]]; then
    echo "Warning: no 'main' branch found on origin, using 'master'." >&2
    echo "master"
    return 0
  fi

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

# Convert a branch name to the slug used in worktree directory names.
_wt_branch_slug() {
  local branch="$1"
  echo "${branch//\//-}"
}

# Build a sibling worktree path from the main root and branch name.
_wt_worktree_path_for_branch() {
  local main_root="$1"
  local branch="$2"
  local repo_name branch_slug

  repo_name=$(basename "$main_root")
  branch_slug=$(_wt_branch_slug "$branch")
  echo "$(dirname "$main_root")/${repo_name}.${branch_slug}"
}

# Resolve the GitHub owner/repo slug from the origin remote URL.
_wt_origin_repo_slug() {
  local remote_url slug

  if [[ -n "${_TREEMAN_GH_REPO:-}" ]]; then
    echo "${_TREEMAN_GH_REPO%/}"
    return 0
  fi

  remote_url=$(git remote get-url origin 2>/dev/null) || {
    echo "Error: could not read origin remote URL." >&2
    return 1
  }

  case "$remote_url" in
    git@github.com:*.git)
      slug=${remote_url#git@github.com:}
      slug=${slug%.git}
      ;;
    git@github.com:*)
      slug=${remote_url#git@github.com:}
      ;;
    https://github.com/*/*.git)
      slug=${remote_url#https://github.com/}
      slug=${slug%.git}
      ;;
    https://github.com/*/*)
      slug=${remote_url#https://github.com/}
      slug=${slug%/}
      ;;
    ssh://git@github.com/*/*.git)
      slug=${remote_url#ssh://git@github.com/}
      slug=${slug%.git}
      ;;
    ssh://git@github.com/*/*)
      slug=${remote_url#ssh://git@github.com/}
      slug=${slug%/}
      ;;
    *)
      echo "Error: origin remote '$remote_url' is not a supported GitHub URL." >&2
      return 1
      ;;
  esac

  echo "$slug"
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
  local plain_selection

  plain_selection=$(printf '%s\n' "$selection" | sed $'s/\033\\[[0-9;]*m//g')

  echo "$display" | sed $'s/\033\\[[0-9;]*m//g' | grep -nxF "$plain_selection" | head -1 | cut -d: -f1
}

# Validate PR/MR number input.
_wt_validate_pr_number() {
  local pr_number="$1"

  if [[ -z "$pr_number" || ! "$pr_number" =~ ^[0-9]+$ ]]; then
    echo "Error: PR/MR number must be numeric." >&2
    return 1
  fi
}

# Resolve PR metadata via gh and print TSV: number, title, headRefName, owner
_wt_pr_metadata() {
  local pr_number="$1"
  local repo_slug

  repo_slug=$(_wt_origin_repo_slug) || return 1

  gh api "repos/$repo_slug/pulls/$pr_number" \
    --jq '[.number, .title, .head.ref, .head.repo.owner.login] | @tsv'
}

# List open PRs/MRs via gh as TSV: number, headRefName, title
_wt_pr_list() {
  local repo_slug

  repo_slug=$(_wt_origin_repo_slug) || return 1

  gh api "repos/$repo_slug/pulls?state=open&per_page=100" \
    --jq '.[] | [.number, .head.ref, .title] | @tsv'
}

# Format gh TSV output into a readable fzf list.
_wt_pr_picker_display() {
  local rows="$1"
  local number branch title

  if [[ -z "$rows" ]]; then
    return 0
  fi

  printf "%-8s %-32s %s\n" "PR/MR" "Branch" "Title"

  while IFS=$'\t' read -r number branch title; do
    [[ -n "$number" ]] || continue
    printf "\033[33m#%-7s\033[0m \033[36m%-32s\033[0m %s\n" "$number" "$branch" "$title"
  done <<EOF
$rows
EOF
}

# Prompt for an open PR/MR and print its number.
_wt_pick_pr_number() {
  local rows display selection line_num pr_number

  if ! command -v fzf >/dev/null 2>&1; then
    echo "Error: fzf is required to pick an open PR/MR. Pass a PR number or install fzf." >&2
    return 1
  fi

  rows=$(_wt_pr_list) || {
    echo "Error: failed to list open PRs/MRs with gh." >&2
    return 1
  }

  if [[ -z "$rows" ]]; then
    echo "No open PRs/MRs found." >&2
    return 1
  fi

  display=$(_wt_pr_picker_display "$rows")
  selection=$(echo "$display" | fzf \
    --ansi \
    --border-label=" open prs / mrs " \
    --header-lines=1 \
    --prompt="review > " \
    --select-1 \
    --exit-0)

  if [[ -z "$selection" ]]; then
    return 1
  fi

  line_num=$(_wt_selection_line_num "$display" "$selection")
  pr_number=$(echo "$rows" | sed -n "${line_num}p" | cut -f1)

  [[ -n "$pr_number" ]] || return 1
  echo "$pr_number"
}

# Print success output for a PR/MR review worktree.
_wt_print_review_ready() {
  local pr_number="$1"
  local pr_title="$2"
  local head_ref="$3"
  local worktree_path="$4"

  echo ""
  echo "Review worktree ready:"
  echo "  PR/MR:  #$pr_number"
  echo "  Title:  $pr_title"
  echo "  Branch: $head_ref"
  echo "  Path:   $worktree_path"
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
  local branch="${1:-}"

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
  local worktree_path
  worktree_path=$(_wt_worktree_path_for_branch "$main_root" "$branch")

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
  cd "$worktree_path" || return 1

  echo ""
  echo "Worktree ready:"
  echo "  Auto-switched to: $worktree_path"
  echo "  Path: $worktree_path"
}

_wt_review_pr() {
  local trigger="$1"
  local pr_number="$2"
  local main_root metadata resolved_number pr_title head_ref owner worktree_path existing_worktree

  if ! git rev-parse --git-dir >/dev/null 2>&1; then
    echo "Error: not inside a git repository." >&2
    return 1
  fi

  if ! command -v gh >/dev/null 2>&1; then
    echo "Error: gh is required for $trigger. Install it from https://cli.github.com/" >&2
    return 1
  fi

  if [[ -z "$pr_number" ]]; then
    pr_number=$(_wt_pick_pr_number) || return 1
  else
    _wt_validate_pr_number "$pr_number" || {
      echo "Usage: $trigger [pr-number]" >&2
      return 1
    }
  fi

  metadata=$(_wt_pr_metadata "$pr_number") || {
    echo "Error: failed to resolve PR/MR #$pr_number with gh. Make sure the PR exists and that origin points at a GitHub repo you can access." >&2
    return 1
  }

  IFS=$'\t' read -r resolved_number pr_title head_ref owner <<EOF
$metadata
EOF

  if [[ -z "$resolved_number" || -z "$head_ref" ]]; then
    echo "Error: incomplete PR/MR metadata returned by gh." >&2
    return 1
  fi

  if ! _wt_validate_pr_number "$resolved_number" >/dev/null 2>&1; then
    echo "Error: invalid PR/MR number returned by gh." >&2
    return 1
  fi

  main_root=$(_wt_main_root)
  worktree_path=$(_wt_worktree_path_for_branch "$main_root" "$head_ref")

  if git show-ref --verify --quiet "refs/heads/$head_ref"; then
    existing_worktree=$(_wt_find_worktree_for_branch "$head_ref") || existing_worktree=""
    if [[ -n "$existing_worktree" ]]; then
      echo "Error: branch '$head_ref' already has a worktree at '$existing_worktree'." >&2
    else
      echo "Error: PR/MR head branch '$head_ref' already exists locally." >&2
    fi
    return 1
  fi

  if [[ -d "$worktree_path" ]]; then
    echo "Error: directory '$worktree_path' already exists for branch '$head_ref'." >&2
    return 1
  fi

  echo "Fetching PR/MR #$resolved_number from origin..."
  git fetch origin "pull/$resolved_number/head" || return 1

  echo "Creating review worktree at $worktree_path (branch: $head_ref)..."
  HUSKY=0 git worktree add --no-track -b "$head_ref" "$worktree_path" FETCH_HEAD || return 1

  _wt_copy_env_files "$main_root" "$worktree_path"

  echo "Detecting dependencies..."
  _wt_install_deps "$worktree_path" || echo "Warning: dependency installation failed." >&2

  cd "$worktree_path" || return 1

  _wt_print_review_ready "$resolved_number" "$pr_title" "$head_ref" "$worktree_path"
}

wtpr() {
  _wt_review_pr "wtpr" "${1:-}"
}

wtmr() {
  _wt_review_pr "wtmr" "${1:-}"
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
