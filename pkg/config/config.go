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

// Provider holds cloud provider configuration for cloud-aware plan.
type Provider struct {
	// Credentials specifies how to authenticate:
	//   "env"      — use standard environment variables (AWS_*, GOOGLE_*, ARM_*)
	//   "default"  — use default credential chain (AWS profiles, GCP ADC, Azure CLI)
	//   "oidc"     — use OIDC/workload identity federation (for CI/CD)
	//   ""         — auto-detect from environment
	Credentials    string `yaml:"credentials"`
	Region         string `yaml:"region,omitempty"`
	Project        string `yaml:"project,omitempty"`
	SubscriptionID string `yaml:"subscription-id,omitempty"`
	TenantID       string `yaml:"tenant-id,omitempty"`
	ClientID       string `yaml:"client-id,omitempty"`

	// OIDC-specific fields (GitHub Actions, GitLab CI, etc.)
	OIDCTokenFile  string `yaml:"oidc-token-file,omitempty"`
	RoleARN        string `yaml:"role-arn,omitempty"`        // AWS: role to assume via OIDC
	WorkloadPool   string `yaml:"workload-pool,omitempty"`   // GCP: workload identity pool
	ServiceAccount string `yaml:"service-account,omitempty"` // GCP: service account email
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

// HasCloudCredentials returns true if any provider has credentials configured
// or if standard cloud credentials are detected in the environment.
func (c *Config) HasCloudCredentials() bool {
	// Check explicit config
	for _, p := range c.Providers {
		if p.Credentials != "" {
			return true
		}
	}

	// Auto-detect from environment
	awsVars := []string{"AWS_ACCESS_KEY_ID", "AWS_PROFILE", "AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE"}
	gcpVars := []string{"GOOGLE_APPLICATION_CREDENTIALS", "GOOGLE_CREDENTIALS", "CLOUDSDK_CONFIG"}
	azureVars := []string{"ARM_CLIENT_ID", "ARM_SUBSCRIPTION_ID", "AZURE_CLIENT_ID", "ARM_USE_OIDC", "ARM_USE_CLI", "ARM_USE_MSI"}

	for _, v := range append(append(awsVars, gcpVars...), azureVars...) {
		if os.Getenv(v) != "" {
			return true
		}
	}

	// Check if running in GitHub Actions with OIDC available
	if os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") != "" {
		return true
	}

	return false
}
