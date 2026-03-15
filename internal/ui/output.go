// Package ui provides terminal output helpers for TreeMan.
package ui

import (
	"fmt"
	"os"
)

// Info prints an informational message to stderr.
func Info(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// Warn prints a warning message to stderr.
func Warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Warning: "+format+"\n", args...)
}

// Error prints an error message to stderr.
func Error(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
}
