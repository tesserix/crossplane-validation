package renderer

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/tesserix/crossplane-validation/pkg/manifest"
)

func hasCrossplaneRender() bool {
	_, err := exec.LookPath("crossplane")
	return err == nil
}

func hasDocker() bool {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// CanUseCrossplaneRender checks if crossplane render is available with Docker.
func CanUseCrossplaneRender() bool {
	return hasCrossplaneRender() && hasDocker()
}

// crossplaneRender shells out to `crossplane render` for a single XR + Composition + Functions.
// Returns the rendered managed resources.
func crossplaneRender(xr, comp unstructured.Unstructured, functions []unstructured.Unstructured, extraResources []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	workDir, err := os.MkdirTemp("", "crossplane-render-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	xrPath := filepath.Join(workDir, "xr.yaml")
	compPath := filepath.Join(workDir, "composition.yaml")
	funcPath := filepath.Join(workDir, "functions.yaml")

	if err := writeYAML(xrPath, xr); err != nil {
		return nil, fmt.Errorf("writing XR: %w", err)
	}
	if err := writeYAML(compPath, comp); err != nil {
		return nil, fmt.Errorf("writing composition: %w", err)
	}
	if err := writeMultiYAML(funcPath, functions); err != nil {
		return nil, fmt.Errorf("writing functions: %w", err)
	}

	args := []string{"render", xrPath, compPath, funcPath, "--timeout=2m"}

	if len(extraResources) > 0 {
		extraPath := filepath.Join(workDir, "extra-resources.yaml")
		if err := writeMultiYAML(extraPath, extraResources); err != nil {
			return nil, fmt.Errorf("writing extra resources: %w", err)
		}
		args = append(args, "--extra-resources="+extraPath)
	}

	cmd := exec.Command("crossplane", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("crossplane render: %s: %w", stderr.String(), err)
	}

	return parseRenderedOutput(stdout.Bytes())
}

// renderWithCrossplane renders all compositions using `crossplane render`.
// It handles recursive compositions (XR → Composition → nested XR → another Composition).
func renderWithCrossplane(rs *manifest.ResourceSet) ([]RenderedResource, error) {
	var results []RenderedResource
	maxDepth := 5

	pending := make([]unstructured.Unstructured, 0)
	pending = append(pending, rs.XRs...)
	pending = append(pending, rs.Claims...)

	// Collect all extra resources (EnvironmentConfigs, etc.) from Other
	var extraResources []unstructured.Unstructured
	for _, o := range rs.Other {
		if o.GetKind() == "EnvironmentConfig" {
			extraResources = append(extraResources, o)
		}
	}

	for depth := 0; depth < maxDepth && len(pending) > 0; depth++ {
		var nextPending []unstructured.Unstructured
		fmt.Fprintf(os.Stderr, "  Render depth %d: %d composites to process\n", depth, len(pending))

		for _, xr := range pending {
			comp, err := findComposition(rs.Compositions, xr)
			if err != nil {
				results = append(results, RenderedResource{
					Source:   "unresolved-xr",
					Resource: xr,
				})
				continue
			}

			fmt.Fprintf(os.Stderr, "    Rendering %s/%s via %s\n", xr.GetKind(), xr.GetName(), comp.GetName())
			rendered, err := crossplaneRender(xr, comp, rs.Functions, extraResources)
			if err != nil {
				fmt.Fprintf(os.Stderr, "    warning: crossplane render failed for %s/%s: %v, falling back to built-in\n",
					xr.GetKind(), xr.GetName(), err)
				builtIn, _ := renderComposition(xr, comp, rs.Functions)
				for _, mr := range builtIn {
					results = append(results, RenderedResource{
						Source:         fmt.Sprintf("composition/%s", comp.GetName()),
						CompositionRef: comp.GetName(),
						Resource:       mr,
					})
				}
				continue
			}

			compName := comp.GetName()
			for _, mr := range rendered {
				// Skip the XR itself (status update echo)
				if mr.GetKind() == xr.GetKind() && mr.GetAPIVersion() == xr.GetAPIVersion() {
					continue
				}

				if isCompositeKind(mr, rs.Compositions) {
					fmt.Fprintf(os.Stderr, "    → nested XR: %s/%s (will render at depth %d)\n", mr.GetKind(), mr.GetName(), depth+1)
					nextPending = append(nextPending, mr)
					continue
				}

				results = append(results, RenderedResource{
					Source:         fmt.Sprintf("composition/%s", compName),
					CompositionRef: compName,
					Resource:       mr,
				})
			}
		}

		pending = nextPending
	}

	return results, nil
}

func isCompositeKind(obj unstructured.Unstructured, compositions []unstructured.Unstructured) bool {
	for _, comp := range compositions {
		typeRefAPIVersion, _, _ := unstructured.NestedString(comp.Object, "spec", "compositeTypeRef", "apiVersion")
		typeRefKind, _, _ := unstructured.NestedString(comp.Object, "spec", "compositeTypeRef", "kind")

		if obj.GetAPIVersion() == typeRefAPIVersion && obj.GetKind() == typeRefKind {
			return true
		}
	}
	return false
}

func parseRenderedOutput(data []byte) ([]unstructured.Unstructured, error) {
	var resources []unstructured.Unstructured
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := k8syaml.NewYAMLOrJSONDecoder(bytes.NewReader(doc), len(doc)).Decode(obj); err != nil {
			continue
		}

		if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
			continue
		}

		// Skip Result objects (function debug output)
		if obj.GetKind() == "Result" {
			continue
		}

		resources = append(resources, *obj)
	}

	return resources, nil
}

func writeYAML(path string, obj unstructured.Unstructured) error {
	data, err := sigyaml.Marshal(obj.Object)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func writeMultiYAML(path string, objs []unstructured.Unstructured) error {
	var buf bytes.Buffer
	for i, obj := range objs {
		if i > 0 {
			buf.WriteString("---\n")
		}
		data, err := sigyaml.Marshal(obj.Object)
		if err != nil {
			return err
		}
		buf.Write(data)
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}
