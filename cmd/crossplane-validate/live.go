package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/config"
	grpcclient "github.com/tesserix/crossplane-validation/pkg/grpc"
	"github.com/tesserix/crossplane-validation/pkg/hcl"
	"github.com/tesserix/crossplane-validation/pkg/k8s"
	"github.com/tesserix/crossplane-validation/pkg/operator"
	"github.com/tesserix/crossplane-validation/pkg/plan"
	"github.com/tesserix/crossplane-validation/pkg/renderer"
	"github.com/tesserix/crossplane-validation/pkg/tofu"
)

type liveOptions struct {
	operatorAddr  string
	apiToken      string
	kubeContext   string
	namespace     string
	manifestDirs  []string
	outputFmt     string
	showSensitive bool
}

func runLivePlan(opts liveOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	address, cleanup, err := resolveOperatorAddress(ctx, opts)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	fmt.Fprintf(os.Stderr, "Connecting to operator at %s...\n", address)

	useTLS := strings.HasSuffix(address, ":443") || strings.HasPrefix(address, "https://")
	cleanAddr := strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "http://")
	token := opts.apiToken
	if token == "" {
		token = os.Getenv("CROSSPLANE_VALIDATE_API_TOKEN")
	}
	client, err := grpcclient.Connect(ctx, grpcclient.ConnectOptions{
		Address:  cleanAddr,
		Timeout:  15 * time.Second,
		TLS:      useTLS,
		APIToken: token,
	})
	if err != nil {
		return fmt.Errorf("connecting to operator: %w", err)
	}
	defer client.Close()

	health, err := client.Health(ctx)
	if err != nil {
		return fmt.Errorf("operator health check failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Connected — %d resources cached, watching %s\n",
		health.CachedResources, strings.Join(health.WatchedGroups, ", "))

	manifestYAML, err := readManifestDirs(opts.manifestDirs)
	if err != nil {
		return fmt.Errorf("reading manifests: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Computing live plan...")

	liveResult, err := client.ComputePlan(ctx, manifestYAML, opts.showSensitive)
	if err != nil {
		return fmt.Errorf("computing plan: %w", err)
	}

	return renderLivePlan(liveResult, opts.outputFmt, os.Stdout)
}

func runLiveCloudPlan(opts liveOptions, providers map[string]config.Provider) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	address, cleanup, err := resolveOperatorAddress(ctx, opts)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	fmt.Fprintf(os.Stderr, "Connecting to operator at %s...\n", address)

	useTLS := strings.HasSuffix(address, ":443") || strings.HasPrefix(address, "https://")
	cleanAddr := strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "http://")
	token := opts.apiToken
	if token == "" {
		token = os.Getenv("CROSSPLANE_VALIDATE_API_TOKEN")
	}
	client, err := grpcclient.Connect(ctx, grpcclient.ConnectOptions{
		Address:  cleanAddr,
		Timeout:  15 * time.Second,
		TLS:      useTLS,
		APIToken: token,
	})
	if err != nil {
		return fmt.Errorf("connecting to operator: %w", err)
	}
	defer client.Close()

	manifestYAML, err := readManifestDirs(opts.manifestDirs)
	if err != nil {
		return fmt.Errorf("reading manifests: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Resolving compositions via operator...")

	resolveResp, err := client.ResolveResources(ctx, manifestYAML)
	if err != nil {
		return fmt.Errorf("resolving resources: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Resolved %d managed resources from compositions\n", resolveResp.Total)

	if resolveResp.Total == 0 {
		fmt.Fprintln(os.Stderr, "No managed resources found — Claims/XRs may not have been deployed yet")
		return nil
	}

	// Convert resolved resources to RenderedSet for HCL conversion
	rs := &renderer.RenderedSet{}
	for _, res := range resolveResp.Resources {
		var obj map[string]interface{}
		if err := json.Unmarshal(res.RawJSON, &obj); err != nil {
			continue
		}
		u := unstructured.Unstructured{Object: obj}
		rs.Resources = append(rs.Resources, renderer.RenderedResource{
			Source:   "live-cluster",
			Resource: u,
		})
	}

	fmt.Fprintln(os.Stderr, "Converting to HCL...")
	hcl.UseSchemaLookup = true
	targetHCL, err := hcl.Convert(rs, providers)
	if err != nil {
		return fmt.Errorf("converting to HCL: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Generated %d Terraform resource blocks\n", len(targetHCL.ResourceBlocks))

	fmt.Fprintln(os.Stderr, "Running cloud plan (read-only)...")
	cloudPlan, err := tofu.Plan(nil, targetHCL, providers)
	if err != nil {
		return fmt.Errorf("running cloud plan: %w", err)
	}

	result := &plan.Result{
		CloudPlan: cloudPlan,
	}

	return plan.Render(result, opts.outputFmt, os.Stdout)
}

func runLiveDrift(opts liveOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	address, cleanup, err := resolveOperatorAddress(ctx, opts)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	useTLS := strings.HasSuffix(address, ":443") || strings.HasPrefix(address, "https://")
	cleanAddr := strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "http://")
	token := opts.apiToken
	if token == "" {
		token = os.Getenv("CROSSPLANE_VALIDATE_API_TOKEN")
	}
	client, err := grpcclient.Connect(ctx, grpcclient.ConnectOptions{
		Address:  cleanAddr,
		Timeout:  15 * time.Second,
		TLS:      useTLS,
		APIToken: token,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	manifestYAML, err := readManifestDirs(opts.manifestDirs)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "Computing drift...")

	driftResult, err := client.GetDrift(ctx, manifestYAML)
	if err != nil {
		return err
	}

	return renderDrift(driftResult, os.Stdout)
}

func runLiveStatus(opts liveOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	address, cleanup, err := resolveOperatorAddress(ctx, opts)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	useTLS := strings.HasSuffix(address, ":443") || strings.HasPrefix(address, "https://")
	cleanAddr := strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "http://")
	token := opts.apiToken
	if token == "" {
		token = os.Getenv("CROSSPLANE_VALIDATE_API_TOKEN")
	}
	client, err := grpcclient.Connect(ctx, grpcclient.ConnectOptions{
		Address:  cleanAddr,
		Timeout:  15 * time.Second,
		TLS:      useTLS,
		APIToken: token,
	})
	if err != nil {
		return err
	}
	defer client.Close()

	state, err := client.GetClusterState(ctx, "", "", "")
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Cluster Resources: %d total\n\n", state.Total)

	byKind := map[string]int{}
	readyCount := 0
	for _, r := range state.Resources {
		byKind[r.Kind]++
		if r.Status != nil && r.Status.Ready {
			readyCount++
		}
	}

	for kind, count := range byKind {
		fmt.Fprintf(os.Stdout, "  %-40s %d\n", kind, count)
	}

	fmt.Fprintf(os.Stdout, "\nReady: %d / %d\n", readyCount, state.Total)
	return nil
}

func resolveOperatorAddress(ctx context.Context, opts liveOptions) (string, func(), error) {
	if opts.operatorAddr != "" {
		return opts.operatorAddr, nil, nil
	}

	fmt.Fprintln(os.Stderr, "Port-forwarding to operator...")

	result, err := k8s.PortForward(ctx, k8s.PortForwardOptions{
		Namespace:   opts.namespace,
		ServiceName: "crossplane-validate-operator",
		ServicePort: 9443,
		KubeContext: opts.kubeContext,
	})
	if err != nil {
		return "", nil, fmt.Errorf("port-forward failed: %w\nTip: use --operator-address to connect directly", err)
	}

	cleanup := func() { result.Stop() }
	return result.Address(), cleanup, nil
}

func readManifestDirs(dirs []string) ([]byte, error) {
	var allYAML []byte

	for _, dir := range dirs {
		err := walkYAMLFiles(dir, func(data []byte) {
			allYAML = append(allYAML, []byte("---\n")...)
			allYAML = append(allYAML, data...)
			allYAML = append(allYAML, '\n')
		})
		if err != nil {
			return nil, err
		}
	}

	return allYAML, nil
}

func walkYAMLFiles(dir string, fn func([]byte)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		path := dir + "/" + entry.Name()
		if entry.IsDir() {
			if err := walkYAMLFiles(path, fn); err != nil {
				return err
			}
			continue
		}

		ext := strings.ToLower(entry.Name())
		if !strings.HasSuffix(ext, ".yaml") && !strings.HasSuffix(ext, ".yml") {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(data)
	}

	return nil
}

func renderLivePlan(result *grpcclient.LivePlanResult, format string, w *os.File) error {
	if result.ClusterInfo != nil {
		switch format {
		case "markdown":
			fmt.Fprintf(w, "### Crossplane Validation (Live)\n\n")
			fmt.Fprintf(w, "Connected to cluster | Resources cached: %d | Cache age: %s\n\n",
				result.ClusterInfo.CachedResources, result.ClusterInfo.CacheAge)
		case "json":
			// included in json output
		default:
			fmt.Fprintf(w, "Connected to operator | Resources cached: %d | Cache age: %s\n\n",
				result.ClusterInfo.CachedResources, result.ClusterInfo.CacheAge)
		}
	}

	if len(result.DriftWarnings) > 0 {
		renderDriftWarnings(w, result.DriftWarnings, format)
	}

	if err := plan.Render(result.Plan, format, w); err != nil {
		return err
	}

	if len(result.ComposedChanges) > 0 {
		renderComposedChanges(w, result.ComposedChanges, format)
	}

	if len(result.ResourceTree) > 0 {
		renderResourceTree(w, result.ResourceTree, format)
	}

	return nil
}

func renderComposedChanges(w *os.File, changes []grpcclient.ComposedResourceChange, format string) {
	yellow := "\033[33m"
	cyan := "\033[36m"
	red := "\033[31m"
	green := "\033[32m"
	reset := "\033[0m"
	dim := "\033[2m"

	if format == "markdown" {
		return
	}

	fmt.Fprintf(w, "\n%s═══ Composed Resource Impact ═══%s\n\n", cyan, reset)

	for _, cc := range changes {
		indent := strings.Repeat("  ", cc.Depth)
		fmt.Fprintf(w, "%s%s~ %s/%s%s", indent, yellow, cc.ResourceKind, cc.ResourceName, reset)
		if cc.CompositionStep != "" {
			fmt.Fprintf(w, " %s(via %s)%s", dim, cc.CompositionStep, reset)
		}
		fmt.Fprintln(w)

		for _, f := range cc.FieldChanges {
			fmt.Fprintf(w, "%s    %s- %s: %s%s\n", indent, red, f.Path, f.OldValue, reset)
			fmt.Fprintf(w, "%s    %s+ %s: %s%s\n", indent, green, f.Path, f.NewValue, reset)
		}
	}
}

func renderResourceTree(w *os.File, trees []grpcclient.ResourceTreeNode, format string) {
	cyan := "\033[36m"
	dim := "\033[2m"
	reset := "\033[0m"

	if format == "markdown" {
		return
	}

	fmt.Fprintf(w, "\n%s═══ Resource Tree ═══%s\n\n", cyan, reset)
	for _, tree := range trees {
		printTree(w, tree, "", true, dim, reset)
	}
	fmt.Fprintln(w)
}

func printTree(w *os.File, node grpcclient.ResourceTreeNode, prefix string, isLast bool, dim, reset string) {
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}

	fmt.Fprintf(w, "%s%s%s/%s %s%s%s\n", prefix, connector, node.Kind, node.Name, dim, node.APIVersion, reset)

	childPrefix := prefix
	if prefix != "" {
		if isLast {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}

	for i, child := range node.Children {
		printTree(w, child, childPrefix, i == len(node.Children)-1, dim, reset)
	}
}

func renderDriftWarnings(w *os.File, warnings []operator.DriftWarning, format string) {
	switch format {
	case "markdown":
		fmt.Fprintln(w, "#### Drift Detected")
		fmt.Fprintln(w, "| Resource | Message |")
		fmt.Fprintln(w, "|----------|---------|")
		for _, warn := range warnings {
			fmt.Fprintf(w, "| %s | %s |\n", warn.ResourceKey, warn.Message)
		}
		fmt.Fprintln(w)
	default:
		fmt.Fprintln(w, "\033[33m⚠ DRIFT DETECTED\033[0m")
		for _, warn := range warnings {
			fmt.Fprintf(w, "  \033[33m~ %s\033[0m\n", warn.Message)
		}
		fmt.Fprintln(w)
	}
}

func renderDrift(result *grpcclient.LiveDriftResult, w *os.File) error {
	if result.Summary == nil || result.Summary.Total == 0 {
		fmt.Fprintln(w, "No drift detected. Cluster state matches git manifests.")
		return nil
	}

	fmt.Fprintf(w, "Drift Summary: %d total\n", result.Summary.Total)
	fmt.Fprintf(w, "  Missing in cluster: %d\n", result.Summary.MissingInCluster)
	fmt.Fprintf(w, "  Missing in git:     %d\n", result.Summary.MissingInGit)
	fmt.Fprintf(w, "  Spec drift:         %d\n\n", result.Summary.SpecDrift)

	for _, d := range result.Drifts {
		symbol := "~"
		switch d.DriftType {
		case operator.DriftMissingInCluster:
			symbol = "+"
		case operator.DriftMissingInGit:
			symbol = "-"
		}
		fmt.Fprintf(w, "  %s %s/%s  (%s)\n", symbol, d.Kind, d.Name, d.DriftType)
	}

	return nil
}
