package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/queue"
)

// drainDeleteQueue processes any pending deletions queued by earlier
// 'treeman delete' invocations. Called from root PersistentPreRunE.
//
// Entries are processed sequentially. Successful entries are removed from
// the queue. Failed entries are retained for retry on the next run.
func drainDeleteQueue() error {
	entries, err := queue.Peek()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read delete queue: %v\n", err)
		return nil // non-fatal: don't block the requested command
	}
	if len(entries) == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "draining %d queued deletion(s)...\n", len(entries))

	failCount, err := queue.Drain(func(e queue.Entry) error {
		fmt.Fprintf(os.Stderr, "  deleting %s...\n", e.Branch)

		// Skip if currently inside the target worktree.
		// The user will naturally leave it, and the next drain will succeed.
		cwd, _ := os.Getwd()
		if strings.HasPrefix(cwd, e.Path) {
			fmt.Fprintf(os.Stderr, "    skipped: currently inside this worktree\n")
			return fmt.Errorf("inside worktree")
		}

		// Load config from the entry's repo root.
		cfgResult := config.Load(e.RepoRoot)
		if cfgResult.Warning != "" {
			fmt.Fprintf(os.Stderr, "    Warning: %s\n", cfgResult.Warning)
		}
		dbEnvKey := cfgResult.Config.DatabaseEnvKey()

		termCfg := config.MergeTerminalConfig(
			config.LoadGlobal("").Config.Terminal,
			cfgResult.Config.Terminal,
		)

		err := deleteWorktreeAndBranchDrain(e.Path, e.Branch, e.RepoRoot, dbEnvKey, termCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    error: %v\n", err)
		}
		return err
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: queue file error: %v\n", err)
	}
	if failCount > 0 {
		fmt.Fprintf(os.Stderr, "  %d deletion(s) failed and will retry on next run\n", failCount)
	}

	return nil // never block the requested command
}
