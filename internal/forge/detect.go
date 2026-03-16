// Package forge provides forge detection and abstraction.
// This file handles detecting which forge (GitHub / GitLab) hosts a repository
// based on the remote hostname.
package forge

import (
	"fmt"
	"strings"
)

// Type identifies the hosting forge.
type Type string

const (
	GitHub Type = "github"
	GitLab Type = "gitlab"
)

// DetectFromHost returns the forge type for the given hostname.
//
// Rules (mirrors _wt_detect_forge in wt.sh:163):
//   - "github.com"      → GitHub
//   - any host containing "gitlab" → GitLab  (covers gitlab.com, gitlab.company.com, etc.)
//   - anything else     → error
func DetectFromHost(host string) (Type, error) {
	switch {
	case host == "github.com":
		return GitHub, nil
	case strings.Contains(host, "gitlab"):
		return GitLab, nil
	default:
		return "", fmt.Errorf(
			"unsupported forge host %q: expected github.com or a GitLab instance",
			host,
		)
	}
}
