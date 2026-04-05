package validate

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/manifest"
)

// ValidationIssue represents a single validation finding.
type ValidationIssue struct {
	Severity string // "error" or "warning"
	Resource string // "Kind/Name"
	Field    string // field path
	Message  string // human-readable description
}

// Validate checks all XRs and Claims against their XRD schemas,
// verifies ProviderConfig references, and checks Composition references.
func Validate(rs *manifest.ResourceSet) []ValidationIssue {
	var issues []ValidationIssue

	issues = append(issues, validateXRDSchemas(rs)...)
	issues = append(issues, validateProviderConfigRefs(rs)...)
	issues = append(issues, validateCompositionRefs(rs)...)

	return issues
}

// xrdInfo holds extracted metadata from an XRD needed for matching and validation.
type xrdInfo struct {
	group     string
	xrKind    string
	claimKind string
	schema    map[string]interface{}
}

// extractXRDInfos parses all XRDs and returns their metadata.
func extractXRDInfos(xrds []unstructured.Unstructured) []xrdInfo {
	var infos []xrdInfo

	for _, xrd := range xrds {
		info := xrdInfo{}

		// Extract spec.group
		group, found, _ := unstructured.NestedString(xrd.Object, "spec", "group")
		if !found {
			continue
		}
		info.group = group

		// Extract spec.names.kind (the XR kind)
		xrKind, found, _ := unstructured.NestedString(xrd.Object, "spec", "names", "kind")
		if !found {
			continue
		}
		info.xrKind = xrKind

		// Extract spec.claimNames.kind (optional)
		claimKind, _, _ := unstructured.NestedString(xrd.Object, "spec", "claimNames", "kind")
		info.claimKind = claimKind

		// Extract schema from the served version (or first version)
		versions, found, _ := unstructured.NestedSlice(xrd.Object, "spec", "versions")
		if !found || len(versions) == 0 {
			continue
		}

		schema := findServedVersionSchema(versions)
		if schema == nil {
			continue
		}
		info.schema = schema

		infos = append(infos, info)
	}

	return infos
}

// findServedVersionSchema finds the schema from the served version, falling back to the first.
func findServedVersionSchema(versions []interface{}) map[string]interface{} {
	// Try to find the served version first
	for _, v := range versions {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		served, _, _ := unstructured.NestedBool(vm, "served")
		if served {
			schema, found, _ := unstructured.NestedMap(vm, "schema", "openAPIV3Schema")
			if found {
				return schema
			}
		}
	}

	// Fallback: use the first version
	if vm, ok := versions[0].(map[string]interface{}); ok {
		schema, found, _ := unstructured.NestedMap(vm, "schema", "openAPIV3Schema")
		if found {
			return schema
		}
	}

	return nil
}

// validateXRDSchemas validates XRs and Claims against their XRD schemas.
func validateXRDSchemas(rs *manifest.ResourceSet) []ValidationIssue {
	var issues []ValidationIssue

	infos := extractXRDInfos(rs.XRDs)

	// Combine XRs and Claims for validation
	candidates := make([]unstructured.Unstructured, 0, len(rs.XRs)+len(rs.Claims))
	candidates = append(candidates, rs.XRs...)
	candidates = append(candidates, rs.Claims...)

	for _, info := range infos {
		for _, res := range candidates {
			if !matchesXRD(res, info) {
				continue
			}

			resourceID := res.GetKind() + "/" + res.GetName()

			// Get the spec-level schema
			specSchema := getNestedSchema(info.schema, "spec")
			if specSchema == nil {
				continue
			}

			// Get the resource's spec
			spec, found, _ := unstructured.NestedMap(res.Object, "spec")
			if !found {
				continue
			}

			issues = append(issues, validateObject(specSchema, spec, "spec", resourceID)...)
		}
	}

	return issues
}

// matchesXRD checks if a resource matches an XRD by group and kind.
func matchesXRD(res unstructured.Unstructured, info xrdInfo) bool {
	apiVersion := res.GetAPIVersion()
	kind := res.GetKind()

	group := extractGroup(apiVersion)
	if group != info.group {
		return false
	}

	return kind == info.xrKind || kind == info.claimKind
}

// extractGroup extracts the API group from an apiVersion string.
func extractGroup(apiVersion string) string {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// getNestedSchema extracts a property schema from an openAPIV3Schema object.
func getNestedSchema(schema map[string]interface{}, field string) map[string]interface{} {
	props, found, _ := unstructured.NestedMap(schema, "properties", field)
	if found {
		return props
	}
	return nil
}

// validateObject recursively validates a resource object against its schema.
func validateObject(schema map[string]interface{}, obj map[string]interface{}, path string, resourceID string) []ValidationIssue {
	var issues []ValidationIssue

	// Check required fields
	issues = append(issues, checkRequired(schema, obj, path, resourceID)...)

	// Check each property that exists in the object
	propsSchema, found, _ := unstructured.NestedMap(schema, "properties")
	if !found {
		return issues
	}

	for fieldName, value := range obj {
		fieldSchemaRaw, ok := propsSchema[fieldName]
		if !ok {
			continue // field not in schema, skip (additionalProperties handling is out of scope)
		}

		fieldSchema, ok := fieldSchemaRaw.(map[string]interface{})
		if !ok {
			continue
		}

		fieldPath := path + "." + fieldName

		// Check type mismatch
		issues = append(issues, checkType(fieldSchema, value, fieldPath, resourceID)...)

		// Check enum constraint
		issues = append(issues, checkEnum(fieldSchema, value, fieldPath, resourceID)...)

		// Recurse into nested objects
		if nestedObj, ok := value.(map[string]interface{}); ok {
			issues = append(issues, validateObject(fieldSchema, nestedObj, fieldPath, resourceID)...)
		}
	}

	return issues
}

// checkRequired verifies that all required fields exist in the object.
func checkRequired(schema map[string]interface{}, obj map[string]interface{}, path string, resourceID string) []ValidationIssue {
	var issues []ValidationIssue

	requiredRaw, found, _ := unstructured.NestedSlice(schema, "required")
	if !found {
		return nil
	}

	for _, r := range requiredRaw {
		fieldName, ok := r.(string)
		if !ok {
			continue
		}
		if _, exists := obj[fieldName]; !exists {
			issues = append(issues, ValidationIssue{
				Severity: "error",
				Resource: resourceID,
				Field:    path + "." + fieldName,
				Message:  fmt.Sprintf("required field %q is missing", fieldName),
			})
		}
	}

	return issues
}

// checkType validates that the value matches the expected schema type.
func checkType(schema map[string]interface{}, value interface{}, path string, resourceID string) []ValidationIssue {
	expectedType, found, _ := unstructured.NestedString(schema, "type")
	if !found {
		return nil
	}

	actual := detectType(value)
	if actual == "" {
		return nil
	}

	if !typesMatch(expectedType, actual) {
		return []ValidationIssue{{
			Severity: "error",
			Resource: resourceID,
			Field:    path,
			Message:  fmt.Sprintf("type mismatch: expected %s but got %s", expectedType, actual),
		}}
	}

	return nil
}

// detectType returns the JSON Schema type name for a Go value.
func detectType(value interface{}) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int, int32, int64:
		return "integer"
	case float32, float64:
		// JSON/YAML numbers decoded as float64; could be integer
		return "number"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	default:
		return ""
	}
}

// typesMatch checks if the actual type is compatible with the expected schema type.
func typesMatch(expected, actual string) bool {
	if expected == actual {
		return true
	}
	// number is compatible with integer (a float64 value 3.0 could represent an integer)
	if expected == "integer" && actual == "number" {
		return true
	}
	if expected == "number" && actual == "integer" {
		return true
	}
	return false
}

// checkEnum validates that the value is within the allowed enum values.
func checkEnum(schema map[string]interface{}, value interface{}, path string, resourceID string) []ValidationIssue {
	enumRaw, found := schema["enum"]
	if !found {
		return nil
	}

	enumSlice, ok := enumRaw.([]interface{})
	if !ok {
		return nil
	}

	valStr := fmt.Sprintf("%v", value)
	for _, e := range enumSlice {
		if fmt.Sprintf("%v", e) == valStr {
			return nil
		}
	}

	allowed := make([]string, 0, len(enumSlice))
	for _, e := range enumSlice {
		allowed = append(allowed, fmt.Sprintf("%v", e))
	}

	return []ValidationIssue{{
		Severity: "error",
		Resource: resourceID,
		Field:    path,
		Message:  fmt.Sprintf("value %q is not in allowed enum values: [%s]", valStr, strings.Join(allowed, ", ")),
	}}
}

// validateProviderConfigRefs checks that ProviderConfig references point to existing ProviderConfigs.
func validateProviderConfigRefs(rs *manifest.ResourceSet) []ValidationIssue {
	var issues []ValidationIssue

	// Build a set of known ProviderConfig names
	knownConfigs := make(map[string]bool)
	for _, pc := range rs.ProviderConfigs {
		knownConfigs[pc.GetName()] = true
	}

	// Check ManagedResources and XRs
	candidates := make([]unstructured.Unstructured, 0, len(rs.ManagedResources)+len(rs.XRs))
	candidates = append(candidates, rs.ManagedResources...)
	candidates = append(candidates, rs.XRs...)

	for _, res := range candidates {
		refName, found, _ := unstructured.NestedString(res.Object, "spec", "providerConfigRef", "name")
		if !found || refName == "" {
			continue
		}

		if !knownConfigs[refName] {
			issues = append(issues, ValidationIssue{
				Severity: "warning",
				Resource: res.GetKind() + "/" + res.GetName(),
				Field:    "spec.providerConfigRef.name",
				Message:  fmt.Sprintf("ProviderConfig %q not found in scanned manifests (may exist in cluster)", refName),
			})
		}
	}

	return issues
}

// validateCompositionRefs checks that each XR has a matching Composition.
func validateCompositionRefs(rs *manifest.ResourceSet) []ValidationIssue {
	var issues []ValidationIssue

	// Build a set of compositeTypeRefs from Compositions: "group/version.Kind"
	type compositeTypeRef struct {
		apiVersion string
		kind       string
	}

	knownCompositions := make(map[compositeTypeRef]bool)
	for _, comp := range rs.Compositions {
		apiVer, _, _ := unstructured.NestedString(comp.Object, "spec", "compositeTypeRef", "apiVersion")
		kind, _, _ := unstructured.NestedString(comp.Object, "spec", "compositeTypeRef", "kind")
		if apiVer != "" && kind != "" {
			knownCompositions[compositeTypeRef{apiVersion: apiVer, kind: kind}] = true
		}
	}

	for _, xr := range rs.XRs {
		ref := compositeTypeRef{
			apiVersion: xr.GetAPIVersion(),
			kind:       xr.GetKind(),
		}

		if !knownCompositions[ref] {
			issues = append(issues, ValidationIssue{
				Severity: "warning",
				Resource: xr.GetKind() + "/" + xr.GetName(),
				Field:    "",
				Message:  fmt.Sprintf("no Composition found with compositeTypeRef matching %s %s (may exist in cluster)", xr.GetAPIVersion(), xr.GetKind()),
			})
		}
	}

	return issues
}
