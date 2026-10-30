package state

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimWarmStartPromptUsesOneMarkerForCanonicalAliases(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	repo := t.TempDir()
	repo, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)
	alias := filepath.Join(t.TempDir(), "repo-alias")
	require.NoError(t, os.Symlink(repo, alias))

	claimed, err := ClaimWarmStartPrompt(repo)
	require.NoError(t, err)
	assert.True(t, claimed)

	claimed, err = ClaimWarmStartPrompt(alias)
	require.NoError(t, err)
	assert.False(t, claimed)

	marker := filepath.Join(stateHome, "treeman", "warm-start-prompts", fmt.Sprintf("%x", sha256.Sum256([]byte(repo))))
	info, err := os.Stat(marker)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	contents, err := os.ReadFile(marker)
	require.NoError(t, err)
	assert.Empty(t, contents)
}

func TestClaimWarmStartPromptSeparatesRepositories(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	first, err := ClaimWarmStartPrompt(t.TempDir())
	require.NoError(t, err)
	second, err := ClaimWarmStartPrompt(t.TempDir())
	require.NoError(t, err)

	assert.True(t, first)
	assert.True(t, second)
}

func TestClaimWarmStartPromptConcurrentClaimsHaveOneWinner(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	repo := t.TempDir()
	const callers = 32

	start := make(chan struct{})
	results := make(chan bool, callers)
	errors := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimed, err := ClaimWarmStartPrompt(repo)
			results <- claimed
			errors <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errors)

	winners := 0
	for claimed := range results {
		if claimed {
			winners++
		}
	}
	for err := range errors {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, winners)
}

func TestClaimWarmStartPromptReturnsStateErrorsWithoutPanicking(t *testing.T) {
	repo := t.TempDir()
	stateHome := filepath.Join(t.TempDir(), "state-file")
	require.NoError(t, os.WriteFile(stateHome, []byte("not a directory"), 0o600))
	t.Setenv("XDG_STATE_HOME", stateHome)

	var claimed bool
	var err error
	assert.NotPanics(t, func() {
		claimed, err = ClaimWarmStartPrompt(repo)
	})
	assert.False(t, claimed)
	assert.Error(t, err)
}

func TestClaimWarmStartPromptRejectsRelativeXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative-state")

	claimed, err := ClaimWarmStartPrompt(t.TempDir())

	assert.False(t, claimed)
	assert.Error(t, err)
}

func TestClaimWarmStartPromptUsesNormalStatePermissions(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	claimed, err := ClaimWarmStartPrompt(t.TempDir())
	require.NoError(t, err)
	require.True(t, claimed)

	assert.Equal(t, os.FileMode(0o700), fileMode(t, filepath.Join(stateHome, "treeman")))
	assert.Equal(t, os.FileMode(0o700), fileMode(t, filepath.Join(stateHome, "treeman", "warm-start-prompts")))
	entries, err := os.ReadDir(filepath.Join(stateHome, "treeman", "warm-start-prompts"))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	info, err := entries[0].Info()
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
