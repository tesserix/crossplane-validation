package tofu

import (
	"testing"

	"github.com/tesserix/crossplane-validation/pkg/config"
)

func TestSetupProviderEnvAWS(t *testing.T) {
	providers := map[string]config.Provider{
		"aws": {Region: "us-east-1"},
	}

	env := setupProviderEnv(providers)

	found := false
	for _, e := range env {
		if e == "AWS_DEFAULT_REGION=us-east-1" {
			found = true
		}
	}
	if !found {
		t.Error("expected AWS_DEFAULT_REGION in env")
	}
}

func TestSetupProviderEnvGCP(t *testing.T) {
	providers := map[string]config.Provider{
		"gcp": {Project: "my-project"},
	}

	env := setupProviderEnv(providers)

	found := false
	for _, e := range env {
		if e == "GOOGLE_PROJECT=my-project" {
			found = true
		}
	}
	if !found {
		t.Error("expected GOOGLE_PROJECT in env")
	}
}

func TestSetupProviderEnvMulti(t *testing.T) {
	providers := map[string]config.Provider{
		"aws":   {Region: "eu-west-1"},
		"gcp":   {Project: "prod-project"},
		"azure": {},
	}

	env := setupProviderEnv(providers)

	if len(env) != 2 {
		t.Errorf("expected 2 env vars (aws region + gcp project), got %d", len(env))
	}
}

func TestMapAction(t *testing.T) {
	tests := []struct {
		actions []string
		want    string
	}{
		{[]string{"create"}, "create"},
		{[]string{"delete"}, "delete"},
		{[]string{"no-op"}, "no-op"},
		{[]string{"update"}, "update"},
		{[]string{"delete", "create"}, "update"},
		{nil, "no-op"},
		{[]string{}, "no-op"},
	}

	for _, tt := range tests {
		got := mapAction(tt.actions)
		if got != tt.want {
			t.Errorf("mapAction(%v) = %q, want %q", tt.actions, got, tt.want)
		}
	}
}

func TestParsePlanJSON(t *testing.T) {
	jsonData := []byte(`{
		"resource_changes": [
			{
				"address": "aws_s3_bucket.logs",
				"type": "aws_s3_bucket",
				"name": "logs",
				"change": {
					"actions": ["create"],
					"before": null,
					"after": {"region": "us-east-1", "acl": "private"}
				}
			},
			{
				"address": "google_sql_database_instance.db",
				"type": "google_sql_database_instance",
				"name": "db",
				"change": {
					"actions": ["update"],
					"before": {"tier": "db-f1-micro"},
					"after": {"tier": "db-n1-standard-2"}
				}
			},
			{
				"address": "azurerm_resource_group.old_rg",
				"type": "azurerm_resource_group",
				"name": "old_rg",
				"change": {
					"actions": ["delete"],
					"before": {"location": "eastus"},
					"after": null
				}
			}
		]
	}`)

	result, err := parsePlanJSON(jsonData, nil)
	if err != nil {
		t.Fatalf("parsing plan JSON: %v", err)
	}

	if result.Summary.Add != 1 {
		t.Errorf("expected 1 add, got %d", result.Summary.Add)
	}
	if result.Summary.Change != 1 {
		t.Errorf("expected 1 change, got %d", result.Summary.Change)
	}
	if result.Summary.Destroy != 1 {
		t.Errorf("expected 1 destroy, got %d", result.Summary.Destroy)
	}
	if !result.HasChanges {
		t.Error("expected HasChanges to be true")
	}

	if len(result.Changes) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(result.Changes))
	}

	if result.Changes[0].Action != "create" || result.Changes[0].ResourceType != "aws_s3_bucket" {
		t.Errorf("first change: got action=%s type=%s", result.Changes[0].Action, result.Changes[0].ResourceType)
	}
	if result.Changes[1].Action != "update" || result.Changes[1].ResourceType != "google_sql_database_instance" {
		t.Errorf("second change: got action=%s type=%s", result.Changes[1].Action, result.Changes[1].ResourceType)
	}
	if result.Changes[2].Action != "delete" || result.Changes[2].ResourceType != "azurerm_resource_group" {
		t.Errorf("third change: got action=%s type=%s", result.Changes[2].Action, result.Changes[2].ResourceType)
	}
}

func TestParsePlanJSONNoChanges(t *testing.T) {
	jsonData := []byte(`{"resource_changes": []}`)

	result, err := parsePlanJSON(jsonData, nil)
	if err != nil {
		t.Fatal(err)
	}

	if result.HasChanges {
		t.Error("expected HasChanges to be false")
	}
}

func TestFindBinary(t *testing.T) {
	binary := findBinary()
	if binary == "" {
		t.Error("findBinary returned empty string")
	}
}

func TestIsPlanChangesError(t *testing.T) {
	if isPlanChangesError(nil) {
		t.Error("nil error should not be plan changes error")
	}
}
