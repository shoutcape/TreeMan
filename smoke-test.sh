#!/usr/bin/env bash
# TreeMan smoke test — end-to-end tests for the Go binary.
#
# Tests install/uninstall, worktree create, review (GitHub + GitLab),
# switch, delete, and all guards using mock gh/glab/fzf binaries.
#
# Usage:
#   ./smoke-test.sh                     # uses ./bin/treeman
#   TREEMAN_BIN=/path/to/treeman ./smoke-test.sh

set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

# Resolve the binary to test.
TREEMAN_BIN="${TREEMAN_BIN:-$SCRIPT_DIR/bin/treeman}"
if [[ ! -x "$TREEMAN_BIN" ]]; then
  echo "Error: treeman binary not found at $TREEMAN_BIN" >&2
  echo "       Run 'make build' first, or set TREEMAN_BIN=/path/to/treeman" >&2
  exit 1
fi

TMP_DIR=$(mktemp -d)
# Resolve symlinks (macOS /var → /private/var) so paths match git worktree output.
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
  # Go's module cache uses read-only files; chmod before removing.
  chmod -R u+w "$TMP_DIR" 2>/dev/null || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

# --- Assertion helpers -------------------------------------------------------

fail() { echo "FAIL: $1" >&2; exit 1; }

assert_exists()           { [[ -e "$1" ]] || fail "Expected $1 to exist"; }
assert_missing()          { [[ ! -e "$1" ]] || fail "Expected $1 to be missing"; }
assert_file_contains()    { grep -qF "$2" "$1" || fail "Expected '$2' in $1"; }
assert_file_not_contains() {
  if [[ -f "$1" ]] && grep -qF "$2" "$1"; then
    fail "Did not expect '$2' in $1"
  fi
}
assert_eq() {
  local label="$1" expected="$2" actual="$3"
  [[ "$expected" == "$actual" ]] || fail "$label: expected '$expected', got '$actual'"
}

# --- Environment setup -------------------------------------------------------

mkdir -p "$TEST_HOME"
touch "$TEST_HOME/.bashrc"

# Preserve real HOME/GOPATH before overriding, needed for unit test step.
REAL_HOME="$HOME"
REAL_GOPATH="${GOPATH:-$HOME/go}"

export HOME="$TEST_HOME"
export XDG_CONFIG_HOME="$HOME/.config"
export GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME="TreeMan Test"
export GIT_AUTHOR_EMAIL="test@example.com"
export GIT_COMMITTER_NAME="TreeMan Test"
export GIT_COMMITTER_EMAIL="test@example.com"

# Forge/repo overrides — injected into the treeman binary via env vars so that
# forge detection and API routing work against the local bare repo without a
# real GitHub/GitLab remote URL (mirrors wt.sh test hooks).
export _TREEMAN_FORGE="github"
export _TREEMAN_GH_REPO="shoutcape/TreeMan"

mkdir -p "$XDG_CONFIG_HOME"
mkdir -p "$LAZYGIT_CONFIG_DIR"

# Put the treeman binary on PATH for subshell invocations.
mkdir -p "$TEST_HOME/.treeman/bin"
cp "$TREEMAN_BIN" "$TEST_HOME/.treeman/bin/treeman"
export PATH="$TEST_HOME/.treeman/bin:$MOCK_BIN:$PATH"

# Load shell wrappers (wt, wtpr, wtmr, wts, wtd) into this shell session.
eval "$(treeman init bash)"

# ---------------------------------------------------------------------------
# install / uninstall
# ---------------------------------------------------------------------------

echo "==> install/uninstall"

TREEMAN_LOCAL_BIN="$TREEMAN_BIN" \
  TREEMAN_INSTALL_DIR="$TEST_HOME/.treeman-install-test" \
  TREEMAN_SHELL_RC="$TEST_HOME/.bashrc" \
  TREEMAN_LAZYGIT_CONFIG_DIR="$LAZYGIT_CONFIG_DIR" \
  bash "$SCRIPT_DIR/install.sh"

# Binary should be in the install dir.
assert_exists "$TEST_HOME/.treeman-install-test/bin/treeman"

# Shell rc must contain the marker and both new lines.
assert_file_contains "$TEST_HOME/.bashrc" '# TreeMan'
assert_file_contains "$TEST_HOME/.bashrc" '.treeman-install-test/bin'
assert_file_contains "$TEST_HOME/.bashrc" 'treeman init'

# Lazygit config must reference the treeman binary (not wt.sh).
assert_file_contains "$LAZYGIT_CONFIG_DIR/config.yml" 'treeman create'
assert_file_contains "$LAZYGIT_CONFIG_DIR/config.yml" 'treeman delete'
assert_file_not_contains "$LAZYGIT_CONFIG_DIR/config.yml" 'wt.sh'

# Uninstall.
TREEMAN_INSTALL_DIR="$TEST_HOME/.treeman-install-test" \
  TREEMAN_LAZYGIT_CONFIG_DIR="$LAZYGIT_CONFIG_DIR" \
  bash "$SCRIPT_DIR/uninstall.sh"

assert_missing "$TEST_HOME/.treeman-install-test"
assert_file_not_contains "$TEST_HOME/.bashrc" '# TreeMan'
assert_file_not_contains "$LAZYGIT_CONFIG_DIR/config.yml" '# TreeMan'

# ---------------------------------------------------------------------------
# repository setup
# ---------------------------------------------------------------------------

echo "==> repository setup"

git init --bare "$REMOTE_REPO" >/dev/null
git clone "$REMOTE_REPO" "$MAIN_REPO" >/dev/null
git -C "$MAIN_REPO" switch -c main >/dev/null
printf 'hello\n' > "$MAIN_REPO/README.md"
git -C "$MAIN_REPO" add README.md
git -C "$MAIN_REPO" commit -m "init" >/dev/null
git -C "$MAIN_REPO" push -u origin main >/dev/null

# ---------------------------------------------------------------------------
# mock gh / glab / fzf
# ---------------------------------------------------------------------------

echo "==> mock gh/glab/fzf setup"

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

# fzf mock: prints the line number given by FZF_CHOICE (1-based).
# The Go binary pipes display lines to fzf stdin; the mock returns one line.
cat > "$MOCK_BIN/fzf" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
sed -n "${FZF_CHOICE:-1}p"
EOF
chmod +x "$MOCK_BIN/fzf"

# ---------------------------------------------------------------------------
# review branch setup (GitHub)
# ---------------------------------------------------------------------------

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

# ---------------------------------------------------------------------------
# worktree create — wt
# ---------------------------------------------------------------------------

echo "==> worktree create"

cd "$MAIN_REPO"
wt feature/test
assert_exists "$WORKTREE_REPO"
[[ "$(pwd)" == "$WORKTREE_REPO" ]] || fail "Expected wt to cd into created worktree"
git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/test \
  || fail "Expected feature/test branch to exist"

# Guard: duplicate branch.
cd "$MAIN_REPO"
if wt feature/test >/dev/null 2>&1; then
  fail "wt should reject duplicate branch names"
fi

# Guard: invalid branch name.
if wt "bad name" >/dev/null 2>&1; then
  fail "wt should reject branch names with spaces"
fi

# ---------------------------------------------------------------------------
# review worktree create — wtpr (GitHub, explicit number)
# ---------------------------------------------------------------------------

echo "==> review worktree create (GitHub wtpr)"

cd "$MAIN_REPO"
export MOCK_GH_VIEW_123='{"number":123,"title":"Alpha review","head":{"ref":"feature/review-alpha","repo":{"owner":{"login":"shoutcape"}}}}'
wtpr 123
assert_exists "$REVIEW_WT_ALPHA"
assert_exists "$REVIEW_WT_ALPHA/review-alpha.txt"
[[ "$(pwd)" == "$REVIEW_WT_ALPHA" ]] || fail "Expected wtpr to cd into created review worktree"
git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/review-alpha \
  || fail "Expected feature/review-alpha branch"
# Verify upstream is set correctly.
UPSTREAM=$(git -C "$REVIEW_WT_ALPHA" rev-parse --abbrev-ref --symbolic-full-name @{upstream} 2>/dev/null)
[[ "$UPSTREAM" == "origin/feature/review-alpha" ]] \
  || fail "Expected upstream origin/feature/review-alpha, got: $UPSTREAM"

# Guard: non-numeric PR number.
cd "$MAIN_REPO"
if wtpr nope >/dev/null 2>&1; then
  fail "wtpr should reject non-numeric input"
fi

# ---------------------------------------------------------------------------
# review picker — wtmr (GitHub, fzf selection)
# ---------------------------------------------------------------------------

echo "==> review picker (GitHub wtmr fzf)"

cd "$MAIN_REPO"
export MOCK_GH_VIEW_124='{"number":124,"title":"Beta review","head":{"ref":"feature/review-beta","repo":{"owner":{"login":"shoutcape"}}}}'
export MOCK_GH_LIST='[{"number":123,"title":"Alpha review","head":{"ref":"feature/review-alpha"}},{"number":124,"title":"Beta review","head":{"ref":"feature/review-beta"}}]'
# FZF_CHOICE=3 selects the second data row (row 1 = header, row 2 = #123, row 3 = #124).
export FZF_CHOICE=3
wtmr
assert_exists "$REVIEW_WT_BETA"
assert_exists "$REVIEW_WT_BETA/review-beta.txt"
[[ "$(pwd)" == "$REVIEW_WT_BETA" ]] || fail "Expected wtmr to cd into created review worktree"
git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/review-beta \
  || fail "Expected feature/review-beta branch"
unset FZF_CHOICE

# ---------------------------------------------------------------------------
# GitLab MR workflow
# ---------------------------------------------------------------------------

echo "==> GitLab mock setup"

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
git -C "$GITLAB_MR_SOURCE" push origin HEAD:refs/merge-requests/42/head >/dev/null

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

# jq is required by glab integration — provide a passthrough mock if absent.
if ! command -v jq >/dev/null 2>&1; then
  cat > "$MOCK_BIN/jq" <<'EOF'
#!/usr/bin/env bash
cat
EOF
  chmod +x "$MOCK_BIN/jq"
fi

echo "==> review worktree create (GitLab wtmr)"

cd "$GITLAB_MAIN_REPO"
export _TREEMAN_FORGE="gitlab"
unset _TREEMAN_GH_REPO
export _TREEMAN_REMOTE_URL="git@gitlab.company.com:acme/frontend/gl-project.git"
export MOCK_GLAB_VIEW_42='{"iid":42,"title":"Gamma MR","source_branch":"feature/mr-gamma","author":{"username":"testuser"}}'

wtmr 42
assert_exists "$GITLAB_REVIEW_WT"
assert_exists "$GITLAB_REVIEW_WT/gamma.txt"
[[ "$(pwd)" == "$GITLAB_REVIEW_WT" ]] || fail "Expected wtmr to cd into GitLab review worktree"
git -C "$GITLAB_MAIN_REPO" show-ref --verify --quiet refs/heads/feature/mr-gamma \
  || fail "Expected feature/mr-gamma branch"

# Guard: non-numeric MR number.
cd "$GITLAB_MAIN_REPO"
if wtmr nope >/dev/null 2>&1; then
  fail "wtmr should reject non-numeric input for GitLab"
fi

# Restore GitHub forge overrides for remaining tests.
export _TREEMAN_FORGE="github"
export _TREEMAN_GH_REPO="shoutcape/TreeMan"
unset _TREEMAN_REMOTE_URL

cd "$MAIN_REPO"

# ---------------------------------------------------------------------------
# protected deletions — treeman delete --path/--branch/--yes
# ---------------------------------------------------------------------------

echo "==> protected deletions"

# Cannot delete the default branch.
if treeman delete --branch main --path "$MAIN_REPO" --yes >/dev/null 2>&1; then
  fail "Should not delete default branch"
fi

# Cannot delete from a branch that has no worktree.
git -C "$MAIN_REPO" branch scratch >/dev/null
if treeman delete --branch scratch --path "$MAIN_REPO" --yes >/dev/null 2>&1; then
  fail "Should not delete the main worktree path"
fi
git -C "$MAIN_REPO" branch -D scratch >/dev/null

# ---------------------------------------------------------------------------
# safe deletion — treeman delete --path/--branch/--yes
# ---------------------------------------------------------------------------

echo "==> safe deletion"

# We are currently in MAIN_REPO; delete WORKTREE_REPO non-interactively.
treeman delete \
  --path "$WORKTREE_REPO" \
  --branch feature/test \
  --yes

assert_missing "$WORKTREE_REPO"
if git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/test; then
  fail "Expected feature/test branch to be deleted"
fi

# ---------------------------------------------------------------------------
# switch — wts (fzf mock, selects first non-current entry)
# ---------------------------------------------------------------------------

echo "==> worktree switch"

cd "$MAIN_REPO"
# FZF_CHOICE=2 picks the second display row, which is a non-main worktree.
# (Row 1 = MAIN_REPO itself, row 2 = first linked worktree.)
export FZF_CHOICE=2
wts
# After switching we should be in a different worktree.
[[ "$(pwd)" != "$MAIN_REPO" ]] || fail "Expected wts to cd into a different worktree"
unset FZF_CHOICE

# ---------------------------------------------------------------------------
# unit tests (sanity check that they still pass in CI context)
# ---------------------------------------------------------------------------

echo "==> unit tests"
cd "$SCRIPT_DIR"
# Run with the real HOME/GOPATH and without smoke-test env overrides that
# would interfere with forge detection tests (_TREEMAN_FORGE, etc.).
env -u _TREEMAN_FORGE -u _TREEMAN_GH_REPO -u _TREEMAN_REMOTE_URL \
  HOME="$REAL_HOME" GOPATH="${REAL_GOPATH:-$REAL_HOME/go}" \
  go test ./... >/dev/null

echo "PASS"
