package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print TreeMan version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("treeman %s\n", Version)
	},
}
