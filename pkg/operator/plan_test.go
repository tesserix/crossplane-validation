package operator

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestComputeLivePlanNewResource(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	proposed := `
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: new-bucket
spec:
  forProvider:
    region: us-east-1
`

	result, warnings, err := ComputeLivePlan(cache, LivePlanRequest{
		ProposedYAML: []byte(proposed),
	})
	if err != nil {
		t.Fatalf("ComputeLivePlan failed: %v", err)
	}

	if result.StructuralDiff == nil {
		t.Fatal("expected non-nil StructuralDiff")
	}
	if result.StructuralDiff.Summary.ToAdd != 1 {
		t.Errorf("expected 1 resource to add, got %d", result.StructuralDiff.Summary.ToAdd)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no drift warnings for new resource, got %d", len(warnings))
	}
}

func TestComputeLivePlanNoChanges(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "existing-bucket"},
			"spec":       map[string]interface{}{"forProvider": map[string]interface{}{"region": "us-east-1"}},
		},
	}
	cache.onAdd(obj)

	proposed := `
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: existing-bucket
spec:
  forProvider:
    region: us-east-1
`

	result, _, err := ComputeLivePlan(cache, LivePlanRequest{
		ProposedYAML: []byte(proposed),
	})
	if err != nil {
		t.Fatalf("ComputeLivePlan failed: %v", err)
	}

	if result.StructuralDiff.Summary.ToAdd != 0 {
		t.Errorf("expected 0 to add, got %d", result.StructuralDiff.Summary.ToAdd)
	}
	if result.StructuralDiff.Summary.ToChange != 0 {
		t.Errorf("expected 0 to change, got %d", result.StructuralDiff.Summary.ToChange)
	}
}

func TestComputeLivePlanUpdate(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "my-bucket"},
			"spec":       map[string]interface{}{"forProvider": map[string]interface{}{"region": "us-east-1"}},
		},
	}
	cache.onAdd(obj)

	proposed := `
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: my-bucket
spec:
  forProvider:
    region: eu-west-1
`

	result, _, err := ComputeLivePlan(cache, LivePlanRequest{
		ProposedYAML: []byte(proposed),
	})
	if err != nil {
		t.Fatalf("ComputeLivePlan failed: %v", err)
	}

	if result.StructuralDiff.Summary.ToChange != 1 {
		t.Errorf("expected 1 to change, got %d", result.StructuralDiff.Summary.ToChange)
	}
}

func TestComputeLivePlanDelete(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "old-bucket"},
			"spec":       map[string]interface{}{"forProvider": map[string]interface{}{"region": "us-east-1"}},
		},
	}
	cache.onAdd(obj)

	// empty proposed = the resource is being removed
	result, _, err := ComputeLivePlan(cache, LivePlanRequest{
		ProposedYAML: []byte(""),
	})
	if err != nil {
		t.Fatalf("ComputeLivePlan failed: %v", err)
	}

	if result.StructuralDiff.Summary.ToDelete != 1 {
		t.Errorf("expected 1 to delete, got %d", result.StructuralDiff.Summary.ToDelete)
	}
}

func TestComputeLivePlanInvalidYAML(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	_, _, err := ComputeLivePlan(cache, LivePlanRequest{
		ProposedYAML: []byte("{{invalid yaml"),
	})
	if err != nil {
		t.Logf("got expected error for invalid YAML: %v", err)
	}
}
