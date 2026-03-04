# Pull Request Descriptions

Here are descriptions you can use for the pull requests you are going to open:

---

## 1. PR: `fix-wtd-force-delete`

**Title:** fix: force delete branch when removing worktree in `wtd`

**Description:**
This PR fixes a bug in the `wtd` (worktree delete) command where it used the soft delete flag (`git branch -d`) when attempting to remove the branch associated with a worktree. 

Because `wtd` deletes the worktree *before* deleting the branch, the branch might appear as unmerged to Git, causing the soft delete to fail and leaving the orphaned branch behind. This PR changes both the standard `wtd` command in `wt.sh` and the lazygit integration in `install-lazygit.sh` to use the force delete flag (`git branch -D`), ensuring the branch is correctly cleaned up alongside its worktree.

---

## 2. PR: `combine-scripts`

**Title:** feat: combine install and uninstall scripts into unified installers

**Description:**
This PR simplifies the installation and uninstallation process by merging the logic of the `uninstall.sh` and `uninstall-lazygit.sh` scripts directly into their installation counterparts.

**Changes:**
- `install.sh` and `install-lazygit.sh` now accept an `uninstall` argument (or `--uninstall` flag).
- Removed the standalone `uninstall.sh` and `uninstall-lazygit.sh` scripts from the repository.
- Updated the `README.md` to document the new `bash -s uninstall` and `bash install-lazygit.sh uninstall` commands.
