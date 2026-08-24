package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/shoutcape/treeman/internal/state"
	"github.com/shoutcape/treeman/internal/ui"
	"github.com/spf13/cobra"
)

// New returns the root cobra command for treeman.
func New(version, commit, date string) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:   "treeman",
		Short: "Git worktree management CLI",
		Long: `TreeMan is a Git worktree management CLI.

It provides fast commands to create, switch, review, and delete
Git worktrees -- keeping your branches isolated without juggling stashes.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			applyTheme()
			if showVersion {
				printVersion(cmd, version, commit, date)
				return
			}
			printOverview(cmd)
		},
	}
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "Print version information")
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		applyTheme()
	}

	root.AddCommand(newVersionCmd(version, commit, date))
	root.AddCommand(newCreateCmd())
	root.AddCommand(newBranchCmd())
	root.AddCommand(newReviewCmd())
	root.AddCommand(newSwitchCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newBenchmarkCmd())
	root.AddCommand(newCleanCmd())
	root.AddCommand(newDeleteCmd())
	root.AddCommand(newShellCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newThemeCmd())
	root.SetHelpFunc(printHelp)

	return root
}

func applyTheme() {
	theme := state.Theme()
	if envTheme := os.Getenv("TREEMAN_THEME"); envTheme != "" {
		theme = envTheme
	}
	ui.SetTheme(theme)
}

func printOverview(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	render := outputRenderer(cmd)
	commands := []commandSummary{
		{use: "treeman create <branch>", short: "Create a runnable worktree"},
		{use: "treeman branch [query]", short: "Check out a remote branch"},
		{use: "treeman review [number]", short: "Check out a PR or MR"},
		{use: "treeman switch [query]", short: "Select a worktree"},
		{use: "treeman list [--json]", short: "List worktrees and state"},
		{use: "treeman benchmark [command]", short: "Measure command execution time"},
		{use: "treeman clean", short: "Remove clean worktrees merged into default branch"},
		{use: "treeman delete [query]", short: "Remove a worktree and branch"},
		{use: "treeman doctor", short: "Check repository readiness and configuration"},
		{use: "treeman theme", short: "Select a terminal color theme"},
	}

	fmt.Fprintf(out, "\n%s\n%s\n\n", render.Title("TREEMAN"), render.Muted("TreeMan manages isolated Git worktrees."))
	writeCommandSection(out, render, "COMMANDS", commands)
	fmt.Fprintf(out, "%s\n\n  %s\n", render.Header("MORE"), render.Muted(`Run "treeman --help" for full command and flag reference.`))
}

type commandSummary struct {
	use   string
	short string
}

func printHelp(cmd *cobra.Command, _ []string) {
	applyTheme()
	out := cmd.OutOrStdout()
	render := outputRenderer(cmd)

	fmt.Fprintf(out, "\n%s\n", render.Title(strings.ToUpper(cmd.CommandPath())))
	description := cmd.Long
	if description == "" {
		description = cmd.Short
	}
	if description != "" {
		for _, line := range strings.Split(description, "\n") {
			fmt.Fprintln(out, render.Muted(line))
		}
	}

	commands := make([]commandSummary, 0, len(cmd.Commands()))
	for _, child := range cmd.Commands() {
		if child.IsAvailableCommand() || child.Name() == "help" {
			commands = append(commands, commandSummary{use: child.Name(), short: child.Short})
		}
	}
	fmt.Fprintf(out, "\n%s\n\n  %s\n", render.Header("USAGE"), render.Branch(cmd.UseLine()))
	if len(commands) > 0 {
		fmt.Fprintf(out, "  %s [command]\n", render.Branch(cmd.CommandPath()))
	}
	fmt.Fprintln(out)
	writeCommandSection(out, render, "COMMANDS", commands)

	if flags := strings.TrimRight(cmd.Flags().FlagUsagesWrapped(0), "\n"); strings.TrimSpace(flags) != "" {
		fmt.Fprintf(out, "%s\n\n", render.Header("FLAGS"))
		for _, line := range strings.Split(flags, "\n") {
			fmt.Fprintf(out, "  %s\n", render.Muted(strings.TrimLeft(line, " ")))
		}
		fmt.Fprintln(out)
	}

	if len(commands) > 0 {
		fmt.Fprintf(out, "%s\n", render.Muted(fmt.Sprintf(`Run "%s [command] --help" for more information about a command.`, cmd.CommandPath())))
	}
}

func writeCommandSection(out interface{ Write([]byte) (int, error) }, render ui.Renderer, title string, commands []commandSummary) {
	if len(commands) == 0 {
		return
	}

	width := 0
	for _, command := range commands {
		width = max(width, len(command.use))
	}
	fmt.Fprintf(out, "%s\n\n", render.Header(title))
	for _, command := range commands {
		fmt.Fprintf(out, "  %s  %s\n", render.Branch(fmt.Sprintf("%-*s", width, command.use)), render.Muted(command.short))
	}
	fmt.Fprintln(out)
}
