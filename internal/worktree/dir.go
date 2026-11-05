package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shoutcape/treeman/internal/fsutil"
)

// LegacyDir is the worktree parent that earlier releases used by default: a
// directory inside the main worktree. Projects that still have worktrees
// there keep their .gitignore entry, so it stays recognized.
const LegacyDir = ".worktrees"

// siblingSuffix is appended to the project name to form the default worktree
// parent, which sits beside the project directory.
const siblingSuffix = "-worktrees"

// repoPlaceholder expands to the basename of the main worktree root.
const repoPlaceholder = "{repo}"

// ResolveDir turns the configured worktree_dir value into the absolute parent
// directory that new worktrees are created in.
//
// An empty value means SiblingDir(mainRoot): a "<project>-worktrees" directory
// beside the project. A relative value resolves against mainRoot, never
// against the current directory: running TreeMan from inside a linked worktree
// must place the next worktree in the same place as running it from the main
// one. A leading "~/" expands to the current user's home directory, and
// "{repo}" expands to the basename of mainRoot.
//
// Example:
//
//	mainRoot   = "/home/user/Github/my-project"
//	configured = ""
//	result     = "/home/user/Github/my-project-worktrees"
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
		return SiblingDir(absoluteRoot), nil
	}
	return resolveValue(absoluteRoot, value, absoluteRoot)
}

// ResolveFlagDir resolves a worktree directory given on the command line with
// --worktree-dir. Unlike the configured value, a relative path here is taken
// from cwd, as a path typed at the shell is. The value is otherwise handled
// the same way as worktree_dir, including "~/" and "{repo}".
//
// The caller passes only a value the user set; an empty value is an error.
func ResolveFlagDir(cwd, mainRoot, value string) (string, error) {
	absoluteRoot, err := filepath.Abs(mainRoot)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the main worktree root %q: %w", mainRoot, err)
	}
	resolved, err := resolveValue(absoluteRoot, strings.TrimSpace(value), cwd)
	if err != nil {
		return "", fmt.Errorf("--worktree-dir: %w", err)
	}
	return resolved, nil
}

// SiblingDir returns the default worktree parent for the project whose main
// worktree is at root. It is the "<project>-worktrees" directory beside the
// project.
//
// Example:
//
//	root   = "/home/user/Github/my-project"
//	result = "/home/user/Github/my-project-worktrees"
//
// A bare repository has no main worktree, so its Git directory stands in for
// one. Two layouts need a name that is not the directory's own: a bare
// repository named "project.git" uses the name without ".git", and the
// ".bare" directory of the layout "project/.bare" with a ".git" file in
// "project" names the project one level up. Any other directory named
// "*.git" is also read with the suffix dropped.
func SiblingDir(root string) string {
	project := filepath.Clean(root)
	if filepath.Base(project) == ".bare" {
		project = filepath.Dir(project)
	}
	name := strings.TrimSuffix(filepath.Base(project), ".git")
	return filepath.Join(filepath.Dir(project), name+siblingSuffix)
}

// resolveValue expands a worktree directory value and makes it absolute.
// A relative result is joined to base. The result must not be the main
// worktree root, whichever way it was written.
func resolveValue(mainRoot, value, base string) (string, error) {
	if err := checkPlaceholders(value); err != nil {
		return "", err
	}

	expanded, err := expandTilde(value)
	if err != nil {
		return "", err
	}

	expanded = strings.ReplaceAll(expanded, repoPlaceholder, filepath.Base(mainRoot))
	if strings.TrimSpace(expanded) == "" {
		return "", fmt.Errorf("worktree_dir %q expands to an empty path", value)
	}

	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(base, expanded)
	}
	resolved := filepath.Clean(expanded)
	canonicalRoot, err := fsutil.CanonicalPath(mainRoot)
	if err != nil {
		return "", fmt.Errorf("cannot resolve the main worktree root %q: %w", mainRoot, err)
	}
	canonicalDir, err := fsutil.CanonicalPath(resolved)
	if err != nil {
		return "", fmt.Errorf("cannot resolve worktree_dir %q: %w", value, err)
	}
	if canonicalDir == canonicalRoot {
		return "", fmt.Errorf("worktree_dir %q resolves to the main worktree root; choose a dedicated directory", value)
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
