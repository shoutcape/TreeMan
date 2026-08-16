package ui

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestColorEnabled(t *testing.T) {
	output := &bytes.Buffer{}

	tests := []struct {
		name    string
		mode    string
		noColor bool
		want    bool
		wantErr bool
	}{
		{name: "automatic non-terminal", mode: "auto", want: false},
		{name: "always", mode: "always", want: true},
		{name: "never", mode: "never", want: false},
		{name: "NO_COLOR overrides always", mode: "always", noColor: true, want: false},
		{name: "invalid mode", mode: "sometimes", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ColorEnabled(test.mode, output, test.noColor)
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestConfigureColorNeverEmitsANSI(t *testing.T) {
	t.Cleanup(func() { require.NoError(t, ConfigureColor("always", &bytes.Buffer{}, false)) })
	require.NoError(t, ConfigureColor("never", &bytes.Buffer{}, false))

	assert.Empty(t, ColorPath)
	assert.Empty(t, ColorReset)
}
