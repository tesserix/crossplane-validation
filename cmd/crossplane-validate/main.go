package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tesserix/crossplane-validation/pkg/config"
	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/hcl"
	"github.com/tesserix/crossplane-validation/pkg/manifest"
	"github.com/tesserix/crossplane-validation/pkg/plan"
	"github.com/tesserix/crossplane-validation/pkg/renderer"
	"github.com/tesserix/crossplane-validation/pkg/tofu"
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

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func planCmd() *cobra.Command {
	var (
		baseBranch  string
		targetRef   string
		configFile  string
		outputFmt   string
		manifestDir string
		cloudMode   bool
	)

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what will change when the current branch is merged",
		Long: `Compares Crossplane manifests between branches and shows a terraform-style plan.

Mode 1 (default): Git-based diff — compares rendered manifests between base and target branches.
Mode 3 (--cloud): Cloud-aware plan — converts to HCL and runs OpenTofu plan with read-only credentials.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configFile)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if manifestDir != "" {
				cfg.ManifestDirs = []string{manifestDir}
			}

			fmt.Println("Scanning manifests...")
			baseManifests, err := manifest.ScanFromGitRef(cfg.ManifestDirs, baseBranch)
			if err != nil {
				return fmt.Errorf("scanning base branch %s: %w", baseBranch, err)
			}

			targetManifests, err := manifest.ScanFromGitRef(cfg.ManifestDirs, targetRef)
			if err != nil {
				return fmt.Errorf("scanning target ref %s: %w", targetRef, err)
			}

			fmt.Println("Rendering compositions...")
			baseRendered, err := renderer.Render(baseManifests)
			if err != nil {
				return fmt.Errorf("rendering base: %w", err)
			}

			targetRendered, err := renderer.Render(targetManifests)
			if err != nil {
				return fmt.Errorf("rendering target: %w", err)
			}

			fmt.Println("Computing structural diff...")
			structDiff := diff.Compute(baseRendered, targetRendered)

			var cloudPlan *tofu.PlanResult
			if cloudMode && cfg.HasCloudCredentials() {
				fmt.Println("Converting to HCL...")
				baseHCL, err := hcl.Convert(baseRendered, cfg.Providers)
				if err != nil {
					return fmt.Errorf("converting base to HCL: %w", err)
				}
				targetHCL, err := hcl.Convert(targetRendered, cfg.Providers)
				if err != nil {
					return fmt.Errorf("converting target to HCL: %w", err)
				}

				fmt.Println("Running cloud plan (read-only)...")
				cloudPlan, err = tofu.Plan(baseHCL, targetHCL, cfg.Providers)
				if err != nil {
					return fmt.Errorf("running cloud plan: %w", err)
				}
			}

			result := &plan.Result{
				StructuralDiff: structDiff,
				CloudPlan:      cloudPlan,
			}

			return plan.Render(result, outputFmt, os.Stdout)
		},
	}

	cmd.Flags().StringVar(&baseBranch, "base", "main", "Base branch to compare against")
	cmd.Flags().StringVar(&targetRef, "target", "HEAD", "Target ref (branch/commit) to validate")
	cmd.Flags().StringVarP(&configFile, "config", "c", ".crossplane-validate.yml", "Config file path")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "terminal", "Output format: terminal, markdown, json")
	cmd.Flags().StringVarP(&manifestDir, "manifests", "m", "", "Manifest directory (overrides config)")
	cmd.Flags().BoolVar(&cloudMode, "cloud", false, "Enable cloud-aware plan using OpenTofu (requires credentials)")

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
	return &cobra.Command{
		Use:   "render [path]",
		Short: "Render compositions and show resulting managed resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			manifests, err := manifest.Scan([]string{dir})
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
}
