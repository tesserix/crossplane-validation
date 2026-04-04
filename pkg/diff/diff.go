package diff

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/renderer"
)

// Action represents what will happen to a resource.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
	ActionNoop   Action = "noop"
)

// ResourceDiff represents a diff for a single resource.
type ResourceDiff struct {
	Action       Action
	ResourceKey  string
	APIVersion   string
	Kind         string
	Name         string
	Source       string
	FieldChanges []FieldChange
}

// FieldChange represents a change to a single field.
type FieldChange struct {
	Path     string
	OldValue interface{}
	NewValue interface{}
	Action   Action
}

// DiffResult holds the complete diff between two rendered sets.
type DiffResult struct {
	Diffs   []ResourceDiff
	Summary DiffSummary
}

// DiffSummary counts resources by action.
type DiffSummary struct {
	ToAdd    int
	ToChange int
	ToDelete int
	NoOp     int
}

// Compute computes the diff between base (current) and target (desired) rendered sets.
func Compute(base, target *renderer.RenderedSet) *DiffResult {
	result := &DiffResult{}

	baseMap := indexByKey(base)
	targetMap := indexByKey(target)

	// Find creates and updates
	for key, targetRes := range targetMap {
		baseRes, exists := baseMap[key]
		if !exists {
			// New resource
			d := ResourceDiff{
				Action:      ActionCreate,
				ResourceKey: key,
				APIVersion:  targetRes.Resource.GetAPIVersion(),
				Kind:        targetRes.Resource.GetKind(),
				Name:        targetRes.Resource.GetName(),
				Source:      targetRes.Source,
			}
			d.FieldChanges = flattenFields("", nil, targetRes.Resource.Object, ActionCreate)
			result.Diffs = append(result.Diffs, d)
			result.Summary.ToAdd++
		} else {
			// Existing resource — check for changes
			changes := computeFieldChanges(baseRes.Resource, targetRes.Resource)
			if len(changes) > 0 {
				result.Diffs = append(result.Diffs, ResourceDiff{
					Action:       ActionUpdate,
					ResourceKey:  key,
					APIVersion:   targetRes.Resource.GetAPIVersion(),
					Kind:         targetRes.Resource.GetKind(),
					Name:         targetRes.Resource.GetName(),
					Source:       targetRes.Source,
					FieldChanges: changes,
				})
				result.Summary.ToChange++
			} else {
				result.Summary.NoOp++
			}
		}
	}

	// Find deletes
	for key, baseRes := range baseMap {
		if _, exists := targetMap[key]; !exists {
			d := ResourceDiff{
				Action:      ActionDelete,
				ResourceKey: key,
				APIVersion:  baseRes.Resource.GetAPIVersion(),
				Kind:        baseRes.Resource.GetKind(),
				Name:        baseRes.Resource.GetName(),
				Source:      baseRes.Source,
			}
			d.FieldChanges = flattenFields("", baseRes.Resource.Object, nil, ActionDelete)
			result.Diffs = append(result.Diffs, d)
			result.Summary.ToDelete++
		}
	}

	// Sort diffs: deletes first, then updates, then creates
	sort.Slice(result.Diffs, func(i, j int) bool {
		order := map[Action]int{ActionDelete: 0, ActionUpdate: 1, ActionCreate: 2}
		if result.Diffs[i].Action != result.Diffs[j].Action {
			return order[result.Diffs[i].Action] < order[result.Diffs[j].Action]
		}
		return result.Diffs[i].ResourceKey < result.Diffs[j].ResourceKey
	})

	return result
}

// computeFieldChanges diffs the spec of two resources, ignoring metadata noise.
func computeFieldChanges(base, target unstructured.Unstructured) []FieldChange {
	// Only diff spec.forProvider (the user-controlled part)
	baseFP, _, _ := unstructured.NestedMap(base.Object, "spec", "forProvider")
	targetFP, _, _ := unstructured.NestedMap(target.Object, "spec", "forProvider")

	if baseFP != nil || targetFP != nil {
		return diffMaps("spec.forProvider", baseFP, targetFP)
	}

	// Fallback: diff entire spec
	baseSpec, _, _ := unstructured.NestedMap(base.Object, "spec")
	targetSpec, _, _ := unstructured.NestedMap(target.Object, "spec")
	return diffMaps("spec", baseSpec, targetSpec)
}

func diffMaps(prefix string, base, target map[string]interface{}) []FieldChange {
	var changes []FieldChange

	if base == nil {
		base = map[string]interface{}{}
	}
	if target == nil {
		target = map[string]interface{}{}
	}

	allKeys := map[string]bool{}
	for k := range base {
		allKeys[k] = true
	}
	for k := range target {
		allKeys[k] = true
	}

	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, key := range sortedKeys {
		path := prefix + "." + key
		baseVal, baseExists := base[key]
		targetVal, targetExists := target[key]

		switch {
		case !baseExists && targetExists:
			changes = append(changes, FieldChange{
				Path:     path,
				NewValue: targetVal,
				Action:   ActionCreate,
			})
		case baseExists && !targetExists:
			changes = append(changes, FieldChange{
				Path:     path,
				OldValue: baseVal,
				Action:   ActionDelete,
			})
		case baseExists && targetExists:
			baseMap, baseIsMap := baseVal.(map[string]interface{})
			targetMap, targetIsMap := targetVal.(map[string]interface{})

			if baseIsMap && targetIsMap {
				changes = append(changes, diffMaps(path, baseMap, targetMap)...)
			} else if !reflect.DeepEqual(baseVal, targetVal) {
				changes = append(changes, FieldChange{
					Path:     path,
					OldValue: baseVal,
					NewValue: targetVal,
					Action:   ActionUpdate,
				})
			}
		}
	}

	return changes
}

func flattenFields(prefix string, old, new interface{}, action Action) []FieldChange {
	var changes []FieldChange
	target := new
	if target == nil {
		target = old
	}

	m, ok := target.(map[string]interface{})
	if !ok {
		return changes
	}

	for key, val := range m {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if skipField(path) {
			continue
		}

		if subMap, ok := val.(map[string]interface{}); ok {
			changes = append(changes, flattenFields(path, nil, subMap, action)...)
		} else {
			fc := FieldChange{Path: path, Action: action}
			if action == ActionCreate {
				fc.NewValue = val
			} else {
				fc.OldValue = val
			}
			changes = append(changes, fc)
		}
	}
	return changes
}

func skipField(path string) bool {
	skip := []string{
		"metadata.resourceVersion",
		"metadata.uid",
		"metadata.creationTimestamp",
		"metadata.generation",
		"status",
	}
	for _, s := range skip {
		if strings.HasPrefix(path, s) {
			return true
		}
	}
	return false
}

func indexByKey(rs *renderer.RenderedSet) map[string]renderer.RenderedResource {
	m := make(map[string]renderer.RenderedResource)
	if rs == nil {
		return m
	}
	for _, r := range rs.Resources {
		m[r.ResourceKey()] = r
	}
	return m
}

// String returns a human-readable summary.
func (s DiffSummary) String() string {
	return fmt.Sprintf("%d to add, %d to change, %d to destroy", s.ToAdd, s.ToChange, s.ToDelete)
}
