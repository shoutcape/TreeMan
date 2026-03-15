package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a .treeman.yml config file",
	Long: `Generate a starter .treeman.yml configuration file in the current directory.

This file declares how to run your project and which ports to allocate.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		const filename = ".treeman.yml"

		if _, err := os.Stat(filename); err == nil {
			return fmt.Errorf("%s already exists", filename)
		}

		template := `# TreeMan runtime configuration
# See: https://github.com/shoutcape/TreeMan
runtime:
  type: process
  command: pnpm dev
  env_file: .env.treeman
  ports:
    app: 3000
`

		if err := os.WriteFile(filename, []byte(template), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}

		fmt.Fprintf(os.Stderr, "Created %s\n", filename)
		fmt.Fprintf(os.Stderr, "Edit the file to match your project, then run: treeman runtime up\n")
		return nil
	},
}
