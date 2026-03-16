// Package remote provides helpers for parsing git remote URLs.
// It supports the four URL formats TreeMan cares about:
//
//   - git@host:path        (SSH shorthand)
//   - ssh://git@host/path  (SSH URL, with optional :port)
//   - https://host/path
//   - http://host/path
package remote

import (
	"fmt"
	"strings"
)

// ParseHost extracts the hostname from a git remote URL.
//
// Examples:
//
//	"git@github.com:owner/repo.git"                  → "github.com"
//	"https://github.com/owner/repo.git"               → "github.com"
//	"ssh://git@gitlab.company.com:2222/group/proj.git" → "gitlab.company.com"
func ParseHost(url string) (string, error) {
	switch {
	case strings.HasPrefix(url, "git@") && strings.Contains(url, ":"):
		// git@host:path  →  everything between @ and :
		after := url[len("git@"):]
		return strings.SplitN(after, ":", 2)[0], nil

	case strings.HasPrefix(url, "ssh://git@"):
		// ssh://git@host/path  or  ssh://git@host:port/path
		after := url[len("ssh://git@"):]
		hostPort := strings.SplitN(after, "/", 2)[0]
		// strip optional :port
		return strings.SplitN(hostPort, ":", 2)[0], nil

	case strings.HasPrefix(url, "https://"):
		after := url[len("https://"):]
		return strings.SplitN(after, "/", 2)[0], nil

	case strings.HasPrefix(url, "http://"):
		after := url[len("http://"):]
		return strings.SplitN(after, "/", 2)[0], nil

	default:
		return "", fmt.Errorf("cannot extract host from remote URL %q", url)
	}
}

// ParsePath extracts the repository path (owner/repo or group/subgroup/project)
// from a git remote URL. The .git suffix and leading/trailing slashes are stripped.
//
// Examples:
//
//	"git@github.com:owner/repo.git"                          → "owner/repo"
//	"git@gitlab.company.com:acme/frontend/webapp.git"        → "acme/frontend/webapp"
//	"ssh://git@gitlab.company.com:2222/group/project.git"    → "group/project"
func ParsePath(url string) (string, error) {
	var path string

	switch {
	case strings.HasPrefix(url, "git@") && strings.Contains(url, ":"):
		// git@host:path  →  everything after the first :
		parts := strings.SplitN(url, ":", 2)
		path = parts[1]

	case strings.HasPrefix(url, "ssh://git@"):
		// ssh://git@host/path  or  ssh://git@host:port/path
		after := url[len("ssh://git@"):]
		// host_port is everything up to the first /
		slash := strings.Index(after, "/")
		if slash < 0 {
			return "", fmt.Errorf("cannot extract path from remote URL %q", url)
		}
		path = after[slash+1:]

	case strings.HasPrefix(url, "https://"):
		after := url[len("https://"):]
		slash := strings.Index(after, "/")
		if slash < 0 {
			return "", fmt.Errorf("cannot extract path from remote URL %q", url)
		}
		path = after[slash+1:]

	case strings.HasPrefix(url, "http://"):
		after := url[len("http://"):]
		slash := strings.Index(after, "/")
		if slash < 0 {
			return "", fmt.Errorf("cannot extract path from remote URL %q", url)
		}
		path = after[slash+1:]

	default:
		return "", fmt.Errorf("cannot extract path from remote URL %q", url)
	}

	// Strip .git suffix, trailing slash, leading slash.
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")

	return path, nil
}

// URLEncode percent-encodes a string for use in URL path segments.
// Unreserved characters [A-Za-z0-9._~-] pass through unchanged;
// everything else is encoded as %XX.
//
// This matches the GitLab API requirement for project paths in URLs,
// e.g. "group/subgroup/project" → "group%2Fsubgroup%2Fproject".
func URLEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if isUnreserved(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// isUnreserved reports whether a byte is an RFC 3986 unreserved character.
func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '.' || c == '_' || c == '~' || c == '-'
}
