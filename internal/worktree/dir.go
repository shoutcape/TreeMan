package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
)

// DefaultDir is the worktree parent directory used when a project configures
// none. It is relative, so it resolves inside the main worktree.
const DefaultDir = ".worktrees"

// repoPlaceholder expands to the basename of the main worktree root.
const repoPlaceholder = "{repo}"

// ResolveDir turns the configured worktree_dir value into the absolute parent
// directory that new worktrees are created in.
//
// An empty value means DefaultDir. A relative value resolves against mainRoot,
// never against the current directory: running TreeMan from inside a linked
// worktree must place the next worktree in the same place as running it from
// the main one. A leading "~/" expands to the current user's home directory,
// and "{repo}" expands to the basename of mainRoot.
//
// Example:
//
//	mainRoot   = "/home/user/Github/my-project"
//	configured = "~/worktrees/{repo}"
//	result     = "/home/user/worktrees/my-project"
//
// The returned path is cleaned and absolute. It is not required to exist;
// ValidateDestination and EnsureParentDir handle what is on disk.
func ResolveDir(mainRoot, configured string) (string, error) {
	absoluteRoot, err := filepath.Abs(mainRoot)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the main worktree root %q: %w", mainRoot, err)
	}

	value := strings.TrimSpace(configured)
	if value == "" {
		value = DefaultDir
	}

	if err := checkPlaceholders(value); err != nil {
		return "", err
	}

	expanded, err := expandTilde(value)
	if err != nil {
		return "", err
	}

	expanded = strings.ReplaceAll(expanded, repoPlaceholder, filepath.Base(absoluteRoot))
	if strings.TrimSpace(expanded) == "" {
		return "", fmt.Errorf("worktree_dir %q expands to an empty path", configured)
	}

	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(absoluteRoot, expanded)
	}
	resolved := filepath.Clean(expanded)
	canonicalRoot, err := fsutil.CanonicalPath(absoluteRoot)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the main worktree root %q: %w", mainRoot, err)
	}
	canonicalDir, err := fsutil.CanonicalPath(resolved)
	if err != nil {
		return "", fmt.Errorf("cannot resolve worktree_dir %q: %w", configured, err)
	}
	if canonicalDir == canonicalRoot {
		return "", fmt.Errorf("worktree_dir %q resolves to the main worktree root; choose a dedicated directory", configured)
	}
	return resolved, nil
}

// checkPlaceholders rejects every brace expression except "{repo}".
//
// "{branch}" is called out by name because it is the placeholder users reach
// for first, and it cannot be supported: a branch name may contain "/", so it
// is not a single path component, and the slug that is one is already appended
// to the resolved directory.
func checkPlaceholders(value string) error {
	for offset := 0; offset < len(value); {
		open := strings.IndexByte(value[offset:], '{')
		close := strings.IndexByte(value[offset:], '}')
		if close >= 0 && (open < 0 || close < open) {
			return fmt.Errorf("worktree_dir %q has an unmatched %q", value, "}")
		}
		if open < 0 {
			return nil
		}
		open += offset
		closing := strings.IndexByte(value[open:], '}')
		if closing < 0 {
			return fmt.Errorf("worktree_dir %q has an unclosed %q", value, "{")
		}
		name := value[open : open+closing+1]
		switch name {
		case repoPlaceholder:
		case "{branch}":
			return fmt.Errorf("worktree_dir %q cannot use {branch}: a branch name may contain %q, so it is not a directory name; the branch slug is appended to worktree_dir automatically", value, "/")
		default:
			return fmt.Errorf("worktree_dir %q uses unknown placeholder %s; only %s is supported", value, name, repoPlaceholder)
		}
		offset = open + closing + 1
	}
	return nil
}

// expandTilde expands a leading "~" that stands for the current user's home
// directory. Any other tilde form is refused rather than taken literally: a
// value such as "~other/worktrees" asks for another user's home directory,
// and silently creating a directory named "~other" beside the repository is
// not what was asked for.
func expandTilde(value string) (string, error) {
	if !strings.HasPrefix(value, "~") {
		return value, nil
	}
	rest := value[1:]
	if rest != "" && !strings.HasPrefix(rest, "/") {
		return "", fmt.Errorf("worktree_dir %q is not supported: only %q expands to your home directory", value, "~/")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("worktree_dir %q needs a home directory: %w", value, err)
	}
	return filepath.Join(home, rest), nil
}
