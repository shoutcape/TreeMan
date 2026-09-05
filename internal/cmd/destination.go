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

// destinationFile is the file shell integration handed this process, taken out
// of the environment before TreeMan can start anything that would inherit it.
var destinationFile = takeDestinationFile()

// takeDestinationFile reads the destination file out of the environment and
// removes it.
//
// The variable is a handshake between the shell wrapper and the one TreeMan the
// wrapper started. A process that inherited it would write its own destination
// into the wrapper's file, and the wrapper would honour it: a hook, a dependency
// install, or a command given to --exec could send the caller's shell somewhere
// it never asked to go. Under --exec the risk is worst, because TreeMan reports
// no destination of its own and so never overwrites what a child wrote.
//
// Taking the variable once, at startup, keeps the handshake to one process.
// Scrubbing it at each place TreeMan spawns something would leave the next such
// place to remember.
func takeDestinationFile() string {
	file := os.Getenv(cdFileEnv)
	os.Unsetenv(cdFileEnv)
	return file
}

// reportDestination tells the caller's shell which directory to change to.
func reportDestination(cmd *cobra.Command, dir string) error {
	if destinationFile != "" {
		if err := os.WriteFile(destinationFile, []byte(dir+"\n"), 0o600); err != nil {
			return fmt.Errorf("could not report %q to the shell: %w", dir, err)
		}
		return nil
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), dir)
	return err
}
