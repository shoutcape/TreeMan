# TreeMan Development

## Installing TreeMan

- The installed `treeman` binary is `~/.treeman/bin/treeman`.
- Install the latest released version with:

  ```sh
  curl -fsSL https://raw.githubusercontent.com/shoutcape/TreeMan/main/install.sh | bash
  ```

- To test unmerged local changes, run these commands from the worktree that contains the changes:

  ```sh
  ./install-local.sh
  ```

- Restart the shell after either installation with `exec "$SHELL"`.
