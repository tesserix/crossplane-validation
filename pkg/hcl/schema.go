package hcl

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type ProviderSchema struct {
	ResourceTypes []string
	index         map[string]string // snake_kind -> full terraform type
}

var (
	schemaCache   = map[string]*ProviderSchema{}
	schemaCacheMu sync.Mutex
)

// LoadProviderSchema runs terraform init + providers schema to get the full
// list of resource types for a given provider. Results are cached per provider.
func LoadProviderSchema(provider, source string) (*ProviderSchema, error) {
	schemaCacheMu.Lock()
	if cached, ok := schemaCache[provider]; ok {
		schemaCacheMu.Unlock()
		return cached, nil
	}
	schemaCacheMu.Unlock()

	workDir, err := os.MkdirTemp("", "crossplane-validate-schema-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	hcl := fmt.Sprintf(`terraform {
  required_providers {
    %s = {
      source = %q
    }
  }
}
`, provider, source)

	if provider == "azurerm" {
		hcl += fmt.Sprintf("provider %q {\n  features {}\n}\n", provider)
	}

	if err := os.WriteFile(filepath.Join(workDir, "main.tf"), []byte(hcl), 0644); err != nil {
		return nil, err
	}

	binary := findTerraformBinary()

	initCmd := exec.Command(binary, "init", "-no-color")
	initCmd.Dir = workDir
	initCmd.Env = os.Environ()
	if out, err := initCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("terraform init: %s", string(out))
	}

	schemaCmd := exec.Command(binary, "providers", "schema", "-json")
	schemaCmd.Dir = workDir
	schemaCmd.Env = os.Environ()
	out, err := schemaCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("terraform providers schema: %w", err)
	}

	var raw struct {
		ProviderSchemas map[string]struct {
			ResourceSchemas map[string]json.RawMessage `json:"resource_schemas"`
		} `json:"provider_schemas"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing schema: %w", err)
	}

	ps := &ProviderSchema{
		index: make(map[string]string),
	}

	for key, provSchema := range raw.ProviderSchemas {
		if !strings.Contains(key, provider) {
			continue
		}
		for resType := range provSchema.ResourceSchemas {
			ps.ResourceTypes = append(ps.ResourceTypes, resType)

			suffix := strings.TrimPrefix(resType, provider+"_")
			ps.index[suffix] = resType

			parts := strings.Split(suffix, "_")
			if len(parts) > 1 {
				ps.index[strings.Join(parts[1:], "_")] = resType
			}
		}
	}

	schemaCacheMu.Lock()
	schemaCache[provider] = ps
	schemaCacheMu.Unlock()

	return ps, nil
}

// MatchResourceType finds the best matching Terraform resource type for a given
// Crossplane kind using the provider schema.
func (ps *ProviderSchema) MatchResourceType(kind, service string) string {
	snakeKind := camelToSnake(kind)

	// Try exact: service_kind (e.g., "storage_account")
	if rt, ok := ps.index[service+"_"+snakeKind]; ok {
		return rt
	}

	// Try without service prefix (e.g., "account" → might match "storage_account")
	if rt, ok := ps.index[snakeKind]; ok {
		return rt
	}

	// Try fuzzy: search all resource types for one containing the snake_kind
	for _, rt := range ps.ResourceTypes {
		if strings.HasSuffix(rt, "_"+snakeKind) {
			return rt
		}
	}

	return ""
}

func findTerraformBinary() string {
	if path, err := exec.LookPath("tofu"); err == nil {
		return path
	}
	if path, err := exec.LookPath("terraform"); err == nil {
		return path
	}
	return "terraform"
}
