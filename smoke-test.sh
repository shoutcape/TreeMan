#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
TMP_DIR=$(mktemp -d)
TEST_HOME="$TMP_DIR/home"
LAZYGIT_CONFIG_DIR="$TMP_DIR/lazygit"
REMOTE_REPO="$TMP_DIR/remote.git"
MAIN_REPO="$TMP_DIR/project"
WORKTREE_REPO="$TMP_DIR/project.feature-test"

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

echo "==> worktree create"
source "$ROOT_DIR/wt.sh"
cd "$MAIN_REPO"
wt feature/test >/dev/null
assert_exists "$WORKTREE_REPO"
git -C "$MAIN_REPO" show-ref --verify --quiet refs/heads/feature/test || fail "Expected feature/test branch"

if wt feature/test >/dev/null 2>&1; then
  fail "wt should reject duplicate branch names"
fi

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
