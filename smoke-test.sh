#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
TMP_DIR=$(mktemp -d)
TMP_DIR=$(cd "$TMP_DIR" && pwd -P)

TEST_HOME="$TMP_DIR/home"
LAZYGIT_CONFIG_DIR="$TMP_DIR/lazygit"
MOCK_BIN="$TMP_DIR/bin"
LOCAL_TREEMAN="$MOCK_BIN/treeman"

REMOTE_REPO="$TMP_DIR/remote.git"
MAIN_REPO="$TMP_DIR/project"
PR_SOURCE_REPO="$TMP_DIR/pr-source"
WORKTREE_REPO="$TMP_DIR/project.feature-test"
REVIEW_WT_ALPHA="$TMP_DIR/project.feature-review-alpha"
REVIEW_WT_BETA="$TMP_DIR/project.feature-review-beta"
RUNTIME_WT="$TMP_DIR/project.feature-runtime-test"

cleanup() {
	chmod -R u+w "$TMP_DIR" 2>/dev/null || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

assert_exists() {
  local path="$1"
  [[ -e "$path" ]] || fail "Expected $path to exist"
}

assert_missing() {
  local path="$1"
  [[ ! -e "$path" ]] || fail "Expected $path to be missing"
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

mkdir -p "$TEST_HOME" "$LAZYGIT_CONFIG_DIR" "$MOCK_BIN"
touch "$TEST_HOME/.bashrc"

export HOME="$TEST_HOME"
export XDG_CONFIG_HOME="$HOME/.config"
export GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME="TreeMan Test"
export GIT_AUTHOR_EMAIL="test@example.com"
export GIT_COMMITTER_NAME="TreeMan Test"
export GIT_COMMITTER_EMAIL="test@example.com"
export PATH="$MOCK_BIN:$PATH"

mkdir -p "$XDG_CONFIG_HOME"

echo "==> build local treeman"
go build -o "$LOCAL_TREEMAN" ./cmd/treeman

echo "==> install/uninstall"
HOME="$TEST_HOME" \
  TREEMAN_INSTALL_DIR="$HOME/.treeman" \
  TREEMAN_BIN_PATH="$LOCAL_TREEMAN" \
  TREEMAN_WT_SH_PATH="$ROOT_DIR/wt.sh" \
  TREEMAN_SHELL_RC="$HOME/.bashrc" \
  TREEMAN_LAZYGIT_CONFIG_DIR="$LAZYGIT_CONFIG_DIR" \
  bash "$ROOT_DIR/install.sh"

assert_exists "$HOME/.treeman/treeman"
assert_exists "$HOME/.treeman/wt.sh"
assert_file_contains "$HOME/.bashrc" '# TreeMan'
assert_file_contains "$HOME/.bashrc" 'source "'
assert_file_contains "$LAZYGIT_CONFIG_DIR/config.yml" 'TreeMan'

HOME="$TEST_HOME" \
  TREEMAN_INSTALL_DIR="$HOME/.treeman" \
  TREEMAN_LAZYGIT_CONFIG_DIR="$LAZYGIT_CONFIG_DIR" \
  bash "$ROOT_DIR/uninstall.sh"

assert_missing "$HOME/.treeman"
assert_file_not_contains "$HOME/.bashrc" '# TreeMan'
assert_file_not_contains "$LAZYGIT_CONFIG_DIR/config.yml" '# TreeMan'

echo "==> repository setup"
git init --bare "$REMOTE_REPO" >/dev/null
git clone "$REMOTE_REPO" "$MAIN_REPO" >/dev/null
git -C "$MAIN_REPO" switch -c main >/dev/null
printf 'hello\n' > "$MAIN_REPO/README.md"
git -C "$MAIN_REPO" add README.md
git -C "$MAIN_REPO" commit -m "init" >/dev/null
git -C "$MAIN_REPO" push -u origin main >/dev/null

echo "==> mock gh/fzf setup"
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

query=""
for arg in "$@"; do
  case "$arg" in
    --query=*)
      query="${arg#--query=}"
      ;;
  esac
done

if [[ -n "$query" ]]; then
  grep -F "$query" | sed -n "${FZF_CHOICE:-1}p"
else
  sed -n "${FZF_CHOICE:-1}p"
fi
EOF
chmod +x "$MOCK_BIN/fzf"

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

echo "==> wrapper commands"
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
export _TREEMAN_FORGE="github"
export _TREEMAN_GH_REPO="shoutcape/TreeMan"
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
export FZF_CHOICE=3
wtmr >/dev/null
assert_exists "$REVIEW_WT_BETA"
assert_exists "$REVIEW_WT_BETA/review-beta.txt"
[[ "$(pwd)" == "$REVIEW_WT_BETA" ]] || fail "Expected wtmr to cd into created review worktree"
git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/review-beta || fail "Expected feature/review-beta branch"
unset FZF_CHOICE

echo "==> switcher"
cd "$MAIN_REPO"
export FZF_CHOICE=1
wts feature/test >/dev/null
[[ "$(pwd)" == "$WORKTREE_REPO" ]] || fail "Expected wts to cd into selected worktree"
unset FZF_CHOICE

echo "==> runtime"
cat > "$MAIN_REPO/.treeman.yml" <<'EOF'
runtime:
  type: process
  command: sleep 5
  env_file: config/.env.treeman
  ports:
    app: 3000
EOF
mkdir -p "$MAIN_REPO/config"

cd "$MAIN_REPO"
treeman runtime up >/dev/null
assert_exists "$MAIN_REPO/config/.env.treeman"
assert_missing "$MAIN_REPO/.env.treeman"
treeman runtime env > "$TMP_DIR/runtime-env.out"
assert_file_contains "$TMP_DIR/runtime-env.out" 'APP_PORT='
assert_file_contains "$TMP_DIR/runtime-env.out" 'PORT='
treeman runtime down >/dev/null
rm -rf "$MAIN_REPO/config" "$MAIN_REPO/.treeman.yml"

echo "==> runtime cleanup on delete"
cat > "$MAIN_REPO/.treeman.yml" <<'EOF'
runtime:
  type: process
  command: sleep 30
  ports:
    app: 3000
EOF
printf '.env.treeman\n' > "$MAIN_REPO/.gitignore"
git -C "$MAIN_REPO" add .treeman.yml .gitignore
git -C "$MAIN_REPO" commit -m "runtime config" >/dev/null
git -C "$MAIN_REPO" push origin main >/dev/null

treeman worktree create feature/runtime-test >/dev/null
assert_exists "$RUNTIME_WT"

cd "$RUNTIME_WT"
treeman runtime up >/dev/null

cd "$MAIN_REPO"
treeman worktree delete --branch feature/runtime-test >/dev/null
assert_missing "$RUNTIME_WT"
assert_missing "$HOME/.treeman/state/project/feature-runtime-test.json"
if [[ -f "$HOME/.treeman/ports.json" ]] && grep -q 'feature-runtime-test' "$HOME/.treeman/ports.json"; then
  fail "Expected released ports for deleted runtime worktree"
fi

echo "==> safe deletion"
treeman worktree delete --branch feature/test >/dev/null
assert_missing "$WORKTREE_REPO"
if git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/test; then
  fail "Expected feature/test branch to be deleted"
fi

echo "PASS"
