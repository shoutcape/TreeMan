package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/shoutcape/treeman/internal/fsutil"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"
)

// claudePermissionInput is the part of Claude Code's PermissionRequest hook
// input TreeMan reads.
type claudePermissionInput struct {
	claudeHookInput
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Path string `json:"path"`
	} `json:"tool_input"`
}

func newClaudePermissionRequestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "permission-request",
		Short: "Approve entering TreeMan worktrees for Claude Code's PermissionRequest hook",
		Long: `Read Claude Code's PermissionRequest hook input on stdin and approve an
EnterWorktree call into a TreeMan worktree.

Claude Code asks before a session enters a worktree outside .claude/worktrees/,
which is where every TreeMan worktree is, so a background session would wait at
that question. This approves the call when its path is a linked worktree of the
session's repository inside TreeMan's worktree directory. For anything else it
prints nothing, and Claude Code asks as usual. It never fails the hook.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			allow, err := claudeApprovesPermission(cmd.InOrStdin())
			if err != nil {
				// Declining is the safe answer: Claude Code shows its own
				// question.
				fmt.Fprintf(cmd.ErrOrStderr(), "treeman: not approving: %v\n", err)
				return nil
			}
			if !allow {
				return nil
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`)
			return err
		},
	}
}

func claudeApprovesPermission(r io.Reader) (bool, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return false, fmt.Errorf("read hook input: %w", err)
	}
	var input claudePermissionInput
	if err := json.Unmarshal(data, &input); err != nil {
		return false, fmt.Errorf("read hook input: %w", err)
	}
	if input.ToolName != "EnterWorktree" || input.ToolInput.Path == "" {
		return false, nil
	}
	projectDir, err := filepath.Abs(claudeProjectDir(input.claudeHookInput))
	if err != nil {
		return false, err
	}
	target := input.ToolInput.Path
	if !filepath.IsAbs(target) {
		base := input.Cwd
		if base == "" {
			base = projectDir
		}
		target = filepath.Join(base, target)
	}
	return isTreemanWorktreeOf(projectDir, target)
}

// isTreemanWorktreeOf reports whether path is a linked worktree of the
// repository at projectDir that sits inside TreeMan's worktree directory for
// it: the configured one, or the one a bare repository's worktrees go to.
func isTreemanWorktreeOf(projectDir, path string) (bool, error) {
	repo, err := openClaudeRepository(projectDir, true)
	if err != nil {
		return false, err
	}
	parentDir := repo.parentDir
	if parentDir == "" {
		project, err := loadProjectPaths(repo.mainRoot)
		if err != nil {
			return false, err
		}
		parentDir = project.parentDir
	}
	canonicalParent, err := fsutil.CanonicalPath(parentDir)
	if err != nil {
		return false, err
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !samePath(entry.Path, path) || samePath(entry.Path, repo.mainRoot) {
			continue
		}
		if git.WorktreeDirectoryMissing(entry.Path) {
			return false, nil
		}
		canonicalPath, err := fsutil.CanonicalPath(entry.Path)
		if err != nil {
			return false, err
		}
		return fsutil.Contains(canonicalParent, canonicalPath), nil
	}
	return false, nil
}
