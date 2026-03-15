// Package git wraps git CLI operations used by TreeMan.
//
// All functions in this package execute git as a subprocess. No git
// library dependency is used — this keeps the approach identical to
// the original shell implementation and avoids CGo.
package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// run executes a git command and returns its combined stdout.
// Stderr is forwarded to os.Stderr so the user sees git errors.
func run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runSilent executes a git command and captures both stdout and stderr.
// Neither is forwarded to the user. Used for probing commands where
// we handle errors ourselves.
func runSilent(args ...string) (string, error) {
	cmd := exec.Command("git", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// runPassthrough executes a git command with stdin/stdout/stderr
// connected to the terminal. Used for commands where the user needs
// to see output directly (e.g. git fetch progress).
func runPassthrough(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr // Progress goes to stderr (stdout is reserved for paths)
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// IsInsideRepo returns true if the current directory is inside a git repository.
func IsInsideRepo() bool {
	_, err := runSilent("rev-parse", "--git-dir")
	return err == nil
}

// Fetch fetches a ref from a remote.
func Fetch(remote, ref string) error {
	return runPassthrough("fetch", remote, ref)
}

// BranchDelete force-deletes a local branch.
func BranchDelete(branch string) error {
	_, err := run("branch", "-D", branch)
	return err
}

// LocalBranchExists returns true if the given branch exists locally.
func LocalBranchExists(branch string) bool {
	_, err := runSilent("show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// ValidateBranchName checks if a branch name contains invalid characters.
func ValidateBranchName(branch string) error {
	invalid := " \t\n~^:?*[\\"
	for _, c := range invalid {
		if strings.ContainsRune(branch, c) {
			return fmt.Errorf("branch name contains invalid characters")
		}
	}
	return nil
}
