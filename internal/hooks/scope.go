package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/shoutcape/treeman/internal/fsutil"
)

const (
	approvalScopeVersion = 1
	PostCreatePhase      = "post_create"
)

// ApprovalScope identifies the exact hook commands a user approved.
type ApprovalScope struct {
	Repository string
	ConfigPath string
	Phase      string
	Commands   []string
}

type canonicalScope struct {
	Version    int      `json:"version"`
	Repository string   `json:"repository"`
	ConfigPath string   `json:"config_path"`
	Phase      string   `json:"phase"`
	Commands   []string `json:"commands"`
}

// NewApprovalScope creates an immutable snapshot of the hook request.
func NewApprovalScope(repository, configPath, phase string, commands []string) (ApprovalScope, error) {
	repo, err := fsutil.CanonicalPath(repository)
	if err != nil {
		return ApprovalScope{}, fmt.Errorf("canonicalize repository: %w", err)
	}
	config, err := filepath.Abs(configPath)
	if err != nil {
		return ApprovalScope{}, fmt.Errorf("resolve config path: %w", err)
	}
	scope := ApprovalScope{
		Repository: repo,
		ConfigPath: config,
		Phase:      phase,
		Commands:   append([]string(nil), commands...),
	}
	if err := scope.Validate(); err != nil {
		return ApprovalScope{}, err
	}
	return scope, nil
}

// Validate checks the fields accepted by the approval format.
func (s ApprovalScope) Validate() error {
	if s.Repository == "" || !filepath.IsAbs(s.Repository) || filepath.Clean(s.Repository) != s.Repository {
		return errors.New("approval scope repository must be an absolute cleaned path")
	}
	if s.ConfigPath == "" || !filepath.IsAbs(s.ConfigPath) || filepath.Clean(s.ConfigPath) != s.ConfigPath {
		return errors.New("approval scope config path must be an absolute cleaned path")
	}
	if s.Phase != PostCreatePhase {
		return fmt.Errorf("unsupported approval phase %q", s.Phase)
	}
	return nil
}

// ID returns the SHA-256 fingerprint of the versioned canonical scope JSON.
func (s ApprovalScope) ID() string {
	payload, err := json.Marshal(canonicalScope{
		Version: approvalScopeVersion, Repository: s.Repository, ConfigPath: s.ConfigPath,
		Phase: s.Phase, Commands: s.Commands,
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
