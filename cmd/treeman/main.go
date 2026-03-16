package main

import (
	"fmt"
	"os"

	"github.com/shoutcape/treeman/internal/cmd"
)

// Populated via -ldflags at build time.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	root := cmd.New(version, commit, date)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
