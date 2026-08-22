// Package terminal defines terminal interaction policy for TreeMan streams.
package terminal

import (
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

// Capabilities describes the features available for one input/output pair.
// It is policy only: rendering remains the responsibility of the UI package.
type Capabilities struct {
	InputTTY    bool
	OutputTTY   bool
	Interactive bool
	Color       bool
	RichUI      bool
	Hyperlinks  bool
	Motion      bool
	Dumb        bool
	Width       int
}

// Detect determines capabilities for the actual streams a command uses.
func Detect(input io.Reader, output io.Writer) Capabilities {
	inputFD, inputHasFD := fd(input)
	outputFD, outputHasFD := fd(output)
	inputTTY := inputHasFD && term.IsTerminal(inputFD)
	outputTTY := outputHasFD && term.IsTerminal(outputFD)
	dumb := strings.EqualFold(os.Getenv("TERM"), "dumb")
	noColor := os.Getenv("NO_COLOR") != ""
	ci := os.Getenv("CI") != ""

	width := 0
	if outputTTY {
		width, _, _ = term.GetSize(outputFD)
	}
	color := outputTTY && !noColor && !dumb
	return Capabilities{
		InputTTY:    inputTTY,
		OutputTTY:   outputTTY,
		Interactive: inputTTY && outputTTY && !dumb && !ci,
		Color:       color,
		RichUI:      color,
		Hyperlinks:  color,
		Motion:      false,
		Dumb:        dumb,
		Width:       width,
	}
}

func fd(value any) (uintptr, bool) {
	file, ok := value.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	return file.Fd(), true
}
