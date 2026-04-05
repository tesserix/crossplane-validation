package diff

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/renderer"
)

// ShowSensitive disables sensitive field masking when set to true.
var ShowSensitive bool

// sensitiveKeywords are matched case-insensitively against the last segment of a field path.
var sensitiveKeywords = []string{
	"password",
	"secret",
	"token",
	"key",
	"credential",
	"apikey",
	"masterpassword",
	"connectionstring",
	"accesskey",
	"secretkey",
	"privatekey",
	"cert",
	"certificate",
}

// isSensitiveField checks if the last segment of a dot-separated field path
// contains any sensitive keyword (case-insensitive).
func isSensitiveField(path string) bool {
	parts := strings.Split(path, ".")
	field := strings.ToLower(parts[len(parts)-1])
	for _, kw := range sensitiveKeywords {
		if strings.Contains(field, kw) {
			return true
		}
	}
	return false
}

// maskFieldChange replaces values in a FieldChange if the field is sensitive.
func maskFieldChange(fc FieldChange) FieldChange {
	if ShowSensitive || !isSensitiveField(fc.Path) {
		return fc
	}
	masked := fc
	switch fc.Action {
	case ActionCreate:
		masked.NewValue = "(sensitive value)"
	case ActionUpdate:
		masked.OldValue = "(sensitive value)"
		masked.NewValue = "(sensitive value changed)"
	case ActionDelete:
		masked.OldValue = "(sensitive value removed)"
	}
	return masked
}

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
	Namespace    string
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
				Namespace:   targetRes.Resource.GetNamespace(),
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
					Namespace:    targetRes.Resource.GetNamespace(),
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
				Namespace:   baseRes.Resource.GetNamespace(),
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
			changes = append(changes, maskFieldChange(FieldChange{
				Path:     path,
				NewValue: targetVal,
				Action:   ActionCreate,
			}))
		case baseExists && !targetExists:
			changes = append(changes, maskFieldChange(FieldChange{
				Path:     path,
				OldValue: baseVal,
				Action:   ActionDelete,
			}))
		case baseExists && targetExists:
			baseMap, baseIsMap := baseVal.(map[string]interface{})
			targetMap, targetIsMap := targetVal.(map[string]interface{})

			baseArr, baseIsArr := baseVal.([]interface{})
			targetArr, targetIsArr := targetVal.([]interface{})

			if baseIsMap && targetIsMap {
				changes = append(changes, diffMaps(path, baseMap, targetMap)...)
			} else if baseIsArr && targetIsArr {
				changes = append(changes, diffArrays(path, baseArr, targetArr)...)
			} else if !reflect.DeepEqual(baseVal, targetVal) {
				changes = append(changes, maskFieldChange(FieldChange{
					Path:     path,
					OldValue: baseVal,
					NewValue: targetVal,
					Action:   ActionUpdate,
				}))
			}
		}
	}

	return changes
}

// diffArrays computes a meaningful diff between two arrays.
// For short arrays (both <= 3 elements), it falls back to atomic comparison.
// For arrays of primitives, it computes a set diff (added/removed elements).
// For arrays of maps, it tries to match elements by common key fields and diff matched pairs.
// Otherwise, it falls back to atomic comparison.
func diffArrays(path string, old, new []interface{}) []FieldChange {
	if reflect.DeepEqual(old, new) {
		return nil
	}

	// Short arrays: atomic comparison for brevity
	if len(old) <= 3 && len(new) <= 3 {
		return []FieldChange{maskFieldChange(FieldChange{
			Path:     path,
			OldValue: old,
			NewValue: new,
			Action:   ActionUpdate,
		})}
	}

	// Check if arrays are primitives
	if isArrayOfPrimitives(old) && isArrayOfPrimitives(new) {
		return diffPrimitiveArrays(path, old, new)
	}

	// Check if arrays are maps with identifiable keys
	if isArrayOfMaps(old) && isArrayOfMaps(new) {
		if changes := diffMapArrays(path, old, new); changes != nil {
			return changes
		}
	}

	// Fallback: atomic comparison
	return []FieldChange{maskFieldChange(FieldChange{
		Path:     path,
		OldValue: old,
		NewValue: new,
		Action:   ActionUpdate,
	})}
}

// isArrayOfPrimitives returns true if every element is a string, number, or bool.
func isArrayOfPrimitives(arr []interface{}) bool {
	for _, v := range arr {
		switch v.(type) {
		case string, float64, int64, bool, int, float32:
			continue
		default:
			return false
		}
	}
	return true
}

// isArrayOfMaps returns true if every element is a map[string]interface{}.
func isArrayOfMaps(arr []interface{}) bool {
	if len(arr) == 0 {
		return false
	}
	for _, v := range arr {
		if _, ok := v.(map[string]interface{}); !ok {
			return false
		}
	}
	return true
}

// diffPrimitiveArrays computes added/removed elements between two arrays of primitives.
func diffPrimitiveArrays(path string, old, new []interface{}) []FieldChange {
	var changes []FieldChange

	oldSet := make(map[string]bool, len(old))
	for _, v := range old {
		oldSet[fmt.Sprintf("%v", v)] = true
	}
	newSet := make(map[string]bool, len(new))
	for _, v := range new {
		newSet[fmt.Sprintf("%v", v)] = true
	}

	// Find removed elements
	var removed []string
	for _, v := range old {
		s := fmt.Sprintf("%v", v)
		if !newSet[s] {
			removed = append(removed, s)
		}
	}

	// Find added elements
	var added []string
	for _, v := range new {
		s := fmt.Sprintf("%v", v)
		if !oldSet[s] {
			added = append(added, s)
		}
	}

	if len(removed) > 0 {
		changes = append(changes, maskFieldChange(FieldChange{
			Path:     path,
			OldValue: fmt.Sprintf("[removed: %s]", strings.Join(removed, ", ")),
			Action:   ActionDelete,
		}))
	}
	if len(added) > 0 {
		changes = append(changes, maskFieldChange(FieldChange{
			Path:     path,
			NewValue: fmt.Sprintf("[added: %s]", strings.Join(added, ", ")),
			Action:   ActionCreate,
		}))
	}

	return changes
}

// commonKeyFields are fields used to identify and match array elements.
var commonKeyFields = []string{"name", "id", "protocol", "type"}

// findMatchKey returns the first common key field present in a map element.
func findMatchKey(m map[string]interface{}) string {
	for _, key := range commonKeyFields {
		if _, ok := m[key]; ok {
			return key
		}
	}
	return ""
}

// diffMapArrays diffs arrays of maps by matching elements on a common key field.
// Returns nil if no common key field can be found to match on.
func diffMapArrays(path string, old, new []interface{}) []FieldChange {
	// Determine the match key from the first element of either array
	var matchKey string
	for _, v := range old {
		if m, ok := v.(map[string]interface{}); ok {
			matchKey = findMatchKey(m)
			break
		}
	}
	if matchKey == "" {
		for _, v := range new {
			if m, ok := v.(map[string]interface{}); ok {
				matchKey = findMatchKey(m)
				break
			}
		}
	}
	if matchKey == "" {
		return nil // no identifiable key, caller should fall back
	}

	// Index old elements by their match key value
	oldByKey := make(map[string]map[string]interface{})
	for _, v := range old {
		m := v.(map[string]interface{})
		keyVal := fmt.Sprintf("%v", m[matchKey])
		oldByKey[keyVal] = m
	}

	// Index new elements by their match key value
	newByKey := make(map[string]map[string]interface{})
	for _, v := range new {
		m := v.(map[string]interface{})
		keyVal := fmt.Sprintf("%v", m[matchKey])
		newByKey[keyVal] = m
	}

	var changes []FieldChange

	// Collect all keys in order
	allKeys := map[string]bool{}
	for k := range oldByKey {
		allKeys[k] = true
	}
	for k := range newByKey {
		allKeys[k] = true
	}
	sortedKeys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, keyVal := range sortedKeys {
		elemPath := fmt.Sprintf("%s[%s=%s]", path, matchKey, keyVal)
		oldElem, oldExists := oldByKey[keyVal]
		newElem, newExists := newByKey[keyVal]

		switch {
		case oldExists && !newExists:
			// Element removed
			changes = append(changes, maskFieldChange(FieldChange{
				Path:     elemPath,
				OldValue: oldElem,
				Action:   ActionDelete,
			}))
		case !oldExists && newExists:
			// Element added
			changes = append(changes, maskFieldChange(FieldChange{
				Path:     elemPath,
				NewValue: newElem,
				Action:   ActionCreate,
			}))
		case oldExists && newExists:
			// Both exist — diff field by field
			elemChanges := diffMaps(elemPath, oldElem, newElem)
			changes = append(changes, elemChanges...)
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
			changes = append(changes, maskFieldChange(fc))
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
