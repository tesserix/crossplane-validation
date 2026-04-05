package operator

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ComposedChange represents a predicted change to a composed resource.
type ComposedChange struct {
	ResourceKind    string                `json:"resourceKind"`
	ResourceName    string                `json:"resourceName"`
	APIVersion      string                `json:"apiVersion"`
	CompositionStep string                `json:"compositionStep"`
	FieldChanges    []ComposedFieldChange `json:"fieldChanges"`
	Depth           int                   `json:"depth"`
}

// ComposedFieldChange represents a single predicted field change in a composed resource.
type ComposedFieldChange struct {
	Path     string `json:"path"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
}

// ResourceTreeNode represents a node in the Crossplane resource hierarchy.
type ResourceTreeNode struct {
	Kind       string              `json:"kind"`
	Name       string              `json:"name"`
	Namespace  string              `json:"namespace,omitempty"`
	APIVersion string              `json:"apiVersion"`
	Children   []*ResourceTreeNode `json:"children,omitempty"`
}

// CompositionPatch represents a patch from a Composition.
type CompositionPatch struct {
	Type          string `json:"type"`
	FromFieldPath string `json:"fromFieldPath"`
	ToFieldPath   string `json:"toFieldPath"`
}

// ResolveComposedChanges traces how proposed changes to a Claim/XR propagate
// through the Composition pipeline to composed and managed resources.
func ResolveComposedChanges(cache *StateCache, proposed *unstructured.Unstructured, changedFields map[string]ComposedFieldChange) ([]ComposedChange, *ResourceTreeNode) {
	apiVersion := proposed.GetAPIVersion()
	kind := proposed.GetKind()
	name := proposed.GetName()
	ns := proposed.GetNamespace()

	root := &ResourceTreeNode{
		Kind:       kind,
		Name:       name,
		Namespace:  ns,
		APIVersion: apiVersion,
	}

	var allChanges []ComposedChange

	// Step 1: If this is a Claim, find the XR it owns
	xr := findOwnedXR(cache, proposed)
	if xr == nil {
		// This might be an XR directly — check if it has resourceRefs
		xr = proposed
		xrLive := findLiveResource(cache, apiVersion, kind, "", name)
		if xrLive != nil {
			xr = &unstructured.Unstructured{Object: deepCopyMap(xrLive.Object)}
		}
	}

	if xr == nil {
		return allChanges, root
	}

	// Step 2: Find the Composition for this XR
	comp := findComposition(cache, xr)
	if comp == nil {
		return allChanges, root
	}

	// Step 3: Trace patches through the Composition pipeline
	composedChanges := tracePatchPipeline(cache, comp, proposed, changedFields, 1)
	allChanges = append(allChanges, composedChanges...)

	// Step 4: Build the resource tree from live state
	buildResourceTree(cache, xr, root, 0)

	// Step 5: For each composed XR, recursively resolve its own Composition
	for _, change := range composedChanges {
		childChangedFields := make(map[string]ComposedFieldChange)
		for _, fc := range change.FieldChanges {
			childChangedFields[fc.Path] = ComposedFieldChange{Path: fc.Path, OldValue: fc.OldValue, NewValue: fc.NewValue}
		}
		if len(childChangedFields) > 0 {
			childXR := findLiveResource(cache, change.APIVersion, change.ResourceKind, "", change.ResourceName)
			if childXR != nil {
				childComp := findComposition(cache, childXR)
				if childComp != nil {
					deeperChanges := tracePatchPipeline(cache, childComp, childXR, childChangedFields, 2)
					allChanges = append(allChanges, deeperChanges...)
				}
			}
		}
	}

	return allChanges, root
}

// findOwnedXR finds the XR owned by a Claim by looking at the Claim's spec.resourceRef.
func findOwnedXR(cache *StateCache, claim *unstructured.Unstructured) *unstructured.Unstructured {
	// Check live claim for resourceRef
	liveClaim := findLiveResource(cache, claim.GetAPIVersion(), claim.GetKind(), claim.GetNamespace(), claim.GetName())
	if liveClaim == nil {
		return nil
	}

	ref, found, _ := unstructured.NestedMap(liveClaim.Object, "spec", "resourceRef")
	if !found {
		return nil
	}

	refKind, _ := ref["kind"].(string)
	refName, _ := ref["name"].(string)
	refAPIVersion, _ := ref["apiVersion"].(string)

	if refName == "" || refKind == "" {
		return nil
	}

	return findLiveResource(cache, refAPIVersion, refKind, "", refName)
}

// findComposition finds the Composition that matches a given XR.
func findComposition(cache *StateCache, xr *unstructured.Unstructured) *unstructured.Unstructured {
	// First check compositionRef on the XR
	compRef, found, _ := unstructured.NestedString(xr.Object, "spec", "compositionRef", "name")
	if found && compRef != "" {
		return findLiveResource(cache, "apiextensions.crossplane.io/v1", "Composition", "", compRef)
	}

	// Fallback: search all Compositions by compositeTypeRef
	apiVersion := xr.GetAPIVersion()
	kind := xr.GetKind()
	// If this is a Claim (namespaced), the XR kind is usually "X" + ClaimKind
	if xr.GetNamespace() != "" {
		kind = "X" + kind
	}

	cache.mu.RLock()
	defer cache.mu.RUnlock()

	for _, res := range cache.resources {
		if res.GetKind() != "Composition" {
			continue
		}
		typeRef, found, _ := unstructured.NestedMap(res.Object, "spec", "compositeTypeRef")
		if !found {
			continue
		}
		refAPI, _ := typeRef["apiVersion"].(string)
		refKind, _ := typeRef["kind"].(string)
		if refAPI == apiVersion && refKind == kind {
			return res
		}
	}
	return nil
}

// tracePatchPipeline processes a Composition's pipeline steps and traces how
// changed fields propagate to composed resources.
func tracePatchPipeline(cache *StateCache, comp *unstructured.Unstructured, source *unstructured.Unstructured, changedFields map[string]ComposedFieldChange, depth int) []ComposedChange {
	var results []ComposedChange

	mode, _, _ := unstructured.NestedString(comp.Object, "spec", "mode")

	if mode == "Pipeline" {
		results = tracePipelineMode(cache, comp, source, changedFields, depth)
	} else {
		results = traceResourcesMode(cache, comp, source, changedFields, depth)
	}

	return results
}

// tracePipelineMode handles Compositions with mode: Pipeline
func tracePipelineMode(cache *StateCache, comp *unstructured.Unstructured, source *unstructured.Unstructured, changedFields map[string]ComposedFieldChange, depth int) []ComposedChange {
	var results []ComposedChange

	pipeline, found, _ := unstructured.NestedSlice(comp.Object, "spec", "pipeline")
	if !found {
		return results
	}

	for _, stepRaw := range pipeline {
		step, ok := stepRaw.(map[string]interface{})
		if !ok {
			continue
		}

		stepName, _ := step["step"].(string)
		input, ok := step["input"].(map[string]interface{})
		if !ok {
			continue
		}

		resources, ok := input["resources"].([]interface{})
		if !ok {
			continue
		}

		for _, resRaw := range resources {
			res, ok := resRaw.(map[string]interface{})
			if !ok {
				continue
			}

			change := traceResourcePatches(cache, res, source, changedFields, stepName, depth)
			if change != nil {
				results = append(results, *change)
			}
		}
	}

	return results
}

// traceResourcesMode handles Compositions with mode: Resources (legacy)
func traceResourcesMode(cache *StateCache, comp *unstructured.Unstructured, source *unstructured.Unstructured, changedFields map[string]ComposedFieldChange, depth int) []ComposedChange {
	var results []ComposedChange

	resources, found, _ := unstructured.NestedSlice(comp.Object, "spec", "resources")
	if !found {
		return results
	}

	for _, resRaw := range resources {
		res, ok := resRaw.(map[string]interface{})
		if !ok {
			continue
		}

		change := traceResourcePatches(cache, res, source, changedFields, "", depth)
		if change != nil {
			results = append(results, *change)
		}
	}

	return results
}

// traceResourcePatches checks if any patches in a composed resource template
// reference the changed fields, and predicts what the composed resource changes would be.
func traceResourcePatches(cache *StateCache, res map[string]interface{}, source *unstructured.Unstructured, changedFields map[string]ComposedFieldChange, stepName string, depth int) *ComposedChange {
	resName, _ := res["name"].(string)
	base, _ := res["base"].(map[string]interface{})
	if base == nil {
		return nil
	}

	baseKind, _ := base["kind"].(string)
	baseAPIVersion, _ := base["apiVersion"].(string)

	patches, _ := res["patches"].([]interface{})

	var fieldChanges []ComposedFieldChange
	for _, patchRaw := range patches {
		patch, ok := patchRaw.(map[string]interface{})
		if !ok {
			continue
		}

		patchType, _ := patch["type"].(string)
		if patchType == "" {
			patchType = "FromCompositeFieldPath"
		}

		if patchType != "FromCompositeFieldPath" {
			continue
		}

		fromPath, _ := patch["fromFieldPath"].(string)
		toPath, _ := patch["toFieldPath"].(string)

		if fromPath == "" || toPath == "" {
			continue
		}

		// Check if this patch references a changed field
		for changedPath, change := range changedFields {
			if matchesFieldPath(changedPath, fromPath) {
				// Find live value of the target field on the composed resource
				liveComposed := findComposedResource(cache, source, baseAPIVersion, baseKind, resName)
				oldValue := change.OldValue
				if liveComposed != nil {
					if v, found := getNestedField(liveComposed.Object, toPath); found {
						oldValue = fmt.Sprintf("%v", v)
					}
				}

				fieldChanges = append(fieldChanges, ComposedFieldChange{
					Path:     toPath,
					OldValue: oldValue,
					NewValue: change.NewValue,
				})
			}
		}
	}

	if len(fieldChanges) == 0 {
		return nil
	}

	// Find the actual composed resource name from the live XR's resourceRefs
	actualName := resName
	liveComposed := findComposedResource(cache, source, baseAPIVersion, baseKind, resName)
	if liveComposed != nil {
		actualName = liveComposed.GetName()
	}

	return &ComposedChange{
		ResourceKind:    baseKind,
		ResourceName:    actualName,
		APIVersion:      baseAPIVersion,
		CompositionStep: stepName,
		FieldChanges:    fieldChanges,
		Depth:           depth,
	}
}

// findComposedResource finds a live composed resource by looking at the parent XR's resourceRefs.
func findComposedResource(cache *StateCache, parent *unstructured.Unstructured, targetAPIVersion, targetKind, templateName string) *unstructured.Unstructured {
	// Get resourceRefs from the live version of the parent
	liveParent := findLiveResource(cache, parent.GetAPIVersion(), parent.GetKind(), parent.GetNamespace(), parent.GetName())
	if liveParent == nil {
		return nil
	}

	// Check spec.resourceRefs (XR pattern) or spec.crossplane.resourceRefs
	refs, found, _ := unstructured.NestedSlice(liveParent.Object, "spec", "resourceRefs")
	if !found {
		refs, found, _ = unstructured.NestedSlice(liveParent.Object, "spec", "crossplane", "resourceRefs")
	}
	if !found {
		return nil
	}

	for _, refRaw := range refs {
		ref, ok := refRaw.(map[string]interface{})
		if !ok {
			continue
		}
		refKind, _ := ref["kind"].(string)
		refName, _ := ref["name"].(string)
		refAPIVersion, _ := ref["apiVersion"].(string)

		if refKind == targetKind || (refAPIVersion != "" && strings.HasPrefix(refAPIVersion, strings.Split(targetAPIVersion, "/")[0])) {
			res := findLiveResource(cache, refAPIVersion, refKind, "", refName)
			if res != nil {
				return res
			}
		}
	}

	return nil
}

// findLiveResource finds a resource in the cache by its identity.
func findLiveResource(cache *StateCache, apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	key := resourceKey(apiVersion, kind, namespace, name)
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	if res, ok := cache.resources[key]; ok {
		return res
	}
	return nil
}

// buildResourceTree recursively builds the resource tree from live state.
func buildResourceTree(cache *StateCache, xr *unstructured.Unstructured, node *ResourceTreeNode, depth int) {
	if depth > 4 {
		return
	}

	// Look at live XR's resourceRefs
	liveXR := findLiveResource(cache, xr.GetAPIVersion(), xr.GetKind(), xr.GetNamespace(), xr.GetName())
	if liveXR == nil {
		return
	}

	refs, found, _ := unstructured.NestedSlice(liveXR.Object, "spec", "resourceRefs")
	if !found {
		refs, _, _ = unstructured.NestedSlice(liveXR.Object, "spec", "crossplane", "resourceRefs")
	}

	for _, refRaw := range refs {
		ref, ok := refRaw.(map[string]interface{})
		if !ok {
			continue
		}
		refKind, _ := ref["kind"].(string)
		refName, _ := ref["name"].(string)
		refAPIVersion, _ := ref["apiVersion"].(string)

		child := &ResourceTreeNode{
			Kind:       refKind,
			Name:       refName,
			APIVersion: refAPIVersion,
		}
		node.Children = append(node.Children, child)

		childRes := findLiveResource(cache, refAPIVersion, refKind, "", refName)
		if childRes != nil {
			buildResourceTree(cache, childRes, child, depth+1)
		}
	}
}

// matchesFieldPath checks if a changed field path matches a patch's fromFieldPath.
func matchesFieldPath(changedPath, fromPath string) bool {
	return changedPath == fromPath || strings.HasPrefix(changedPath, fromPath+".") || strings.HasPrefix(fromPath, changedPath+".")
}

// getNestedField retrieves a value from a nested map using a dot-separated path.
func getNestedField(obj map[string]interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	current := obj
	for i, part := range parts {
		val, ok := current[part]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return val, true
		}
		next, ok := val.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current = next
	}
	return nil, false
}

// ResolveAllManagedResources returns all managed resources that a given Claim/XR
// resolves to through the composition chain. This walks the resource tree from the
// cache and collects all leaf managed resources (*.upbound.io, *.crossplane.io types).
func ResolveAllManagedResources(cache *StateCache, proposed *unstructured.Unstructured) []unstructured.Unstructured {
	var result []unstructured.Unstructured

	// Find the XR owned by this Claim
	xr := findOwnedXR(cache, proposed)
	if xr == nil {
		xr = proposed
		xrLive := findLiveResource(cache, proposed.GetAPIVersion(), proposed.GetKind(), "", proposed.GetName())
		if xrLive != nil {
			xr = xrLive
		}
	}

	collectManagedResources(cache, xr, &result, 0)
	return result
}

// collectManagedResources recursively walks the resource tree and collects managed resources.
func collectManagedResources(cache *StateCache, xr *unstructured.Unstructured, result *[]unstructured.Unstructured, depth int) {
	if depth > 5 {
		return
	}

	liveXR := findLiveResource(cache, xr.GetAPIVersion(), xr.GetKind(), xr.GetNamespace(), xr.GetName())
	if liveXR == nil {
		return
	}

	refs, found, _ := unstructured.NestedSlice(liveXR.Object, "spec", "resourceRefs")
	if !found {
		refs, _, _ = unstructured.NestedSlice(liveXR.Object, "spec", "crossplane", "resourceRefs")
	}

	for _, refRaw := range refs {
		ref, ok := refRaw.(map[string]interface{})
		if !ok {
			continue
		}
		refKind, _ := ref["kind"].(string)
		refName, _ := ref["name"].(string)
		refAPIVersion, _ := ref["apiVersion"].(string)

		childRes := findLiveResource(cache, refAPIVersion, refKind, "", refName)
		if childRes == nil {
			continue
		}

		group := extractGroup(refAPIVersion)
		if isManagedResourceGroup(group) {
			*result = append(*result, *childRes.DeepCopy())
		} else {
			// This is a composed XR — recurse into it
			collectManagedResources(cache, childRes, result, depth+1)
		}
	}
}

func extractGroup(apiVersion string) string {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func isManagedResourceGroup(group string) bool {
	return strings.HasSuffix(group, ".upbound.io") ||
		group == "upbound.io" ||
		strings.Contains(group, ".aws.crossplane.io") ||
		strings.Contains(group, ".gcp.crossplane.io") ||
		strings.Contains(group, ".azure.crossplane.io")
}

// deepCopyMap creates a deep copy of a map.
func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	data, _ := json.Marshal(m)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result
}
