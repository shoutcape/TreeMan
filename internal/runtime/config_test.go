package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".treeman.yml")

	content := `runtime:
  type: process
  command: pnpm dev
  env_file: .env.treeman
  ports:
    app: 3000
    api: 4000
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Runtime.Type != "process" {
		t.Errorf("Type = %q, want %q", cfg.Runtime.Type, "process")
	}
	if cfg.Runtime.Command != "pnpm dev" {
		t.Errorf("Command = %q, want %q", cfg.Runtime.Command, "pnpm dev")
	}
	if cfg.Runtime.EnvFile != ".env.treeman" {
		t.Errorf("EnvFile = %q, want %q", cfg.Runtime.EnvFile, ".env.treeman")
	}
	if cfg.Runtime.Ports["app"] != 3000 {
		t.Errorf("Ports[app] = %d, want %d", cfg.Runtime.Ports["app"], 3000)
	}
	if cfg.Runtime.Ports["api"] != 4000 {
		t.Errorf("Ports[api] = %d, want %d", cfg.Runtime.Ports["api"], 4000)
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".treeman.yml")

	content := `runtime:
  type: process
  command: npm start
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	// EnvFile should default to .env.treeman
	if cfg.Runtime.EnvFile != ".env.treeman" {
		t.Errorf("EnvFile = %q, want %q", cfg.Runtime.EnvFile, ".env.treeman")
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name:    "valid process",
			cfg:     Config{Runtime: RuntimeConfig{Type: "process", Command: "pnpm dev"}},
			wantErr: false,
		},
		{
			name:    "process without command",
			cfg:     Config{Runtime: RuntimeConfig{Type: "process"}},
			wantErr: true,
		},
		{
			name:    "valid docker-compose",
			cfg:     Config{Runtime: RuntimeConfig{Type: "docker-compose", ComposeFile: "docker-compose.yml"}},
			wantErr: false,
		},
		{
			name:    "docker-compose without compose_file",
			cfg:     Config{Runtime: RuntimeConfig{Type: "docker-compose"}},
			wantErr: true,
		},
		{
			name:    "missing type",
			cfg:     Config{Runtime: RuntimeConfig{}},
			wantErr: true,
		},
		{
			name:    "unknown type",
			cfg:     Config{Runtime: RuntimeConfig{Type: "kubernetes"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestLoadConfigDockerCompose(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".treeman.yml")

	content := `runtime:
  type: docker-compose
  compose_file: docker-compose.dev.yml
  ports:
    app: 3000
    api: 4000
    postgres: 5432
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Runtime.Type != "docker-compose" {
		t.Errorf("Type = %q, want %q", cfg.Runtime.Type, "docker-compose")
	}
	if cfg.Runtime.ComposeFile != "docker-compose.dev.yml" {
		t.Errorf("ComposeFile = %q, want %q", cfg.Runtime.ComposeFile, "docker-compose.dev.yml")
	}
	if len(cfg.Runtime.Ports) != 3 {
		t.Errorf("len(Ports) = %d, want 3", len(cfg.Runtime.Ports))
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}
