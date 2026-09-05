package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// cdFileEnv names the file that shell integration hands TreeMan to receive the
// directory the caller's shell should change to.
//
// The file exists so that shell integration never has to run TreeMan inside
// command substitution. A captured TreeMan cannot hand its terminal to --exec,
// and the wrapper would have to parse TreeMan's own flags to know when to skip
// the capture. With a destination file the wrapper reads one path from a known
// place and stays out of TreeMan's argument list.
//
// When the variable is unset -- a bare run, a script, a pipe -- the destination
// goes to stdout, which is the original contract.
const cdFileEnv = "TREEMAN_CD_FILE"

// reportDestination tells the caller's shell which directory to change to.
func reportDestination(cmd *cobra.Command, dir string) error {
	if file := os.Getenv(cdFileEnv); file != "" {
		if err := os.WriteFile(file, []byte(dir+"\n"), 0o600); err != nil {
			return fmt.Errorf("could not report %q to the shell: %w", dir, err)
		}
		return nil
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), dir)
	return err
}
