package validate_test

import (
	"strconv"
	"testing"

	"github.com/shoutcape/treeman/internal/validate"
	"github.com/stretchr/testify/assert"
)

func TestBranchName_Valid(t *testing.T) {
	valid := []string{
		"feature/test",
		"fix-123",
		"hotfix",
		"feat/nested/deep",
		"release/v1.0.0",
		"my-branch",
	}
	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			assert.NoError(t, validate.BranchName(name))
		})
	}
}

func TestBranchName_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"only whitespace", "   "},
		{"space in name", "my branch"},
		{"tilde", "feat~1"},
		{"caret", "feat^1"},
		{"colon", "feat:1"},
		{"question mark", "feat?"},
		{"asterisk", "feat*"},
		{"open bracket", "feat[1]"},
		{"backslash", `feat\1`},
		{"tab character", "feat\t1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, validate.BranchName(tt.input))
		})
	}
}

func TestPRNumber_Valid(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	valid := []struct {
		input string
		want  int
	}{
		{"1", 1},
		{"123", 123},
		{"9999", 9999},
		{strconv.Itoa(maxInt), maxInt},
	}
	for _, tt := range valid {
		t.Run(tt.input, func(t *testing.T) {
			got, err := validate.PRNumber(tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPRNumber_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"empty string", "", ""},
		{"zero", "0", ""},
		{"letters", "abc", ""},
		{"decimal", "12.3", ""},
		{"negative", "-1", ""},
		{"mixed", "12abc", ""},
		{"leading hash", "#123", ""},
		{"overflow", "999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999", "too large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validate.PRNumber(tt.input)
			assert.Error(t, err)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
