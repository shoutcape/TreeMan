//go:build !linux

package git

import (
	"os"
	"os/exec"
	"path/filepath"
)

// cleanupCommand builds the detached process that unlinks one cleanup job and
// then removes its lock and diagnostic files.
//
// The descriptor-relative addressing the Linux implementation uses depends on
// /proc/self/fd, which exists only there, so the child is given ordinary paths
// and the same argument order. What that gives up is the guarantee that a
// directory replaced along the path after validation cannot redirect the
// deletion; detachRemoveAll's containment check and its SameFile comparison of
// the trash root still run, and every caller holds the repository mutation
// lock, so the exposure is to a foreign process rather than to TreeMan.
//
// fd 3 still carries the lock, whose flock the child inherits and holds for
// its lifetime. Positional arguments are the job, diagnostic, and lock paths.
func cleanupCommand(trashRoot, jobName, errorName, lockName string, lock, rootFile, jobFile *os.File) *exec.Cmd {
	// As on Linux, each step runs only if every earlier one succeeded and the
	// first non-zero status is the exit status, so a failure leaves the
	// diagnostic and the lock for the next removal to find.
	script := `
status=0
rm -rf -- "$1" || status=$?
if [ "$status" -eq 0 ]; then
  rm -f -- "$3" || status=$?
fi
if [ "$status" -eq 0 ]; then
  rm -f -- "$2" || status=$?
fi
exit "$status"
`
	cmd := exec.Command("sh", "-c", script, "treeman-cleanup",
		filepath.Join(trashRoot, jobName),
		filepath.Join(trashRoot, errorName),
		filepath.Join(trashRoot, lockName),
	)
	cmd.ExtraFiles = []*os.File{lock}
	return cmd
}
