#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
TMP_DIR=$(mktemp -d)
# Resolve symlinks (macOS /var → /private/var) so paths match git worktree output
TMP_DIR=$(cd "$TMP_DIR" && pwd -P)
TEST_HOME="$TMP_DIR/home"
LAZYGIT_CONFIG_DIR="$TMP_DIR/lazygit"
MOCK_BIN="$TMP_DIR/bin"
REMOTE_REPO="$TMP_DIR/remote.git"
MAIN_REPO="$TMP_DIR/project"
PR_SOURCE_REPO="$TMP_DIR/pr-source"
WORKTREE_REPO="$TMP_DIR/project.feature-test"
REVIEW_WT_ALPHA="$TMP_DIR/project.feature-review-alpha"
REVIEW_WT_BETA="$TMP_DIR/project.feature-review-beta"

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

assert_file_contains() {
  local file="$1"
  local text="$2"
  grep -qF "$text" "$file" || fail "Expected '$text' in $file"
}

assert_file_not_contains() {
  local file="$1"
  local text="$2"
  if [[ -f "$file" ]] && grep -qF "$text" "$file"; then
    fail "Did not expect '$text' in $file"
  fi
}

assert_missing() {
  local path="$1"
  [[ ! -e "$path" ]] || fail "Expected $path to be missing"
}

assert_exists() {
  local path="$1"
  [[ -e "$path" ]] || fail "Expected $path to exist"
}

mkdir -p "$TEST_HOME"
touch "$TEST_HOME/.bashrc"

export HOME="$TEST_HOME"
export XDG_CONFIG_HOME="$HOME/.config"
export GIT_CONFIG_NOSYSTEM=1
export _TREEMAN_GH_REPO="shoutcape/TreeMan"
export _TREEMAN_FORGE="github"
export GIT_AUTHOR_NAME="TreeMan Test"
export GIT_AUTHOR_EMAIL="test@example.com"
export GIT_COMMITTER_NAME="TreeMan Test"
export GIT_COMMITTER_EMAIL="test@example.com"

mkdir -p "$XDG_CONFIG_HOME"
mkdir -p "$LAZYGIT_CONFIG_DIR"

echo "==> install/uninstall"
TREEMAN_REPO_URL="file://$ROOT_DIR/wt.sh" TREEMAN_SHELL_RC="$HOME/.bashrc" TREEMAN_LAZYGIT_CONFIG_DIR="$LAZYGIT_CONFIG_DIR" bash "$ROOT_DIR/install.sh"
assert_exists "$HOME/.treeman/wt.sh"
assert_file_contains "$HOME/.bashrc" '# TreeMan'
assert_file_contains "$HOME/.bashrc" 'source "'
assert_file_contains "$LAZYGIT_CONFIG_DIR/config.yml" "$HOME/.treeman/wt.sh"
if git config --global --get alias.wt >/dev/null 2>&1; then
  fail "install.sh should not create git alias.wt"
fi

TREEMAN_LAZYGIT_CONFIG_DIR="$LAZYGIT_CONFIG_DIR" bash "$ROOT_DIR/uninstall.sh"
assert_missing "$HOME/.treeman"
assert_file_not_contains "$HOME/.bashrc" '# TreeMan'
assert_file_not_contains "$LAZYGIT_CONFIG_DIR/config.yml" '# TreeMan'
if git config --global --get alias.wt >/dev/null 2>&1; then
  fail "uninstall.sh should not leave git alias.wt behind"
fi

echo "==> repository setup"
git init --bare "$REMOTE_REPO" >/dev/null
git clone "$REMOTE_REPO" "$MAIN_REPO" >/dev/null
git -C "$MAIN_REPO" switch -c main >/dev/null
printf 'hello\n' > "$MAIN_REPO/README.md"
git -C "$MAIN_REPO" add README.md
git -C "$MAIN_REPO" commit -m "init" >/dev/null
git -C "$MAIN_REPO" push -u origin main >/dev/null

echo "==> mock gh/fzf setup"
mkdir -p "$MOCK_BIN"
cat > "$MOCK_BIN/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "api" ]]; then
  endpoint="${2:-}"

  case "$endpoint" in
    */pulls/123)
      printf '%s\n' "${MOCK_GH_VIEW_123:-}"
      exit 0
      ;;
    */pulls/124)
      printf '%s\n' "${MOCK_GH_VIEW_124:-}"
      exit 0
      ;;
    *"/pulls?state=open&per_page=100")
      printf '%s\n' "${MOCK_GH_LIST:-}"
      exit 0
      ;;
  esac
fi

echo "unsupported gh invocation: $*" >&2
exit 1
EOF
chmod +x "$MOCK_BIN/gh"

cat > "$MOCK_BIN/fzf" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
sed -n "${FZF_CHOICE:-1}p"
EOF
chmod +x "$MOCK_BIN/fzf"

export PATH="$MOCK_BIN:$PATH"

echo "==> review branch setup"
git clone "$REMOTE_REPO" "$PR_SOURCE_REPO" >/dev/null
git -C "$PR_SOURCE_REPO" switch -c main origin/main >/dev/null

git -C "$PR_SOURCE_REPO" switch -c feature/review-alpha >/dev/null
printf 'alpha review\n' > "$PR_SOURCE_REPO/review-alpha.txt"
git -C "$PR_SOURCE_REPO" add review-alpha.txt
git -C "$PR_SOURCE_REPO" commit -m "alpha review" >/dev/null
git -C "$PR_SOURCE_REPO" push -u origin feature/review-alpha >/dev/null
git -C "$PR_SOURCE_REPO" push origin HEAD:refs/pull/123/head >/dev/null

git -C "$PR_SOURCE_REPO" switch main >/dev/null
git -C "$PR_SOURCE_REPO" switch -c feature/review-beta >/dev/null
printf 'beta review\n' > "$PR_SOURCE_REPO/review-beta.txt"
git -C "$PR_SOURCE_REPO" add review-beta.txt
git -C "$PR_SOURCE_REPO" commit -m "beta review" >/dev/null
git -C "$PR_SOURCE_REPO" push -u origin feature/review-beta >/dev/null
git -C "$PR_SOURCE_REPO" push origin HEAD:refs/pull/124/head >/dev/null

echo "==> worktree create"
source "$ROOT_DIR/wt.sh"
cd "$MAIN_REPO"
wt feature/test >/dev/null
assert_exists "$WORKTREE_REPO"
[[ "$(pwd)" == "$WORKTREE_REPO" ]] || fail "Expected wt to cd into created worktree"
git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/test || fail "Expected feature/test branch"

cd "$MAIN_REPO"
if wt feature/test >/dev/null 2>&1; then
  fail "wt should reject duplicate branch names"
fi

echo "==> review worktree create"
export MOCK_GH_VIEW_123=$'123\tAlpha review\tfeature/review-alpha\tshoutcape'
wtpr 123 >/dev/null
assert_exists "$REVIEW_WT_ALPHA"
assert_exists "$REVIEW_WT_ALPHA/review-alpha.txt"
[[ "$(pwd)" == "$REVIEW_WT_ALPHA" ]] || fail "Expected wtpr to cd into created review worktree"
git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/review-alpha || fail "Expected feature/review-alpha branch"

cd "$MAIN_REPO"
if wtpr nope >/dev/null 2>&1; then
  fail "wtpr should reject non-numeric input"
fi

echo "==> review picker alias"
export MOCK_GH_VIEW_124=$'124\tBeta review\tfeature/review-beta\tshoutcape'
export MOCK_GH_LIST=$'123\tfeature/review-alpha\tAlpha review\n124\tfeature/review-beta\tBeta review'
export FZF_CHOICE=2
wtmr >/dev/null
assert_exists "$REVIEW_WT_BETA"
assert_exists "$REVIEW_WT_BETA/review-beta.txt"
[[ "$(pwd)" == "$REVIEW_WT_BETA" ]] || fail "Expected wtmr to cd into created review worktree"
git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/review-beta || fail "Expected feature/review-beta branch"
unset FZF_CHOICE

# ---------------------------------------------------------------------------
# URL parsing & forge detection tests
# ---------------------------------------------------------------------------

assert_eq() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$expected" != "$actual" ]]; then
    fail "$label: expected '$expected', got '$actual'"
  fi
}

echo "==> URL parsing: _wt_parse_remote_host"
assert_eq "github ssh shorthand"     "github.com"    "$(_wt_parse_remote_host 'git@github.com:owner/repo.git')"
assert_eq "github https"             "github.com"    "$(_wt_parse_remote_host 'https://github.com/owner/repo.git')"
assert_eq "github ssh://"            "github.com"    "$(_wt_parse_remote_host 'ssh://git@github.com/owner/repo.git')"
assert_eq "gitlab.com ssh shorthand" "gitlab.com"    "$(_wt_parse_remote_host 'git@gitlab.com:group/project.git')"
assert_eq "gitlab.com https"         "gitlab.com"    "$(_wt_parse_remote_host 'https://gitlab.com/group/project.git')"
assert_eq "self-hosted gitlab ssh"   "gitlab.company.com" "$(_wt_parse_remote_host 'git@gitlab.company.com:acme/frontend/webapp.git')"
assert_eq "self-hosted gitlab https" "gitlab.company.com" "$(_wt_parse_remote_host 'https://gitlab.company.com/acme/frontend/webapp.git')"
assert_eq "ssh:// with port"         "gitlab.company.com" "$(_wt_parse_remote_host 'ssh://git@gitlab.company.com:2222/group/project.git')"

echo "==> URL parsing: _wt_parse_remote_path"
assert_eq "github ssh .git"          "owner/repo"    "$(_wt_parse_remote_path 'git@github.com:owner/repo.git')"
assert_eq "github ssh no .git"       "owner/repo"    "$(_wt_parse_remote_path 'git@github.com:owner/repo')"
assert_eq "github https .git"        "owner/repo"    "$(_wt_parse_remote_path 'https://github.com/owner/repo.git')"
assert_eq "github https no .git"     "owner/repo"    "$(_wt_parse_remote_path 'https://github.com/owner/repo')"
assert_eq "github ssh://"            "owner/repo"    "$(_wt_parse_remote_path 'ssh://git@github.com/owner/repo.git')"
assert_eq "gitlab nested groups ssh" "acme/frontend/webapp" \
  "$(_wt_parse_remote_path 'git@gitlab.company.com:acme/frontend/webapp.git')"
assert_eq "gitlab nested groups https" "acme/frontend/webapp" \
  "$(_wt_parse_remote_path 'https://gitlab.company.com/acme/frontend/webapp.git')"
assert_eq "gitlab nested groups ssh://" "acme/frontend/webapp" \
  "$(_wt_parse_remote_path 'ssh://git@gitlab.company.com/acme/frontend/webapp.git')"
assert_eq "ssh:// with port" "group/project" \
  "$(_wt_parse_remote_path 'ssh://git@gitlab.company.com:2222/group/project.git')"

echo "==> forge detection: _wt_detect_forge"
# Use _TREEMAN_REMOTE_URL to inject test URLs without a real remote
_TREEMAN_FORGE="" _TREEMAN_REMOTE_URL="git@github.com:owner/repo.git" \
  assert_eq "github from ssh" "github" "$(_TREEMAN_FORGE="" _TREEMAN_REMOTE_URL="git@github.com:owner/repo.git" _wt_detect_forge)"

_TREEMAN_FORGE="" _TREEMAN_REMOTE_URL="https://gitlab.com/group/project.git" \
  assert_eq "gitlab.com from https" "gitlab" "$(_TREEMAN_FORGE="" _TREEMAN_REMOTE_URL="https://gitlab.com/group/project.git" _wt_detect_forge)"

_TREEMAN_FORGE="" _TREEMAN_REMOTE_URL="git@gitlab.company.com:g/p.git" \
  assert_eq "self-hosted gitlab from ssh" "gitlab" "$(_TREEMAN_FORGE="" _TREEMAN_REMOTE_URL="git@gitlab.company.com:g/p.git" _wt_detect_forge)"

if _TREEMAN_FORGE="" _TREEMAN_REMOTE_URL="git@bitbucket.org:o/r.git" _wt_detect_forge 2>/dev/null; then
  fail "detect_forge should reject unsupported hosts"
fi

echo "==> URL encoding: _wt_urlencode"
assert_eq "simple path" "owner%2Frepo" "$(_wt_urlencode 'owner/repo')"
assert_eq "nested path" "acme%2Ffrontend%2Fwebapp" \
  "$(_wt_urlencode 'acme/frontend/webapp')"

echo "==> _wt_origin_repo_slug with _TREEMAN_GH_REPO override"
assert_eq "override still works" "shoutcape/TreeMan" "$(_wt_origin_repo_slug)"

echo "==> _wt_origin_repo_slug from remote URL (no override)"
_old_gh_repo="${_TREEMAN_GH_REPO}"
unset _TREEMAN_GH_REPO
export _TREEMAN_REMOTE_URL="git@gitlab.company.com:acme/frontend/webapp.git"
assert_eq "slug from gitlab ssh remote" "acme/frontend/webapp" "$(_wt_origin_repo_slug)"
unset _TREEMAN_REMOTE_URL
export _TREEMAN_GH_REPO="$_old_gh_repo"

# ---------------------------------------------------------------------------
# GitLab MR workflow tests
# ---------------------------------------------------------------------------

echo "==> GitLab mock glab/jq setup"

GITLAB_REMOTE_REPO="$TMP_DIR/gl-remote.git"
GITLAB_MAIN_REPO="$TMP_DIR/gl-project"
GITLAB_MR_SOURCE="$TMP_DIR/gl-mr-source"
GITLAB_REVIEW_WT="$TMP_DIR/gl-project.feature-mr-gamma"

git init --bare "$GITLAB_REMOTE_REPO" >/dev/null
git clone "$GITLAB_REMOTE_REPO" "$GITLAB_MAIN_REPO" >/dev/null
git -C "$GITLAB_MAIN_REPO" switch -c main >/dev/null
printf 'gitlab hello\n' > "$GITLAB_MAIN_REPO/README.md"
git -C "$GITLAB_MAIN_REPO" add README.md
git -C "$GITLAB_MAIN_REPO" commit -m "init gitlab" >/dev/null
git -C "$GITLAB_MAIN_REPO" push -u origin main >/dev/null

git clone "$GITLAB_REMOTE_REPO" "$GITLAB_MR_SOURCE" >/dev/null
git -C "$GITLAB_MR_SOURCE" switch -c main origin/main >/dev/null
git -C "$GITLAB_MR_SOURCE" switch -c feature/mr-gamma >/dev/null
printf 'gamma mr\n' > "$GITLAB_MR_SOURCE/gamma.txt"
git -C "$GITLAB_MR_SOURCE" add gamma.txt
git -C "$GITLAB_MR_SOURCE" commit -m "gamma mr" >/dev/null
git -C "$GITLAB_MR_SOURCE" push -u origin feature/mr-gamma >/dev/null
# GitLab uses merge-requests/<iid>/head refs
git -C "$GITLAB_MR_SOURCE" push origin HEAD:refs/merge-requests/42/head >/dev/null

# Create mock glab that returns JSON (glab api does not support --jq)
cat > "$MOCK_BIN/glab" <<'GLABEOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "api" ]]; then
  endpoint="${2:-}"

  case "$endpoint" in
    */merge_requests/42)
      printf '%s\n' "${MOCK_GLAB_VIEW_42:-}"
      exit 0
      ;;
    *"merge_requests?state=opened&per_page=100"*)
      printf '%s\n' "${MOCK_GLAB_LIST:-}"
      exit 0
      ;;
  esac
fi

echo "unsupported glab invocation: $*" >&2
exit 1
GLABEOF
chmod +x "$MOCK_BIN/glab"

echo "==> GitLab review worktree create (wtmr)"
cd "$GITLAB_MAIN_REPO"

# Switch to GitLab forge for these tests
export _TREEMAN_FORGE="gitlab"
unset _TREEMAN_GH_REPO
export _TREEMAN_REMOTE_URL="git@gitlab.company.com:acme/frontend/gl-project.git"

export MOCK_GLAB_VIEW_42='{"iid":42,"title":"Gamma MR","source_branch":"feature/mr-gamma","author":{"username":"testuser"}}'
wtmr 42 >/dev/null
assert_exists "$GITLAB_REVIEW_WT"
assert_exists "$GITLAB_REVIEW_WT/gamma.txt"
[[ "$(pwd)" == "$GITLAB_REVIEW_WT" ]] || fail "Expected wtmr to cd into GitLab review worktree"
git -C "$GITLAB_MAIN_REPO" show-ref --verify --quiet refs/heads/feature/mr-gamma || fail "Expected feature/mr-gamma branch"

cd "$GITLAB_MAIN_REPO"
if wtmr nope >/dev/null 2>&1; then
  fail "wtmr should reject non-numeric input for GitLab"
fi

# Restore GitHub forge for remaining tests
export _TREEMAN_FORGE="github"
export _TREEMAN_GH_REPO="shoutcape/TreeMan"
unset _TREEMAN_REMOTE_URL

cd "$MAIN_REPO"

echo "==> protected deletions"
if _wt_lazygit_delete_branch main >/dev/null 2>&1; then
  fail "Should not delete default branch"
fi

git -C "$MAIN_REPO" branch scratch >/dev/null
if _wt_lazygit_delete_branch scratch >/dev/null 2>&1; then
  fail "Should not delete branch without removable worktree"
fi

echo "==> safe deletion"
cd "$WORKTREE_REPO"
_wt_delete_worktree_and_branch "$WORKTREE_REPO" feature/test >/dev/null
[[ "$(pwd)" == "$MAIN_REPO" ]] || fail "Expected delete helper to cd back to main worktree"
assert_missing "$WORKTREE_REPO"
if git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/test; then
  fail "Expected feature/test branch to be deleted"
fi

echo "PASS"
