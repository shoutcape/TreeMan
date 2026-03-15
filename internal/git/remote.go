package git

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// DefaultBranch detects the default branch on origin (main or master).
// Fast path: use local origin/HEAD metadata. Falls back to querying origin.
func DefaultBranch() (string, error) {
	// Try local origin/HEAD first
	originHead, err := runSilent("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		originHead = strings.TrimPrefix(originHead, "origin/")
		if originHead == "main" {
			return "main", nil
		}
		if originHead == "master" {
			fmt.Fprintln(logWriter(), "Warning: no 'main' branch found on origin, using 'master'.")
			return "master", nil
		}
	}

	// Fall back to querying origin
	refs, err := runSilent("ls-remote", "--heads", "origin", "main", "master")
	if err != nil {
		return "", fmt.Errorf("could not query origin for default branch: %w", err)
	}

	if strings.Contains(refs, "refs/heads/main") {
		return "main", nil
	}
	if strings.Contains(refs, "refs/heads/master") {
		fmt.Fprintln(logWriter(), "Warning: no 'main' branch found on origin, using 'master'.")
		return "master", nil
	}

	return "", fmt.Errorf("could not find 'main' or 'master' on origin")
}

// RemoteURL returns the URL of the named remote.
func RemoteURL(remote string) (string, error) {
	return runSilent("remote", "get-url", remote)
}

// ParseRemoteHost extracts the hostname from a git remote URL.
// Supports: SSH shorthand, HTTPS, SSH with ssh:// prefix.
//
//	git@github.com:owner/repo.git         → github.com
//	https://github.com/owner/repo.git     → github.com
//	ssh://git@gitlab.com:2222/group/proj   → gitlab.com
func ParseRemoteHost(rawURL string) (string, error) {
	// SSH shorthand: git@host:path
	if strings.Contains(rawURL, "@") && !strings.Contains(rawURL, "://") {
		parts := strings.SplitN(rawURL, "@", 2)
		if len(parts) == 2 {
			hostPath := parts[1]
			host := strings.SplitN(hostPath, ":", 2)[0]
			return host, nil
		}
	}

	// Standard URL parsing (https://, ssh://)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("cannot parse remote URL %q: %w", rawURL, err)
	}

	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("cannot extract host from remote URL %q", rawURL)
	}
	return host, nil
}

// ParseRemotePath extracts the owner/repo path from a git remote URL.
// Strips leading slashes and trailing .git suffix.
//
//	git@github.com:owner/repo.git                         → owner/repo
//	https://github.com/owner/repo.git                     → owner/repo
//	git@gitlab.company.com:acme/frontend/webapp.git        → acme/frontend/webapp
//	ssh://git@gitlab.company.com:2222/group/project.git    → group/project
func ParseRemotePath(rawURL string) (string, error) {
	var path string

	// SSH shorthand: git@host:path
	if strings.Contains(rawURL, "@") && !strings.Contains(rawURL, "://") {
		parts := strings.SplitN(rawURL, ":", 2)
		if len(parts) == 2 {
			path = parts[1]
		}
	} else {
		// Standard URL parsing
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("cannot parse remote URL %q: %w", rawURL, err)
		}
		path = parsed.Path
	}

	// Clean up
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	if path == "" {
		return "", fmt.Errorf("cannot extract path from remote URL %q", rawURL)
	}
	return path, nil
}

// ForgeType represents the type of git forge (GitHub, GitLab, etc.).
type ForgeType string

const (
	ForgeGitHub  ForgeType = "github"
	ForgeGitLab  ForgeType = "gitlab"
	ForgeUnknown ForgeType = ""
)

// DetectForge detects whether the origin remote points to GitHub or GitLab.
// Supports self-hosted GitLab instances (any host containing "gitlab").
func DetectForge() (ForgeType, error) {
	remoteURL, err := RemoteURL("origin")
	if err != nil {
		return ForgeUnknown, fmt.Errorf("cannot read origin remote: %w", err)
	}

	return DetectForgeFromURL(remoteURL)
}

// DetectForgeFromURL detects the forge type from a remote URL string.
func DetectForgeFromURL(remoteURL string) (ForgeType, error) {
	host, err := ParseRemoteHost(remoteURL)
	if err != nil {
		return ForgeUnknown, err
	}

	host = strings.ToLower(host)

	if host == "github.com" {
		return ForgeGitHub, nil
	}
	if strings.Contains(host, "gitlab") {
		return ForgeGitLab, nil
	}

	return ForgeUnknown, fmt.Errorf("unsupported forge host: %s", host)
}

// OriginRepoSlug returns the owner/repo slug for the origin remote.
func OriginRepoSlug() (string, error) {
	remoteURL, err := RemoteURL("origin")
	if err != nil {
		return "", err
	}
	return ParseRemotePath(remoteURL)
}

// OriginHost returns the hostname of the origin remote.
func OriginHost() (string, error) {
	remoteURL, err := RemoteURL("origin")
	if err != nil {
		return "", err
	}
	return ParseRemoteHost(remoteURL)
}

// newGitCmd creates an exec.Cmd for a git command.
func newGitCmd(args ...string) *exec.Cmd {
	return exec.Command("git", args...)
}

// newCmd creates an exec.Cmd.
func newCmd(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}

// findExecutable looks up a binary in PATH.
func findExecutable(name string) (string, error) {
	return exec.LookPath(name)
}
