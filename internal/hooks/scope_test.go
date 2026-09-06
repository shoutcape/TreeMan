package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalScopeSnapshotAndID(t *testing.T) {
	root := t.TempDir()
	commands := []string{"echo  one", "echo two"}
	scope, err := NewApprovalScope(root, filepath.Join(root, ".treeman.toml"), PostCreatePhase, commands)
	require.NoError(t, err)
	commands[0] = "changed"
	assert.Equal(t, "echo  one", scope.Commands[0])

	for name, mutate := range map[string]func(*ApprovalScope){
		"repository": func(s *ApprovalScope) { s.Repository += "-other" },
		"config":     func(s *ApprovalScope) { s.ConfigPath += "-other" },
		"phase":      func(s *ApprovalScope) { s.Phase = "other" },
		"bytes":      func(s *ApprovalScope) { s.Commands[0] = "echo one" },
		"order":      func(s *ApprovalScope) { s.Commands[0], s.Commands[1] = s.Commands[1], s.Commands[0] },
		"addition":   func(s *ApprovalScope) { s.Commands = append(s.Commands, "third") },
		"removal":    func(s *ApprovalScope) { s.Commands = s.Commands[:1] },
		"duplicate":  func(s *ApprovalScope) { s.Commands = append(s.Commands, s.Commands[0]) },
	} {
		copy := scope
		copy.Commands = append([]string(nil), scope.Commands...)
		mutate(&copy)
		assert.NotEqual(t, scope.ID(), copy.ID(), name)
	}
}

func TestApprovalScopeKeepsDiscoveredConfigPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "config")
	alias := filepath.Join(root, "alias")
	require.NoError(t, os.WriteFile(target, nil, 0o600))
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	original, err := NewApprovalScope(root, target, PostCreatePhase, []string{"true"})
	require.NoError(t, err)
	linked, err := NewApprovalScope(root, alias, PostCreatePhase, []string{"true"})
	require.NoError(t, err)
	assert.Equal(t, alias, linked.ConfigPath)
	assert.NotEqual(t, original.ID(), linked.ID())
}

func TestApprovalScopeRejectsUnsupportedFields(t *testing.T) {
	root := t.TempDir()
	_, err := NewApprovalScope(root, filepath.Join(root, "config"), "before", nil)
	require.Error(t, err)

	scope := ApprovalScope{Repository: root, ConfigPath: filepath.Join(root, "config"), Phase: PostCreatePhase}
	require.NoError(t, scope.Validate())
	scope.Repository = "relative"
	require.Error(t, scope.Validate())
}
