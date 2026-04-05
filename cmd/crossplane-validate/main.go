package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/config"
	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/hcl"
	"github.com/tesserix/crossplane-validation/pkg/lint"
	"github.com/tesserix/crossplane-validation/pkg/manifest"
	"github.com/tesserix/crossplane-validation/pkg/plan"
	"github.com/tesserix/crossplane-validation/pkg/renderer"
	"github.com/tesserix/crossplane-validation/pkg/tofu"
	"github.com/tesserix/crossplane-validation/pkg/validate"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "crossplane-validate",
		Short:   "Validate Crossplane resources before applying — like terraform plan for Crossplane",
		Version: version,
	}

	root.AddCommand(planCmd())
	root.AddCommand(diffCmd())
	root.AddCommand(renderCmd())
	root.AddCommand(scanCmd())
	root.AddCommand(validateCmd())
	root.AddCommand(lintCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(driftCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func planCmd() *cobra.Command {
	var (
		baseBranch       string
		targetRef        string
		configFile       string
		outputFmt        string
		manifestDir      string
		cloudMode        bool
		detailedExitcode bool
		showSensitive    bool
		liveMode         bool
		operatorAddr     string
		apiToken         string
		kubeContext      string
		namespace        string
	)

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what will change when the current branch is merged",
		Long: `Compares Crossplane manifests between branches and shows a terraform-style plan.

By default, performs a git-based diff comparing rendered manifests between base and target branches.
With --cloud, converts to HCL and runs OpenTofu plan with read-only credentials for cloud-aware validation.
With --live, connects to the in-cluster operator and compares proposed manifests against live cluster state.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if manifestDir != "" {
				cfg.ManifestDirs = []string{manifestDir}
			}

			if liveMode {
				ns := namespace
				if ns == "" {
					ns = cfg.Operator.Namespace
				}
				addr := operatorAddr
				if addr == "" {
					addr = cfg.Operator.Address
				}
				return runLivePlan(liveOptions{
					operatorAddr:  addr,
					apiToken:      apiToken,
					kubeContext:   kubeContext,
					namespace:     ns,
					manifestDirs:  cfg.ManifestDirs,
					outputFmt:     outputFmt,
					showSensitive: showSensitive,
				})
			}

			fmt.Fprintln(os.Stderr, "Scanning manifests...")
			baseManifests, err := manifest.ScanFromGitRef(cfg.ManifestDirs, baseBranch)
			if err != nil {
				return fmt.Errorf("scanning base branch %s: %w", baseBranch, err)
			}

			targetManifests, err := manifest.ScanFromGitRef(cfg.ManifestDirs, targetRef)
			if err != nil {
				return fmt.Errorf("scanning target ref %s: %w", targetRef, err)
			}

			fmt.Fprintln(os.Stderr, "Rendering compositions...")
			baseRendered, err := renderer.Render(baseManifests)
			if err != nil {
				return fmt.Errorf("rendering base: %w", err)
			}

			targetRendered, err := renderer.Render(targetManifests)
			if err != nil {
				return fmt.Errorf("rendering target: %w", err)
			}

			diff.ShowSensitive = showSensitive

			fmt.Fprintln(os.Stderr, "Computing structural diff...")
			structDiff := diff.Compute(baseRendered, targetRendered)

			var cloudPlan *tofu.PlanResult
			if cloudMode {
				if !cfg.HasCloudCredentials() {
					fmt.Fprintln(os.Stderr, "Cloud mode: no credentials detected")
					fmt.Fprintln(os.Stderr, "  Checked: AWS_ACCESS_KEY_ID, AWS_PROFILE, GOOGLE_APPLICATION_CREDENTIALS,")
					fmt.Fprintln(os.Stderr, "           ARM_CLIENT_ID, ARM_SUBSCRIPTION_ID, OIDC tokens")
					fmt.Fprintln(os.Stderr, "  Tip: authenticate with your cloud provider or set credentials in .crossplane-validate.yml")
					fmt.Fprintln(os.Stderr, "  Skipping cloud plan.")
				} else {
					printDetectedAuth(cfg)

					fmt.Fprintln(os.Stderr, "Loading provider schemas...")
					hcl.UseSchemaLookup = true
					fmt.Fprintln(os.Stderr, "Converting to HCL...")
					baseHCL, err := hcl.Convert(baseRendered, cfg.Providers)
					if err != nil {
						return fmt.Errorf("converting base to HCL: %w", err)
					}
					targetHCL, err := hcl.Convert(targetRendered, cfg.Providers)
					if err != nil {
						return fmt.Errorf("converting target to HCL: %w", err)
					}

					fmt.Fprintln(os.Stderr, "Running cloud plan (read-only)...")
					cloudPlan, err = tofu.Plan(baseHCL, targetHCL, cfg.Providers)
					if err != nil {
						return fmt.Errorf("running cloud plan: %w", err)
					}
				}
			}

			fmt.Fprintln(os.Stderr, "Validating schemas...")
			validationIssues := validate.Validate(targetManifests)

			result := &plan.Result{
				StructuralDiff:   structDiff,
				CloudPlan:        cloudPlan,
				ValidationIssues: validationIssues,
			}

			if err := plan.Render(result, outputFmt, os.Stdout); err != nil {
				return err
			}

			if detailedExitcode {
				if structDiff.Summary.ToAdd > 0 || structDiff.Summary.ToChange > 0 || structDiff.Summary.ToDelete > 0 {
					os.Exit(2)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&baseBranch, "base", "main", "Base branch to compare against")
	cmd.Flags().StringVar(&targetRef, "target", "HEAD", "Target ref (branch/commit) to validate")
	cmd.Flags().StringVarP(&configFile, "config", "c", ".crossplane-validate.yml", "Config file path")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "terminal", "Output format: terminal, markdown, json")
	cmd.Flags().StringVarP(&manifestDir, "manifests", "m", "", "Manifest directory (overrides config)")
	cmd.Flags().BoolVar(&cloudMode, "cloud", false, "Enable cloud-aware plan using OpenTofu (requires credentials)")
	cmd.Flags().BoolVar(&detailedExitcode, "detailed-exitcode", false, "Return exit code 2 when changes are detected (0=no changes, 1=error, 2=changes)")
	cmd.Flags().BoolVar(&showSensitive, "show-sensitive", false, "Show sensitive field values in plain text (passwords, tokens, keys)")
	cmd.Flags().BoolVar(&liveMode, "live", false, "Connect to in-cluster operator for live state comparison")
	cmd.Flags().StringVar(&operatorAddr, "operator-address", "", "Operator address (e.g. localhost:9443 or crossplane-validator.hub.civica.tech:443)")
	cmd.Flags().StringVar(&apiToken, "api-token", "", "API bearer token for operator authentication (or set CROSSPLANE_VALIDATE_API_TOKEN)")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kubernetes context to use")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Operator namespace (default: crossplane-system)")

	return cmd
}

func statusCmd() *cobra.Command {
	var (
		operatorAddr string
		apiToken     string
		kubeContext  string
		namespace    string
		configFile   string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show live Crossplane resource status from the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configFile)
			if err != nil {
				return err
			}

			ns := namespace
			if ns == "" {
				ns = cfg.Operator.Namespace
			}
			addr := operatorAddr
			if addr == "" {
				addr = cfg.Operator.Address
			}

			return runLiveStatus(liveOptions{
				operatorAddr: addr,
				apiToken:     apiToken,
				kubeContext:  kubeContext,
				namespace:    ns,
			})
		},
	}

	cmd.Flags().StringVar(&operatorAddr, "operator-address", "", "Operator address")
	cmd.Flags().StringVar(&apiToken, "api-token", "", "API bearer token (or set CROSSPLANE_VALIDATE_API_TOKEN)")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kubernetes context to use")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Operator namespace")
	cmd.Flags().StringVarP(&configFile, "config", "c", ".crossplane-validate.yml", "Config file path")
	return cmd
}

func driftCmd() *cobra.Command {
	var (
		operatorAddr string
		apiToken     string
		kubeContext  string
		namespace    string
		configFile   string
		manifestDir  string
	)

	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Show differences between git manifests and live cluster state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configFile)
			if err != nil {
				return err
			}

			if manifestDir != "" {
				cfg.ManifestDirs = []string{manifestDir}
			}

			ns := namespace
			if ns == "" {
				ns = cfg.Operator.Namespace
			}
			addr := operatorAddr
			if addr == "" {
				addr = cfg.Operator.Address
			}

			return runLiveDrift(liveOptions{
				operatorAddr: addr,
				apiToken:     apiToken,
				kubeContext:  kubeContext,
				namespace:    ns,
				manifestDirs: cfg.ManifestDirs,
			})
		},
	}

	cmd.Flags().StringVar(&operatorAddr, "operator-address", "", "Operator address")
	cmd.Flags().StringVar(&apiToken, "api-token", "", "API bearer token (or set CROSSPLANE_VALIDATE_API_TOKEN)")
	cmd.Flags().StringVar(&kubeContext, "context", "", "Kubernetes context to use")
	cmd.Flags().StringVar(&namespace, "namespace", "", "Operator namespace")
	cmd.Flags().StringVarP(&configFile, "config", "c", ".crossplane-validate.yml", "Config file path")
	cmd.Flags().StringVarP(&manifestDir, "manifests", "m", "", "Manifest directory")
	return cmd
}

func diffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff [path]",
		Short: "Show structural diff of manifests between branches",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			base, _ := cmd.Flags().GetString("base")

			baseManifests, err := manifest.ScanFromGitRef([]string{dir}, base)
			if err != nil {
				return err
			}
			targetManifests, err := manifest.ScanFromGitRef([]string{dir}, "HEAD")
			if err != nil {
				return err
			}

			baseRendered, _ := renderer.Render(baseManifests)
			targetRendered, _ := renderer.Render(targetManifests)

			d := diff.Compute(baseRendered, targetRendered)
			return plan.RenderDiffOnly(d, os.Stdout)
		},
	}

	cmd.Flags().String("base", "main", "Base branch")
	return cmd
}

func renderCmd() *cobra.Command {
	var functionsDir string

	cmd := &cobra.Command{
		Use:   "render [path...]",
		Short: "Render compositions and show resulting managed resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := []string{"."}
			if len(args) > 0 {
				dirs = args
			}

			if functionsDir != "" {
				dirs = append(dirs, functionsDir)
			}

			manifests, err := manifest.ScanWithKustomize(dirs)
			if err != nil {
				return err
			}

			rendered, err := renderer.Render(manifests)
			if err != nil {
				return err
			}

			return renderer.Print(rendered, os.Stdout)
		},
	}

	cmd.Flags().StringVarP(&functionsDir, "functions", "f", "", "Directory containing Function definitions (auto-detected if in same tree)")
	return cmd
}

func validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [path...]",
		Short: "Validate XRs and Claims against their XRD schemas",
		Long: `Scans directories for Crossplane manifests and validates:
- XRs/Claims against their XRD openAPIV3Schema (required fields, enum values, type mismatches)
- ProviderConfig references exist in the scanned manifests
- Composition references match XR types`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := []string{"."}
			if len(args) > 0 {
				dirs = args
			}

			rs, err := manifest.Scan(dirs)
			if err != nil {
				return fmt.Errorf("scanning manifests: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Scanned %d resources\n", len(rs.AllResources()))

			issues := validate.Validate(rs)

			if len(issues) == 0 {
				fmt.Fprintln(os.Stdout, "No validation issues found.")
				return nil
			}

			errorCount := 0
			warningCount := 0

			fmt.Fprintln(os.Stdout)
			fmt.Fprintln(os.Stdout, "Errors:")
			for _, issue := range issues {
				if issue.Severity != "error" {
					continue
				}
				errorCount++
				field := issue.Field
				if field != "" {
					field = " " + field + ":"
				}
				fmt.Fprintf(os.Stdout, "  \u2717 %s%s %s\n", issue.Resource, field, issue.Message)
			}
			if errorCount == 0 {
				fmt.Fprintln(os.Stdout, "  (none)")
			}

			fmt.Fprintln(os.Stdout)
			fmt.Fprintln(os.Stdout, "Warnings:")
			for _, issue := range issues {
				if issue.Severity != "warning" {
					continue
				}
				warningCount++
				field := issue.Field
				if field != "" {
					field = " " + field + ":"
				}
				fmt.Fprintf(os.Stdout, "  \u26a0 %s%s %s\n", issue.Resource, field, issue.Message)
			}
			if warningCount == 0 {
				fmt.Fprintln(os.Stdout, "  (none)")
			}

			fmt.Fprintf(os.Stdout, "\nTotal: %d errors, %d warnings\n", errorCount, warningCount)

			if errorCount > 0 {
				os.Exit(1)
			}

			return nil
		},
	}
}

func lintCmd() *cobra.Command {
	var (
		tools       []string
		outputFmt   string
		autoInstall bool
		noInstall   bool
	)

	cmd := &cobra.Command{
		Use:   "lint [path...]",
		Short: "Run external linting and validation tools on manifests",
		Long: `Wraps popular open-source tools for comprehensive YAML and Kubernetes validation.

By default, missing tools are downloaded automatically to ~/.crossplane-validate/tools/.
Use --no-install to skip auto-installation.

Supported tools:
  yamllint             YAML syntax and style validation
  kubeconform          Kubernetes manifest schema validation
  pluto                Deprecated API version detection
  kube-linter          Kubernetes best practices and security analysis
  crossplane-validate  Crossplane composition and XRD validation (crossplane CLI)

Use --tools to run specific tools: --tools=yamllint,kubeconform`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := []string{"."}
			if len(args) > 0 {
				dirs = args
			}

			autoInstall = !noInstall

			// Show available tools before install
			available := lint.DetectTools()
			fmt.Fprintln(os.Stderr, "Tool detection:")
			missing := 0
			for _, t := range lint.AvailableTools() {
				status := "available"
				if !available[t.Name] {
					status = "not found"
					missing++
				}
				fmt.Fprintf(os.Stderr, "  %-22s %s (%s)\n", t.Name, t.Purpose, status)
			}
			fmt.Fprintln(os.Stderr)

			if missing > 0 && autoInstall {
				fmt.Fprintf(os.Stderr, "Auto-installing %d missing tool(s)...\n", missing)
			}

			result, err := lint.Run(dirs, tools, autoInstall)
			if err != nil {
				return fmt.Errorf("running lint: %w", err)
			}

			if len(result.Tools) == 0 {
				fmt.Fprintln(os.Stdout, "No external tools available.")
				if noInstall {
					fmt.Fprintln(os.Stdout, "Run without --no-install to auto-download tools, or install manually:")
					fmt.Fprintln(os.Stdout, "  brew install yamllint kubeconform kube-linter")
					fmt.Fprintln(os.Stdout, "  brew install FairwindsOps/tap/pluto")
				}
				return nil
			}

			if outputFmt == "json" {
				return renderLintJSON(result, os.Stdout)
			}

			return renderLintTerminal(result, os.Stdout)
		},
	}

	cmd.Flags().StringSliceVar(&tools, "tools", nil, "Specific tools to run (comma-separated)")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "terminal", "Output format: terminal, json")
	cmd.Flags().BoolVar(&noInstall, "no-install", false, "Skip auto-installation of missing tools")
	return cmd
}

func renderLintTerminal(result *lint.Result, w *os.File) error {
	fmt.Fprintf(w, "Tools run: %s\n\n", strings.Join(result.Tools, ", "))

	if len(result.Issues) == 0 {
		fmt.Fprintln(w, "No issues found.")
		return nil
	}

	errorCount, warningCount := 0, 0
	// Group by tool
	byTool := map[string][]lint.Issue{}
	for _, issue := range result.Issues {
		byTool[issue.Tool] = append(byTool[issue.Tool], issue)
		if issue.Severity == "error" {
			errorCount++
		} else {
			warningCount++
		}
	}

	for _, toolName := range result.Tools {
		issues := byTool[toolName]
		if len(issues) == 0 {
			fmt.Fprintf(w, "%s: no issues\n\n", toolName)
			continue
		}

		fmt.Fprintf(w, "%s (%d issues)\n", toolName, len(issues))
		for _, issue := range issues {
			prefix := "\u26a0"
			if issue.Severity == "error" {
				prefix = "\u2717"
			}
			loc := ""
			if issue.File != "" {
				loc = issue.File
				if issue.Resource != "" {
					loc += " " + issue.Resource
				}
				loc += ": "
			} else if issue.Resource != "" {
				loc = issue.Resource + ": "
			}
			fmt.Fprintf(w, "  %s %s%s\n", prefix, loc, issue.Message)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Total: %d errors, %d warnings\n", errorCount, warningCount)

	if errorCount > 0 {
		os.Exit(1)
	}
	return nil
}

func renderLintJSON(result *lint.Result, w *os.File) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func printDetectedAuth(cfg *config.Config) {
	fmt.Fprintln(os.Stderr, "Cloud authentication:")

	// Check explicit config
	for name, prov := range cfg.Providers {
		if prov.Credentials != "" {
			fmt.Fprintf(os.Stderr, "  %-12s %s (from config)\n", name+":", prov.Credentials)
		}
	}

	// Auto-detect from environment
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		fmt.Fprintln(os.Stderr, "  aws:         access key (env)")
	} else if os.Getenv("AWS_PROFILE") != "" {
		fmt.Fprintf(os.Stderr, "  aws:         profile %q (env)\n", os.Getenv("AWS_PROFILE"))
	} else if os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE") != "" {
		fmt.Fprintln(os.Stderr, "  aws:         OIDC web identity (env)")
	} else if os.Getenv("AWS_ROLE_ARN") != "" {
		fmt.Fprintln(os.Stderr, "  aws:         assume role (env)")
	}

	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		fmt.Fprintln(os.Stderr, "  gcp:         service account key (env)")
	} else if os.Getenv("GOOGLE_CREDENTIALS") != "" {
		fmt.Fprintln(os.Stderr, "  gcp:         inline credentials (env)")
	} else if os.Getenv("CLOUDSDK_CONFIG") != "" {
		fmt.Fprintln(os.Stderr, "  gcp:         gcloud CLI (env)")
	}

	if os.Getenv("ARM_USE_OIDC") != "" || os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL") != "" {
		fmt.Fprintln(os.Stderr, "  azure:       OIDC federation (env)")
	} else if os.Getenv("ARM_USE_MSI") != "" {
		fmt.Fprintln(os.Stderr, "  azure:       managed identity (env)")
	} else if os.Getenv("ARM_USE_CLI") != "" {
		fmt.Fprintln(os.Stderr, "  azure:       CLI credentials (env)")
	} else if os.Getenv("ARM_CLIENT_ID") != "" {
		fmt.Fprintln(os.Stderr, "  azure:       service principal (env)")
	} else if os.Getenv("ARM_SUBSCRIPTION_ID") != "" {
		fmt.Fprintln(os.Stderr, "  azure:       subscription detected (env)")
	}

	fmt.Fprintln(os.Stderr)
}

func scanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan [path...]",
		Short: "Scan directories and show all detected Crossplane resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs := []string{"."}
			if len(args) > 0 {
				dirs = args
			}

			dir := dirs[0]
			rs, err := manifest.ScanWithKustomize(dirs)
			if err != nil {
				return err
			}

			total := len(rs.AllResources())
			fmt.Fprintf(os.Stdout, "Scanned: %s\n\n", dir)

			if total == 0 {
				fmt.Fprintln(os.Stdout, "No Crossplane resources found.")
				return nil
			}

			printSection := func(name string, resources []unstructured.Unstructured) {
				if len(resources) == 0 {
					return
				}
				fmt.Fprintf(os.Stdout, "%s (%d)\n", name, len(resources))
				for _, r := range resources {
					fmt.Fprintf(os.Stdout, "  %-40s %s\n", r.GetKind()+"/"+r.GetName(), r.GetAPIVersion())
				}
				fmt.Fprintln(os.Stdout)
			}

			printSection("XRDs", rs.XRDs)
			printSection("Compositions", rs.Compositions)
			printSection("Functions", rs.Functions)
			printSection("Claims", rs.Claims)
			printSection("Composite Resources (XRs)", rs.XRs)
			printSection("Managed Resources", rs.ManagedResources)
			printSection("Provider Configs", rs.ProviderConfigs)
			printSection("Other", rs.Other)

			fmt.Fprintf(os.Stdout, "Total: %d resources\n", total)
			return nil
		},
	}
}
