package manifest

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestParseBytes(t *testing.T) {
	yaml := `
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: my-bucket
spec:
  forProvider:
    region: us-east-1
---
apiVersion: apiextensions.crossplane.io/v1
kind: CompositeResourceDefinition
metadata:
  name: xbuckets.storage.example.org
`
	rs, err := ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	if len(rs.ManagedResources) != 1 {
		t.Errorf("expected 1 managed resource, got %d", len(rs.ManagedResources))
	}
	if len(rs.XRDs) != 1 {
		t.Errorf("expected 1 XRD, got %d", len(rs.XRDs))
	}
}

func TestParseBytesEmpty(t *testing.T) {
	rs, err := ParseBytes([]byte(""))
	if err != nil {
		t.Fatalf("ParseBytes on empty input failed: %v", err)
	}
	if len(rs.AllResources()) != 0 {
		t.Errorf("expected 0 resources, got %d", len(rs.AllResources()))
	}
}

func TestParseBytesInvalidYAML(t *testing.T) {
	_, err := ParseBytes([]byte("not: valid: yaml: ["))
	// should not crash, may return error or empty set
	if err != nil {
		t.Logf("got expected error for invalid YAML: %v", err)
	}
}

func TestFromUnstructuredList(t *testing.T) {
	items := []unstructured.Unstructured{
		{
			Object: map[string]interface{}{
				"apiVersion": "s3.aws.upbound.io/v1beta1",
				"kind":       "Bucket",
				"metadata":   map[string]interface{}{"name": "bucket-1"},
			},
		},
		{
			Object: map[string]interface{}{
				"apiVersion": "apiextensions.crossplane.io/v1",
				"kind":       "Composition",
				"metadata":   map[string]interface{}{"name": "comp-1"},
			},
		},
		{
			Object: map[string]interface{}{
				"apiVersion": "apiextensions.crossplane.io/v1",
				"kind":       "CompositeResourceDefinition",
				"metadata":   map[string]interface{}{"name": "xrd-1"},
			},
		},
		{
			Object: map[string]interface{}{
				"apiVersion": "storage.example.org/v1alpha1",
				"kind":       "XBucket",
				"metadata":   map[string]interface{}{"name": "xr-1"},
			},
		},
	}

	rs := FromUnstructuredList(items)

	if len(rs.ManagedResources) != 1 {
		t.Errorf("expected 1 managed resource, got %d", len(rs.ManagedResources))
	}
	if rs.ManagedResources[0].GetName() != "bucket-1" {
		t.Errorf("expected bucket-1, got %s", rs.ManagedResources[0].GetName())
	}
	if len(rs.Compositions) != 1 {
		t.Errorf("expected 1 composition, got %d", len(rs.Compositions))
	}
	if len(rs.XRDs) != 1 {
		t.Errorf("expected 1 XRD, got %d", len(rs.XRDs))
	}
	if len(rs.XRs) != 1 {
		t.Errorf("expected 1 XR, got %d", len(rs.XRs))
	}
}

func TestFromUnstructuredListEmpty(t *testing.T) {
	rs := FromUnstructuredList(nil)
	if len(rs.AllResources()) != 0 {
		t.Errorf("expected 0 resources, got %d", len(rs.AllResources()))
	}
}

func TestParseBytesMultiProvider(t *testing.T) {
	yaml := `
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: aws-bucket
---
apiVersion: storage.gcp.upbound.io/v1beta1
kind: Bucket
metadata:
  name: gcp-bucket
---
apiVersion: storage.azure.upbound.io/v1beta1
kind: Account
metadata:
  name: azure-storage
`
	rs, err := ParseBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}
	if len(rs.ManagedResources) != 3 {
		t.Errorf("expected 3 managed resources, got %d", len(rs.ManagedResources))
	}
}
