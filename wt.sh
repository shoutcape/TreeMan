# TreeMan — shell adapter
# Sources into bash/zsh to provide cd-capable aliases for the treeman binary.
#
# All logic lives in the `treeman` binary. This file only wraps commands
# that need to change the shell's working directory (which a subprocess
# cannot do).
#
# Usage:
#   wt   <branch-name>     Create a new worktree + branch
#   wtpr [pr-number]       Create a review worktree from a GitHub PR or GitLab MR
#   wtmr [pr-number]       Same as wtpr (PR/MR are interchangeable here)
#   wts  [query]           Switch between worktrees (fzf picker)
#   wtd  [query]           Delete a worktree and its branch (fzf picker)
#   lg   [args...]         Run lazygit with auto-cd on worktree switch

wt() {
  local dir
  dir=$(treeman worktree create "$@") || return $?
  cd "$dir" || return 1
}

wtpr() {
  local dir
  dir=$(treeman worktree review "$@") || return $?
  cd "$dir" || return 1
}

wtmr() {
  local dir
  dir=$(treeman worktree review "$@") || return $?
  cd "$dir" || return 1
}

wts() {
  local dir
  dir=$(treeman worktree switch "$@") || return $?
  [ -n "$dir" ] && cd "$dir" || return 0
}

wtd() {
  local dir
  dir=$(treeman worktree delete "$@") || return $?
  [ -n "$dir" ] && [ "$dir" != "$(pwd)" ] && cd "$dir"
  return 0
}

lg() {
  if ! command -v lazygit >/dev/null 2>&1; then
    echo "Warning: lazygit is not installed." >&2
    return 1
  fi

  local newdir_file="$HOME/.lazygit/newdir"
  mkdir -p "$HOME/.lazygit"

  LAZYGIT_NEW_DIR_FILE="$newdir_file" lazygit "$@"

  if [ -f "$newdir_file" ]; then
    local target
    target=$(cat "$newdir_file")
    rm -f "$newdir_file"
    if [ -n "$target" ] && [ "$target" != "$(pwd)" ]; then
      cd "$target" || return 1
    fi
  fi
}
