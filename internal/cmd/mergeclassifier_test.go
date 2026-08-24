package cmd

import (
	"testing"

	"github.com/shoutcape/treeman/internal/git"
	"github.com/shoutcape/treeman/internal/merge"
)

// stubForgeVerifier returns an isolated classifier for command integration tests.
func stubForgeVerifier(t *testing.T, headSHAs []string, verifyErr error) merge.ClassifierFunc {
	t.Helper()
	return merge.ClassifierFunc(func(_ string, branches []string) (merge.Result, error) {
		cleanable := make([]merge.Candidate, 0, len(branches))
		for _, branch := range branches {
			sha, err := git.BranchSHA(branch)
			if err != nil {
				return merge.Result{}, err
			}
			for _, headSHA := range headSHAs {
				if sha == headSHA {
					cleanable = append(cleanable, merge.Candidate{Branch: branch, SHA: sha})
					break
				}
			}
		}
		result := merge.Result{Cleanable: cleanable}
		if verifyErr != nil {
			result.Diagnostics = []merge.Diagnostic{{Operation: "merge verification failed", Err: verifyErr}}
		}
		return result, nil
	})
}
