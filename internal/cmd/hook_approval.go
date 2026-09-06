package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/shoutcape/treeman/internal/hooks"
	"github.com/shoutcape/treeman/internal/state"
	"github.com/spf13/cobra"
)

// prepareApprovedCreationPaths resolves the destination and the hook consent
// that goes with it, in that order: approval names the exact commands and the
// directory they would run in, so it can only be asked for once both are
// known, and it has to be settled before any ref-mutating fetch.
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
	paths.hooks = hookApproval{}
	commands := append([]string(nil), paths.config.PostCreateHooks()...)
	if opts.skipHooks || len(commands) == 0 {
		return paths, nil
	}
	if !opts.trustHooks {
		scope, err := hooks.NewApprovalScope(paths.protected.CommonDir, paths.configPath, hooks.PostCreatePhase, commands)
		if err != nil {
			return paths, err
		}
		store, err := state.NewHookApprovalStore()
		if err != nil {
			return paths, fmt.Errorf("hook approval state: %w", err)
		}
		approved, err := store.Lookup(scope)
		if err != nil {
			return paths, fmt.Errorf("hook approval state: %w", err)
		}
		if !approved {
			// The scope is the question. A session that cannot be asked must
			// not have it printed as though it had been.
			unavailable := fmt.Errorf("hook approval required; rerun with --trust-hooks to authorize this invocation or --skip-hooks to skip hooks")
			if !canInteract(cmd) {
				return paths, unavailable
			}
			out := cmd.ErrOrStderr()
			writeHookScope(out, scope)
			fmt.Fprintf(out, "Execution directory: %q\n", paths.path)
			fmt.Fprintln(out, "Approval permits these command strings, not just their current script contents. Hooks are not sandboxed.")
			granted, err := confirmYN(cmd, "Approve and save this exact scope for future use? [y/N] ", unavailable)
			if err != nil {
				return paths, err
			}
			if !granted {
				return paths, fmt.Errorf("hook approval refused")
			}
			if err := store.Approve(scope); err != nil {
				return paths, fmt.Errorf("save hook approval: %w", err)
			}
		}
	}
	paths.hooks = hookApproval{commands: commands, dir: paths.path}
	return paths, nil
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
			store, err := state.NewHookApprovalStore()
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
			store, err := state.NewHookApprovalStore()
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
