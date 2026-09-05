// Package sh selects the system shell that runs a user command string.
//
// TreeMan runs user command strings -- .treeman.toml hooks and --exec -- in
// this shell so that quoting, arguments, and operators behave the way the user
// types them.
//
// Windows returns cmd, which suits a consumer that spawns a child process.
// Process replacement is POSIX-only, so internal/launch does not build there;
// released builds are linux and darwin.
package sh

import "runtime"

// Command returns the shell binary and the flag that makes it run a command
// string.
func Command() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "sh", "-c"
}
