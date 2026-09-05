package git

import (
	"os"
	"os/exec"
)

// cleanupCommand builds the detached process that unlinks one cleanup job and
// then removes its lock and diagnostic files.
//
// Every path the child touches is named relative to an inherited descriptor
// rather than by its own name, so replacing a directory along the path after
// this process validated it cannot redirect the deletion. Recursive deletion
// runs relative to the job descriptor; the trash root and job descriptors stay
// stable across path replacement.
//
// The child receives three descriptors: fd 3 carries the lock, whose flock it
// inherits and holds for its lifetime, fd 4 the trash root, and fd 5 the job.
// Positional arguments are the job, diagnostic, and lock paths in that order.
func cleanupCommand(trashRoot, jobName, errorName, lockName string, lock, rootFile, jobFile *os.File) *exec.Cmd {
	// Each step runs only if every earlier one succeeded, and the first
	// non-zero status is what the process exits with, so a failure stops the
	// sequence at the boundary that produced it and leaves the diagnostic and
	// the lock behind for the next removal to find.
	script := `
status=0
cd -P -- /proc/self/fd/5 || status=$?
if [ "$status" -eq 0 ]; then
  rm -rf -- ./* ./.[!.]* ./..?* || status=$?
fi
cd /
if [ "$status" -eq 0 ]; then
  if [ "$1" -ef /proc/self/fd/5 ]; then
    rmdir -- "$1" || status=$?
  else
    printf 'cleanup job path changed during removal\n' >&2
    status=1
  fi
fi
if [ "$status" -eq 0 ]; then
  rm -f -- "$3" || status=$?
fi
if [ "$status" -eq 0 ]; then
  rm -f -- "$2" || status=$?
fi
exit "$status"
`
	jobRef := "/proc/self/fd/4/" + jobName
	errorRef := "/proc/self/fd/4/" + errorName
	lockRef := "/proc/self/fd/4/" + lockName
	cmd := exec.Command("sh", "-c", script, "treeman-cleanup", jobRef, errorRef, lockRef)
	cmd.ExtraFiles = []*os.File{lock, rootFile, jobFile}
	return cmd
}
