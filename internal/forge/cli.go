package forge

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// runForgeCLI executes a forge CLI request while keeping stdout separate from
// diagnostics. Forge-specific wrappers own argument and payload construction.
func runForgeCLI(tool string, args []string, input []byte, context string) ([]byte, error) {
	cmd := exec.Command(tool, args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, fmt.Errorf("%s: %s", context, message)
		}
		return nil, fmt.Errorf("%s: %w", context, err)
	}
	return stdout.Bytes(), nil
}
