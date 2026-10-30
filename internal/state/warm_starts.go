package state

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shoutcape/treeman/internal/fsutil"
)

// ClaimWarmStartPrompt records an offer before it is displayed. Exclusive
// creation elects one caller across concurrent commands and linked worktrees.
// Declining, EOF, or interruption does not cause another offer next time.
func ClaimWarmStartPrompt(commonDir string) (bool, error) {
	canonical, err := fsutil.CanonicalPath(commonDir)
	if err != nil {
		return false, err
	}
	base, err := stateDir()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(base, "warm-start-prompts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%x", sha256.Sum256([]byte(canonical))))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, file.Close()
}
