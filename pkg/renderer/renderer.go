package renderer

import (
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	sigyaml "sigs.k8s.io/yaml"

	"github.com/tesserix/crossplane-validation/pkg/manifest"
)

// RenderedResource represents a managed resource that would be created/managed
// by Crossplane after composition rendering.
type RenderedResource struct {
	// Source is where this resource came from (composition name, direct MR, etc.)
	Source string
	// CompositionRef is the composition that produced this resource (empty for direct MRs).
	CompositionRef string
	// Resource is the full unstructured resource.
	Resource unstructured.Unstructured
}

// RenderedSet holds all rendered managed resources.
type RenderedSet struct {
	Resources []RenderedResource
}

// Render produces the managed resources that Crossplane would create from a ResourceSet.
// It automatically selects the best rendering strategy:
//   - If Docker + crossplane CLI are available: uses `crossplane render` (full fidelity,
//     supports all composition functions including go-templating)
//   - Otherwise: uses built-in renderer (Patch-and-Transform only, best effort for pipelines)
func Render(rs *manifest.ResourceSet) (*RenderedSet, error) {
	rendered := &RenderedSet{}

	for _, mr := range rs.ManagedResources {
		rendered.Resources = append(rendered.Resources, RenderedResource{
			Source:   "direct",
			Resource: mr,
		})
	}

	hasComposites := len(rs.XRs) > 0 || len(rs.Claims) > 0
	if !hasComposites {
		return rendered, nil
	}

	useCrossplane := CanUseCrossplaneRender() && len(rs.Functions) > 0
	if useCrossplane {
		fmt.Println("  Using crossplane render (Docker + Functions detected)")
		compositeResults, err := renderWithCrossplane(rs)
		if err != nil {
			return nil, err
		}
		rendered.Resources = append(rendered.Resources, compositeResults...)
	} else {
		if !CanUseCrossplaneRender() && len(rs.Functions) > 0 {
			fmt.Println("  Docker or crossplane CLI not available — using built-in renderer (limited)")
		}
		composites := append(rs.XRs, rs.Claims...)
		for _, xr := range composites {
			comp, err := findComposition(rs.Compositions, xr)
			if err != nil {
				rendered.Resources = append(rendered.Resources, RenderedResource{
					Source:   "unresolved-xr",
					Resource: xr,
				})
				continue
			}

			mrs, err := renderComposition(xr, comp, rs.Functions)
			if err != nil {
				return nil, fmt.Errorf("rendering composition %s for %s/%s: %w",
					comp.GetName(), xr.GetKind(), xr.GetName(), err)
			}

			compName := comp.GetName()
			for _, mr := range mrs {
				rendered.Resources = append(rendered.Resources, RenderedResource{
					Source:         fmt.Sprintf("composition/%s", compName),
					CompositionRef: compName,
					Resource:       mr,
				})
			}
		}
	}

	return rendered, nil
}

// ResourceKey returns a unique identifier for a rendered resource.
// Format: apiVersion/kind/namespace/name (namespace is empty for cluster-scoped resources).
func (r *RenderedResource) ResourceKey() string {
	res := r.Resource
	return fmt.Sprintf("%s/%s/%s/%s",
		res.GetAPIVersion(),
		res.GetKind(),
		res.GetNamespace(),
		res.GetName(),
	)
}

// Print writes all rendered resources as YAML to the writer.
func Print(rs *RenderedSet, w io.Writer) error {
	for i, r := range rs.Resources {
		if i > 0 {
			fmt.Fprintln(w, "---")
		}

		data, err := sigyaml.Marshal(r.Resource.Object)
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "# Source: %s\n", r.Source)
		fmt.Fprint(w, string(data))
	}
	return nil
}

// findComposition finds a composition that matches the given XR/Claim.
func findComposition(compositions []unstructured.Unstructured, xr unstructured.Unstructured) (unstructured.Unstructured, error) {
	xrAPIVersion := xr.GetAPIVersion()
	xrKind := xr.GetKind()

	// Check for explicit compositionRef
	if ref, found, _ := unstructured.NestedString(xr.Object, "spec", "compositionRef", "name"); found && ref != "" {
		for _, comp := range compositions {
			if comp.GetName() == ref {
				return comp, nil
			}
		}
	}

	// Match by compositeTypeRef
	for _, comp := range compositions {
		typeRefAPIVersion, _, _ := unstructured.NestedString(comp.Object, "spec", "compositeTypeRef", "apiVersion")
		typeRefKind, _, _ := unstructured.NestedString(comp.Object, "spec", "compositeTypeRef", "kind")

		if typeRefAPIVersion == xrAPIVersion && typeRefKind == xrKind {
			return comp, nil
		}

		// For Claims, try matching the composite kind (strip "Claim" suffix)
		if xrKind+"Claim" == xrKind {
			continue
		}
	}

	return unstructured.Unstructured{}, fmt.Errorf("no matching composition found for %s/%s", xrAPIVersion, xrKind)
}

// renderComposition renders a composition for a given XR, producing managed resources.
// This implements offline composition rendering using Crossplane's patch-and-transform
// and function pipeline modes.
func renderComposition(xr, comp unstructured.Unstructured, functions []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	mode, _, _ := unstructured.NestedString(comp.Object, "spec", "mode")

	switch mode {
	case "Pipeline":
		return renderPipelineComposition(xr, comp, functions)
	default:
		// Patch-and-Transform (default mode)
		return renderPatchAndTransform(xr, comp)
	}
}

// renderPatchAndTransform renders a classic Patch-and-Transform composition.
func renderPatchAndTransform(xr, comp unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	resources, found, err := unstructured.NestedSlice(comp.Object, "spec", "resources")
	if err != nil || !found {
		return nil, fmt.Errorf("no resources found in composition spec")
	}

	var rendered []unstructured.Unstructured
	for _, res := range resources {
		resMap, ok := res.(map[string]interface{})
		if !ok {
			continue
		}

		base, found, err := unstructured.NestedMap(resMap, "base")
		if err != nil || !found {
			continue
		}

		mr := unstructured.Unstructured{Object: deepCopyMap(base)}

		// Apply name from XR if not set
		if mr.GetName() == "" {
			name, _, _ := unstructured.NestedString(resMap, "name")
			if name == "" {
				name = xr.GetName()
			}
			mr.SetName(fmt.Sprintf("%s-%s", xr.GetName(), name))
		}

		// Apply patches from XR to MR
		patches, _, _ := unstructured.NestedSlice(resMap, "patches")
		applyPatches(&mr, xr, patches)

		rendered = append(rendered, mr)
	}

	return rendered, nil
}

// renderPipelineComposition handles Function Pipeline compositions.
// For offline rendering, we extract the input resources from pipeline steps.
func renderPipelineComposition(xr, comp unstructured.Unstructured, functions []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	pipeline, found, err := unstructured.NestedSlice(comp.Object, "spec", "pipeline")
	if err != nil || !found {
		return nil, fmt.Errorf("no pipeline found in composition spec")
	}

	var rendered []unstructured.Unstructured
	for _, step := range pipeline {
		stepMap, ok := step.(map[string]interface{})
		if !ok {
			continue
		}

		// Extract resources from function input
		input, found, _ := unstructured.NestedMap(stepMap, "input")
		if !found {
			continue
		}

		resources, found, _ := unstructured.NestedSlice(input, "resources")
		if !found {
			continue
		}

		for _, res := range resources {
			resMap, ok := res.(map[string]interface{})
			if !ok {
				continue
			}

			base, found, _ := unstructured.NestedMap(resMap, "base")
			if !found {
				continue
			}

			mr := unstructured.Unstructured{Object: deepCopyMap(base)}
			if mr.GetName() == "" {
				name, _, _ := unstructured.NestedString(resMap, "name")
				mr.SetName(fmt.Sprintf("%s-%s", xr.GetName(), name))
			}

			patches, _, _ := unstructured.NestedSlice(resMap, "patches")
			applyPatches(&mr, xr, patches)

			rendered = append(rendered, mr)
		}
	}

	return rendered, nil
}

// applyPatches applies Crossplane patches from XR to the managed resource.
func applyPatches(mr *unstructured.Unstructured, xr unstructured.Unstructured, patches []interface{}) {
	for _, patch := range patches {
		p, ok := patch.(map[string]interface{})
		if !ok {
			continue
		}

		patchType, _, _ := unstructured.NestedString(p, "type")
		if patchType == "" {
			patchType = "FromCompositeFieldPath"
		}

		switch patchType {
		case "FromCompositeFieldPath":
			fromField, _, _ := unstructured.NestedString(p, "fromFieldPath")
			toField, _, _ := unstructured.NestedString(p, "toFieldPath")
			if fromField == "" || toField == "" {
				continue
			}

			val, found, _ := unstructured.NestedFieldNoCopy(xr.Object, splitFieldPath(fromField)...)
			if found && val != nil {
				unstructured.SetNestedField(mr.Object, deepCopyValue(val), splitFieldPath(toField)...)
			}
		}
	}
}

func splitFieldPath(path string) []string {
	// Simple field path split — handles dot notation
	parts := []string{}
	for _, p := range splitDotPath(path) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func splitDotPath(path string) []string {
	result := []string{}
	current := ""
	for _, c := range path {
		if c == '.' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = deepCopyValue(v)
	}
	return result
}

func deepCopyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(val)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = deepCopyValue(item)
		}
		return result
	default:
		return v
	}
}
