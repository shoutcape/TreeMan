package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd(version, commit, date string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Print the treeman version, git commit, and build date.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "treeman %s\n", version)
			if commit != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "commit  %s\n", commit)
			}
			if date != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "built   %s\n", date)
			}
		},
	}
}
