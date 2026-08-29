# TreeMan Development

## Development Paths

- Repository root: `/home/shoutcape/github/TreeMan`
- Git worktrees: `/home/shoutcape/github/TreeMan/.worktrees/`
- Current worktree: the directory containing this `AGENTS.md`
- Go entrypoint: `cmd/treeman/main.go`
- Application code and tests: `internal/`
- Documentation: `docs/`
- Smoke test: `smoke-test.sh`
- Local installer: `install-local.sh`
- Installed binary: `~/.treeman/bin/treeman`

## Testing Repositories

- Manual integration lab: `/home/shoutcape/github/treeman-lab/`
- Lab bare remote: `/home/shoutcape/github/treeman-lab-origin.git/`
- External project fixtures: `/home/shoutcape/github/treeman-testing/`
- Bubble Tea checkout: `/home/shoutcape/github/treeman-testing/bubbletea/`
- Other external fixtures under `treeman-testing/`: `bat/`, `cal.com/`,
  `directus/`, `immich/`, `mattermost/`, `supabase/`, and `vite/`

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
