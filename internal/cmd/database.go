package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/shoutcape/treeman/internal/config"
	"github.com/shoutcape/treeman/internal/database"
	"github.com/shoutcape/treeman/internal/git"
	"github.com/spf13/cobra"
)

type branchDatabase struct {
	Name      string
	Branch    string
	Worktree  string
	Active    bool
	Container string
	BaseURI   string
}

var (
	discoverBranchDatabasesFn = discoverBranchDatabases
	dropRepairDatabaseFn      = database.DropDatabase
)

func newDatabaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "database",
		Short: "Inspect and repair TreeMan branch databases",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newDatabaseInspectCmd())
	cmd.AddCommand(newDatabaseRepairCmd())
	return cmd
}

func newDatabaseInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "List TreeMan branch database state without changes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := discoverBranchDatabasesFn()
			if err != nil {
				return err
			}
			for _, entry := range entries {
				state := "orphaned"
				if entry.Active {
					state = "active"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", entry.Name, state, entry.Branch, entry.Worktree)
			}
			return nil
		},
	}
}

func newDatabaseRepairCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "repair <database>",
		Short: "Remove an orphaned TreeMan branch database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDatabaseRepair(cmd, args[0], yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm removal without prompting")
	return cmd
}

func runDatabaseRepair(cmd *cobra.Command, name string, yes bool) error {
	entries, err := discoverBranchDatabasesFn()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name != name || entry.Active {
			continue
		}
		fmt.Fprintln(os.Stderr, "About to remove orphaned database:")
		fmt.Fprintf(os.Stderr, "  Database: %s\n  Branch:   %s\n  Worktree: %s\n\n", entry.Name, entry.Branch, entry.Worktree)
		if !yes && !confirmYN(cmd, "Remove this database? [y/N] ") {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return nil
		}
		if err := dropRepairDatabaseFn(entry.Container, entry.BaseURI, entry.Name); err != nil {
			return fmt.Errorf("removing orphaned database %q: %w", entry.Name, err)
		}
		fmt.Fprintf(os.Stderr, "Removed orphaned database: %s\n", entry.Name)
		return nil
	}
	return fmt.Errorf("database %q is not a repairable TreeMan orphan", name)
}

func discoverBranchDatabases() ([]branchDatabase, error) {
	if !git.IsInsideRepo() {
		return nil, fmt.Errorf("not inside a git repository")
	}
	mainRoot, err := git.MainWorktreeRoot()
	if err != nil {
		return nil, err
	}
	cfgResult := config.Load(mainRoot)
	if cfgResult.Warning != "" {
		return nil, fmt.Errorf("database configuration invalid: %s", cfgResult.Warning)
	}
	envKey := cfgResult.Config.DatabaseEnvKey()
	if envKey == "" {
		return nil, fmt.Errorf("database management is not configured")
	}
	uri, err := database.ReadDatabaseURI(mainRoot, envKey)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", envKey, err)
	}
	if !isPostgresURI(uri) {
		return nil, fmt.Errorf("%s must contain a PostgreSQL URI", envKey)
	}
	parsed, err := database.ParseURI(uri)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", envKey, err)
	}
	container, err := database.FindPostgresContainer(parsed.Port)
	if err != nil {
		return nil, fmt.Errorf("finding postgres container: %w", err)
	}
	names, err := database.ListDatabases(container, parsed.BaseURI)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
	}
	entries, err := git.WorktreeList()
	if err != nil {
		return nil, err
	}
	expected := make(map[string][]branchDatabase)
	for _, worktree := range entries {
		if worktree.Branch == "" || samePath(worktree.Path, mainRoot) {
			continue
		}
		name := database.BranchDBName(parsed.Database, worktree.Branch)
		if !present[name] {
			continue
		}
		expected[name] = append(expected[name], branchDatabase{Name: name, Branch: worktree.Branch, Worktree: worktree.Path, Container: container, BaseURI: parsed.BaseURI})
	}
	var result []branchDatabase
	for _, matches := range expected {
		if len(matches) != 1 {
			continue
		}
		entry := matches[0]
		worktreeURI, err := database.ReadDatabaseURI(entry.Worktree, envKey)
		if err != nil {
			return nil, fmt.Errorf("reading %s in %s: %w", envKey, entry.Worktree, err)
		}
		if isPostgresURI(worktreeURI) {
			worktreeParsed, err := database.ParseURI(worktreeURI)
			if err != nil {
				return nil, fmt.Errorf("parsing %s in %s: %w", envKey, entry.Worktree, err)
			}
			entry.Active = worktreeParsed.Database == entry.Name
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func isPostgresURI(uri string) bool {
	lower := strings.ToLower(uri)
	return strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
}
