package cmd

import (
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/state"
	"github.com/spf13/cobra"
)

// prepareApprovedCreationPaths resolves the destination and the hook consent
// that goes with it, in that order. Approval names the exact commands and
// shows the worktree parent directory, before any ref-mutating fetch.
//
// Every creation flow enters here rather than pairing the two calls itself. A
// creationPaths whose consent was never resolved would run no hooks at all and
// report success, so it must not be possible to hold one.
func prepareApprovedCreationPaths(cmd *cobra.Command, mainRoot, branch, parentDir string, opts creationSetupOptions) (creationPaths, error) {
	paths, err := prepareCreationPathsIn(mainRoot, branch, parentDir)
	if err != nil {
		return creationPaths{}, err
	}
	return approveCreationHooks(cmd, paths, opts)
}

func approveCreationHooks(cmd *cobra.Command, paths creationPaths, opts creationSetupOptions) (creationPaths, error) {
	paths.options = opts
	commands := paths.config.PostCreateHooks()
	if opts.skipHooks || len(commands) == 0 {
		return paths, nil
	}
	approved, err := approveHooks(cmd, paths.projectPaths, commands, opts.trustHooks,
		fmt.Sprintf("Hooks run in the new worktree under %q", paths.parentDir))
	if err != nil {
		return paths, err
	}
	paths.hooks = approved
	return paths, nil
}

func approveHooks(cmd *cobra.Command, project projectPaths, commands []string, trust bool, location string) (hookApproval, error) {
	if !trust {
		if err := requireHookApprovalFor(cmd, hookApprovalRequest{
			commonDir:  project.protected.CommonDir,
			configPath: project.configPath,
			commands:   commands,
			location:   location,
		}); err != nil {
			return hookApproval{}, err
		}
	}
	return hookApproval{commands: slices.Clone(commands)}, nil
}

// hookApprovalRequest is one request for consent to run hook commands.
//
// Only the sentence describing where the hooks will run differs between
// creating a worktree and repairing one. The scope does not: the same commands
// in the same repository under the same configuration are the same scope, so
// an approval granted at creation is still the approval a rerun matches.
type hookApprovalRequest struct {
	commonDir  string
	configPath string
	commands   []string
	// location tells the user where the commands will run, in their own
	// terms. Consent to run something in a worktree that already holds work
	// is not the same question as consent to run it in a new one.
	location string
}

func requireHookApprovalFor(cmd *cobra.Command, req hookApprovalRequest) error {
	scope, err := hooks.NewApprovalScope(req.commonDir, req.configPath, hooks.PostCreatePhase, req.commands)
	if err != nil {
		return err
	}
	store, err := state.NewHookApprovalStore(req.commonDir)
	if err != nil {
		return fmt.Errorf("hook approval state: %w", err)
	}
	approved, err := store.Lookup(scope)
	if err != nil {
		return fmt.Errorf("hook approval state: %w", err)
	}
	if approved {
		return nil
	}
	// Do not print a question when the session cannot answer it.
	unavailable := fmt.Errorf("hook approval required; rerun with --trust-hooks to authorize this invocation or --skip-hooks to skip hooks")
	if !canInteract(cmd) {
		return unavailable
	}
	out := cmd.ErrOrStderr()
	writeHookScope(out, scope)
	fmt.Fprintln(out, req.location)
	fmt.Fprintln(out, "Approval permits these command strings, not just their current script contents. Hooks are not sandboxed.")
	granted, err := confirmYN(cmd, "Approve and save this exact scope for future use? [y/N] ", unavailable)
	if err != nil {
		return err
	}
	if !granted {
		return fmt.Errorf("hook approval refused")
	}
	if err := store.Approve(scope); err != nil {
		return fmt.Errorf("save hook approval: %w", err)
	}
	return nil
}

func writeHookScope(out io.Writer, scope hooks.ApprovalScope) {
	fmt.Fprintf(out, "Repository: %q\nConfig: %q\nPhase: %q\n", scope.Repository, scope.ConfigPath, scope.Phase)
	for i, command := range scope.Commands {
		// Quoting preserves complete command text while escaping terminal controls.
		fmt.Fprintf(out, "  %d. %q\n", i+1, command)
	}
}

func newHooksCmd() *cobra.Command {
	command := &cobra.Command{Use: "hooks", Short: "Manage hook command consent"}
	approvals := &cobra.Command{Use: "approvals", Short: "Inspect or revoke saved hook approvals"}
	approvals.AddCommand(&cobra.Command{
		Use: "list", Short: "List saved hook approvals", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := state.NewHookApprovalStore("")
			if err != nil {
				return err
			}
			records, err := store.List()
			if err != nil {
				return err
			}
			for _, record := range records {
				fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nApproved: %s\n", record.ID, record.ApprovedAt.Format(time.RFC3339Nano))
				writeHookScope(cmd.OutOrStdout(), record.Scope)
				fmt.Fprintln(cmd.OutOrStdout())
			}
			if len(records) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No saved hook approvals.")
			}
			return nil
		},
	})
	approvals.AddCommand(&cobra.Command{
		Use: "revoke <id>", Short: "Revoke one exact approval ID", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := state.NewHookApprovalStore("")
			if err != nil {
				return err
			}
			if err := store.Revoke(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked hook approval %s\n", args[0])
			return nil
		},
	})
	command.AddCommand(approvals)
	return command
}
