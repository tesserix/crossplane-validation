package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds the crossplane-validate configuration.
type Config struct {
	ManifestDirs []string            `yaml:"manifests"`
	Providers    map[string]Provider `yaml:"providers"`
	Settings     Settings            `yaml:"settings"`
}

// Provider holds cloud provider configuration for Mode 3 (cloud plan).
type Provider struct {
	// Source of credentials: "env", "file", or "secret/<NAME>"
	Credentials string `yaml:"credentials"`
	Region      string `yaml:"region,omitempty"`
	Project     string `yaml:"project,omitempty"`
}

// Settings holds optional behavior configuration.
type Settings struct {
	Timeout      string   `yaml:"timeout"`
	DiffFormat   string   `yaml:"diff-format"`
	IgnoreFields []string `yaml:"ignore-fields"`
}

// Load reads config from a file. Returns defaults if file doesn't exist.
func Load(path string) (*Config, error) {
	cfg := &Config{
		ManifestDirs: []string{"."},
		Providers:    make(map[string]Provider),
		Settings: Settings{
			Timeout:    "5m",
			DiffFormat: "terraform",
			IgnoreFields: []string{
				"metadata.resourceVersion",
				"metadata.uid",
				"metadata.creationTimestamp",
				"metadata.generation",
				"status.conditions",
			},
		},
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if len(cfg.ManifestDirs) == 0 {
		cfg.ManifestDirs = []string{"."}
	}

	return cfg, nil
}

// HasCloudCredentials returns true if any provider has credentials configured.
func (c *Config) HasCloudCredentials() bool {
	for _, p := range c.Providers {
		if p.Credentials != "" {
			return true
		}
	}
	return false
}
