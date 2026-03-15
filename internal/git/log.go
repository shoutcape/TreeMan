package git

import (
	"io"
	"os"
)

// logWriter returns the writer for informational/warning messages.
// All non-path output goes to stderr so stdout remains clean for
// the shell wrapper to capture.
func logWriter() io.Writer {
	return os.Stderr
}
