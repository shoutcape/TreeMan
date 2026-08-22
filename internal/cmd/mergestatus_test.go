package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// stubForgeVerifier overrides forge verification with fixed merged head SHAs
// for all remote-gone branches.
func stubForgeVerifier(t *testing.T, headSHAs []string, verifyErr error) {
	t.Helper()
	previous := forgeMergedLookup
	forgeMergedLookup = func() (func(string) ([]string, error), string) {
		return func(string) ([]string, error) { return headSHAs, verifyErr }, ""
	}
	t.Cleanup(func() { forgeMergedLookup = previous })
}

func TestJoinWarning(t *testing.T) {
	assert.Equal(t, "first", joinWarning("", "first"))
	assert.Equal(t, "first; second", joinWarning("first", "second"))
}
