package tofu

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tesserix/crossplane-validation/pkg/config"
	"github.com/tesserix/crossplane-validation/pkg/hcl"
)

// PlanResult holds the output of an OpenTofu plan.
type PlanResult struct {
	RawOutput  string
	Changes    []ResourceChange
	HasChanges bool
	Summary    PlanSummary
}

// ResourceChange represents a single resource change from the tofu plan.
type ResourceChange struct {
	Address      string                 // e.g., "aws_s3_bucket.my_bucket"
	Action       string                 // "create", "update", "delete", "no-op"
	ResourceType string                 // e.g., "aws_s3_bucket"
	Name         string                 // e.g., "my_bucket"
	Before       map[string]interface{} // current state (nil for creates)
	After        map[string]interface{} // planned state (nil for deletes)
	SourceKey    string                 // Crossplane resource key for correlation
}

// PlanSummary counts changes by type.
type PlanSummary struct {
	Add     int
	Change  int
	Destroy int
}

// Plan runs OpenTofu plan comparing base state against target configuration.
func Plan(baseHCL, targetHCL *hcl.ConvertedSet, providers map[string]config.Provider) (*PlanResult, error) {
	// Create a temp directory for the tofu workspace
	workDir, err := os.MkdirTemp("", "crossplane-validate-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	// Write the target HCL configuration
	mainTF := filepath.Join(workDir, "main.tf")
	if err := os.WriteFile(mainTF, []byte(targetHCL.ToHCL()), 0644); err != nil {
		return nil, fmt.Errorf("writing main.tf: %w", err)
	}

	// Setup provider credentials as environment variables
	env := setupProviderEnv(providers)

	// Find tofu binary (prefer tofu, fallback to terraform)
	binary := findBinary()

	// Init
	if err := runTofu(binary, workDir, env, "init", "-no-color"); err != nil {
		return nil, fmt.Errorf("tofu init: %w", err)
	}

	// If we have base HCL, import existing state
	if baseHCL != nil && len(baseHCL.ResourceBlocks) > 0 {
		// Write base config, apply to get state, then swap to target
		if err := importBaseState(binary, workDir, env, baseHCL); err != nil {
			// Non-fatal: we can still plan without base state
			fmt.Fprintf(os.Stderr, "warning: could not import base state: %v\n", err)
		}
	}

	// Plan with JSON output
	planFile := filepath.Join(workDir, "plan.out")
	err = runTofu(binary, workDir, env, "plan", "-out="+planFile, "-no-color")
	// Plan returns exit code 2 when there are changes — that's expected
	if err != nil && !isPlanChangesError(err) {
		return nil, fmt.Errorf("tofu plan: %w", err)
	}

	// Get plan JSON
	jsonOut, err := runTofuOutput(binary, workDir, env, "show", "-json", planFile)
	if err != nil {
		return nil, fmt.Errorf("tofu show: %w", err)
	}

	return parsePlanJSON(jsonOut, targetHCL)
}

func findBinary() string {
	if path, err := exec.LookPath("tofu"); err == nil {
		return path
	}
	if path, err := exec.LookPath("terraform"); err == nil {
		return path
	}
	return "tofu" // will fail with a clear error
}

func runTofu(binary, dir string, env []string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), binary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stderr // plan output goes to stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runTofuOutput(binary, dir string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), binary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	return cmd.Output()
}

func setupProviderEnv(providers map[string]config.Provider) []string {
	var env []string
	for name, prov := range providers {
		switch strings.ToLower(name) {
		case "aws":
			env = append(env, setupAWSEnv(prov)...)
		case "gcp":
			env = append(env, setupGCPEnv(prov)...)
		case "azure", "azurerm":
			env = append(env, setupAzureEnv(prov)...)
		case "azuread":
			env = append(env, setupAzureADEnv(prov)...)
		}
	}
	return env
}

func setupAWSEnv(prov config.Provider) []string {
	var env []string
	if prov.Region != "" {
		env = append(env, "AWS_DEFAULT_REGION="+prov.Region)
	}
	switch prov.Credentials {
	case "oidc":
		// GitHub Actions OIDC: assume role via web identity token
		if prov.RoleARN != "" {
			env = append(env, "AWS_ROLE_ARN="+prov.RoleARN)
		}
		// GitHub Actions sets ACTIONS_ID_TOKEN_REQUEST_TOKEN/URL automatically
		// Terraform AWS provider handles OIDC natively via assume_role_with_web_identity
		if tokenFile := prov.OIDCTokenFile; tokenFile != "" {
			env = append(env, "AWS_WEB_IDENTITY_TOKEN_FILE="+tokenFile)
		} else if v := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN"); v != "" {
			// GitHub Actions: token is available via OIDC
			env = append(env, "AWS_WEB_IDENTITY_TOKEN_FILE=/dev/stdin")
		}
	case "default", "":
		// Use default credential chain: env vars → shared credentials → instance profile
		// No additional env vars needed — AWS SDK and Terraform handle this natively
	}
	return env
}

func setupGCPEnv(prov config.Provider) []string {
	var env []string
	if prov.Project != "" {
		env = append(env, "GOOGLE_PROJECT="+prov.Project)
	}
	switch prov.Credentials {
	case "oidc":
		// Workload Identity Federation for CI/CD
		if prov.WorkloadPool != "" {
			// The credential config file is generated by the GCP auth action
			// and pointed to by GOOGLE_APPLICATION_CREDENTIALS
		}
		if prov.ServiceAccount != "" {
			env = append(env, "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT="+prov.ServiceAccount)
		}
	case "default", "":
		// Use Application Default Credentials (ADC):
		// gcloud auth → GOOGLE_APPLICATION_CREDENTIALS → GCE metadata
	}
	return env
}

func setupAzureEnv(prov config.Provider) []string {
	var env []string
	if prov.SubscriptionID != "" {
		env = append(env, "ARM_SUBSCRIPTION_ID="+prov.SubscriptionID)
	}
	if prov.TenantID != "" {
		env = append(env, "ARM_TENANT_ID="+prov.TenantID)
	}
	if prov.ClientID != "" {
		env = append(env, "ARM_CLIENT_ID="+prov.ClientID)
	}
	switch prov.Credentials {
	case "oidc":
		// GitHub Actions OIDC with Azure federated credentials
		env = append(env, "ARM_USE_OIDC=true")
		if tokenFile := prov.OIDCTokenFile; tokenFile != "" {
			env = append(env, "ARM_OIDC_TOKEN_FILE_PATH="+tokenFile)
		}
	case "cli":
		// Use Azure CLI credentials (az login)
		env = append(env, "ARM_USE_CLI=true")
	case "msi", "managed-identity":
		// Use Managed Service Identity (for Azure-hosted runners)
		env = append(env, "ARM_USE_MSI=true")
	case "default", "":
		// Auto-detect: OIDC → CLI → MSI → env vars
		// Check if running in GitHub Actions with OIDC
		if os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") != "" {
			env = append(env, "ARM_USE_OIDC=true")
		}
	}
	return env
}

func setupAzureADEnv(prov config.Provider) []string {
	// Azure AD provider uses the same auth as azurerm
	return setupAzureEnv(prov)
}

func importBaseState(binary, workDir string, env []string, baseHCL *hcl.ConvertedSet) error {
	for _, block := range baseHCL.ResourceBlocks {
		if block.ImportID == "" {
			continue
		}
		addr := block.Label + "." + block.Name
		fmt.Fprintf(os.Stderr, "Importing %s...\n", addr)
		err := runTofu(binary, workDir, env, "import", "-no-color", addr, block.ImportID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not import %s: %v\n", addr, err)
		}
	}
	return nil
}

func isPlanChangesError(err error) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode() == 2
	}
	return false
}

func parsePlanJSON(data []byte, targetHCL *hcl.ConvertedSet) (*PlanResult, error) {
	var planJSON struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Type    string `json:"type"`
			Name    string `json:"name"`
			Change  struct {
				Actions []string               `json:"actions"`
				Before  map[string]interface{} `json:"before"`
				After   map[string]interface{} `json:"after"`
			} `json:"change"`
		} `json:"resource_changes"`
	}

	if err := json.Unmarshal(data, &planJSON); err != nil {
		return nil, fmt.Errorf("parsing plan JSON: %w", err)
	}

	result := &PlanResult{}

	// Build source key lookup from HCL blocks
	sourceKeys := map[string]string{}
	if targetHCL != nil {
		for _, block := range targetHCL.ResourceBlocks {
			addr := block.Label + "." + block.Name
			sourceKeys[addr] = block.SourceKey
		}
	}

	for _, rc := range planJSON.ResourceChanges {
		action := mapAction(rc.Change.Actions)
		change := ResourceChange{
			Address:      rc.Address,
			Action:       action,
			ResourceType: rc.Type,
			Name:         rc.Name,
			Before:       rc.Change.Before,
			After:        rc.Change.After,
			SourceKey:    sourceKeys[rc.Address],
		}
		result.Changes = append(result.Changes, change)

		switch action {
		case "create":
			result.Summary.Add++
		case "update":
			result.Summary.Change++
		case "delete":
			result.Summary.Destroy++
		}
	}

	result.HasChanges = result.Summary.Add > 0 || result.Summary.Change > 0 || result.Summary.Destroy > 0
	return result, nil
}

func mapAction(actions []string) string {
	if len(actions) == 0 {
		return "no-op"
	}
	if len(actions) == 1 {
		switch actions[0] {
		case "create":
			return "create"
		case "delete":
			return "delete"
		case "no-op":
			return "no-op"
		}
	}
	// ["delete", "create"] = replace, ["update"] = update
	for _, a := range actions {
		if a == "update" {
			return "update"
		}
	}
	return "update"
}
