package terminal

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectDisablesRichFeaturesForRedirectedStreams(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("CI", "")

	capabilities := Detect(bytes.NewBufferString("input"), &bytes.Buffer{})

	assert.False(t, capabilities.InputTTY)
	assert.False(t, capabilities.OutputTTY)
	assert.False(t, capabilities.Interactive)
	assert.False(t, capabilities.Color)
	assert.False(t, capabilities.RichUI)
	assert.False(t, capabilities.Hyperlinks)
	assert.False(t, capabilities.Motion)
}

func TestDetectMarksDumbTerminals(t *testing.T) {
	t.Setenv("TERM", "dumb")

	capabilities := Detect(bytes.NewBufferString("input"), &bytes.Buffer{})

	assert.True(t, capabilities.Dumb)
	assert.False(t, capabilities.Color)
	assert.False(t, capabilities.Interactive)
}
