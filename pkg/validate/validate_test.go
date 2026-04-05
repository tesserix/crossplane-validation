package validate

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/manifest"
)

func makeUnstructured(apiVersion, kind, name string, spec map[string]interface{}) unstructured.Unstructured {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	if spec != nil {
		obj.Object["spec"] = spec
	}
	return obj
}

func makeXRD() unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apiextensions.crossplane.io/v1",
			"kind":       "CompositeResourceDefinition",
			"metadata": map[string]interface{}{
				"name": "xbuckets.storage.example.org",
			},
			"spec": map[string]interface{}{
				"group": "storage.example.org",
				"names": map[string]interface{}{
					"kind":   "XBucket",
					"plural": "xbuckets",
				},
				"claimNames": map[string]interface{}{
					"kind":   "Bucket",
					"plural": "buckets",
				},
				"versions": []interface{}{
					map[string]interface{}{
						"name":          "v1alpha1",
						"served":        true,
						"referenceable": true,
						"schema": map[string]interface{}{
							"openAPIV3Schema": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"spec": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"region": map[string]interface{}{
												"type": "string",
											},
											"bucketName": map[string]interface{}{
												"type": "string",
											},
											"acl": map[string]interface{}{
												"type": "string",
												"enum": []interface{}{"private", "public-read", "public-read-write"},
											},
										},
										"required": []interface{}{"region", "bucketName"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestValidXRPasses(t *testing.T) {
	rs := &manifest.ResourceSet{
		XRDs: []unstructured.Unstructured{makeXRD()},
		XRs: []unstructured.Unstructured{
			makeUnstructured("storage.example.org/v1alpha1", "XBucket", "my-bucket", map[string]interface{}{
				"region":     "us-east-1",
				"bucketName": "my-bucket",
				"acl":        "private",
			}),
		},
	}

	issues := Validate(rs)

	// Filter to only errors (there may be a warning about missing Composition)
	var errors []ValidationIssue
	for _, i := range issues {
		if i.Severity == "error" {
			errors = append(errors, i)
		}
	}

	if len(errors) > 0 {
		for _, i := range errors {
			t.Errorf("unexpected error: %s %s: %s", i.Resource, i.Field, i.Message)
		}
	}
}

func TestMissingRequiredFieldGeneratesError(t *testing.T) {
	rs := &manifest.ResourceSet{
		XRDs: []unstructured.Unstructured{makeXRD()},
		XRs: []unstructured.Unstructured{
			makeUnstructured("storage.example.org/v1alpha1", "XBucket", "my-bucket", map[string]interface{}{
				"region": "us-east-1",
				// bucketName is missing
			}),
		},
	}

	issues := Validate(rs)

	found := false
	for _, i := range issues {
		if i.Severity == "error" && i.Field == "spec.bucketName" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected error for missing required field spec.bucketName, got issues: %+v", issues)
	}
}

func TestInvalidEnumValueGeneratesError(t *testing.T) {
	rs := &manifest.ResourceSet{
		XRDs: []unstructured.Unstructured{makeXRD()},
		XRs: []unstructured.Unstructured{
			makeUnstructured("storage.example.org/v1alpha1", "XBucket", "my-bucket", map[string]interface{}{
				"region":     "us-east-1",
				"bucketName": "my-bucket",
				"acl":        "invalid-acl",
			}),
		},
	}

	issues := Validate(rs)

	found := false
	for _, i := range issues {
		if i.Severity == "error" && i.Field == "spec.acl" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected error for invalid enum value at spec.acl, got issues: %+v", issues)
	}
}

func TestMissingProviderConfigGeneratesWarning(t *testing.T) {
	rs := &manifest.ResourceSet{
		ManagedResources: []unstructured.Unstructured{
			makeUnstructured("s3.aws.upbound.io/v1beta1", "Bucket", "my-s3", map[string]interface{}{
				"providerConfigRef": map[string]interface{}{
					"name": "aws-provider-config",
				},
			}),
		},
		// No ProviderConfigs
	}

	issues := Validate(rs)

	found := false
	for _, i := range issues {
		if i.Severity == "warning" && i.Field == "spec.providerConfigRef.name" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected warning for missing ProviderConfig, got issues: %+v", issues)
	}
}

func TestMissingCompositionGeneratesWarning(t *testing.T) {
	rs := &manifest.ResourceSet{
		XRs: []unstructured.Unstructured{
			makeUnstructured("storage.example.org/v1alpha1", "XBucket", "my-bucket", map[string]interface{}{
				"region":     "us-east-1",
				"bucketName": "my-bucket",
			}),
		},
		// No Compositions
	}

	issues := Validate(rs)

	found := false
	for _, i := range issues {
		if i.Severity == "warning" && i.Resource == "XBucket/my-bucket" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected warning for missing Composition, got issues: %+v", issues)
	}
}
