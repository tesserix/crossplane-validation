package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("nonexistent.yml")
	if err != nil {
		t.Fatalf("loading defaults: %v", err)
	}

	if len(cfg.ManifestDirs) != 1 || cfg.ManifestDirs[0] != "." {
		t.Errorf("expected default manifest dir '.', got %v", cfg.ManifestDirs)
	}

	if cfg.Settings.Timeout != "5m" {
		t.Errorf("expected default timeout '5m', got %q", cfg.Settings.Timeout)
	}

	if cfg.Settings.DiffFormat != "terraform" {
		t.Errorf("expected default diff format 'terraform', got %q", cfg.Settings.DiffFormat)
	}

	if len(cfg.Settings.IgnoreFields) == 0 {
		t.Error("expected default ignore fields")
	}
}

func TestLoadFromFile(t *testing.T) {
	content := `
manifests:
  - crossplane/
  - infra/

providers:
  aws:
    credentials: env
    region: us-east-1
  gcp:
    credentials: env
    project: my-project
  azure:
    credentials: env

settings:
  timeout: 10m
  diff-format: yaml
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".crossplane-validate.yml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	if len(cfg.ManifestDirs) != 2 {
		t.Errorf("expected 2 manifest dirs, got %d", len(cfg.ManifestDirs))
	}

	if len(cfg.Providers) != 3 {
		t.Errorf("expected 3 providers, got %d", len(cfg.Providers))
	}

	aws := cfg.Providers["aws"]
	if aws.Region != "us-east-1" {
		t.Errorf("expected AWS region us-east-1, got %q", aws.Region)
	}

	gcp := cfg.Providers["gcp"]
	if gcp.Project != "my-project" {
		t.Errorf("expected GCP project my-project, got %q", gcp.Project)
	}

	if cfg.Settings.Timeout != "10m" {
		t.Errorf("expected timeout 10m, got %q", cfg.Settings.Timeout)
	}
}

func TestHasCloudCredentials(t *testing.T) {
	cfg := &Config{
		Providers: map[string]Provider{},
	}
	if cfg.HasCloudCredentials() {
		t.Error("empty providers should have no credentials")
	}

	cfg.Providers["aws"] = Provider{Credentials: "env"}
	if !cfg.HasCloudCredentials() {
		t.Error("expected HasCloudCredentials to return true")
	}
}

func TestHasCloudCredentialsMultiProvider(t *testing.T) {
	cfg := &Config{
		Providers: map[string]Provider{
			"aws":   {Credentials: "env", Region: "us-east-1"},
			"gcp":   {Credentials: "env", Project: "my-project"},
			"azure": {Credentials: "env"},
		},
	}

	if !cfg.HasCloudCredentials() {
		t.Error("expected true with all three providers configured")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	if err := os.WriteFile(path, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
