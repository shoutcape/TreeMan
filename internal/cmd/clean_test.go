package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewCleanCmd(t *testing.T) {
	cmd := newCleanCmd()

	assert.Equal(t, "clean", cmd.Name())
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("yes"))
}
