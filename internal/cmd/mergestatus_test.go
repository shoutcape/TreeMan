package cmd

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubForgeVerifier overrides forge verification for the feature branch.
func stubForgeVerifier(t *testing.T, headSHAs []string, verifyErr error) {
	t.Helper()
	previous := forgeMergedLookup
	forgeMergedLookup = func(string) (forgeMergeVerifier, error) {
		if verifyErr != nil {
			return nil, fmt.Errorf("merge verification failed: %w", verifyErr)
		}
		return func(branch, sha string) (bool, error) {
			if branch != "feature" {
				return false, nil
			}
			for _, headSHA := range headSHAs {
				if sha == headSHA {
					return true, nil
				}
			}
			return false, nil
		}, nil
	}
	t.Cleanup(func() { forgeMergedLookup = previous })
}

func TestJoinWarning(t *testing.T) {
	assert.Equal(t, "first", joinWarning("", "first"))
	assert.Equal(t, "first; second", joinWarning("first", "second"))
}
