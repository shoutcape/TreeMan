package validate_test

import (
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
	valid := []string{"1", "123", "9999"}
	for _, s := range valid {
		t.Run(s, func(t *testing.T) {
			assert.NoError(t, validate.PRNumber(s))
		})
	}
}

func TestPRNumber_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"letters", "abc"},
		{"decimal", "12.3"},
		{"negative", "-1"},
		{"mixed", "12abc"},
		{"leading hash", "#123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, validate.PRNumber(tt.input))
		})
	}
}
