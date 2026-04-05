package operator

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestResourceKey(t *testing.T) {
	tests := []struct {
		apiVersion string
		kind       string
		namespace  string
		name       string
		want       string
	}{
		{"s3.aws.upbound.io/v1beta1", "Bucket", "", "my-bucket", "s3.aws.upbound.io/v1beta1/Bucket/my-bucket"},
		{"v1", "ConfigMap", "default", "my-cm", "v1/ConfigMap/default/my-cm"},
		{"apps/v1", "Deployment", "kube-system", "coredns", "apps/v1/Deployment/kube-system/coredns"},
	}

	for _, tt := range tests {
		got := resourceKey(tt.apiVersion, tt.kind, tt.namespace, tt.name)
		if got != tt.want {
			t.Errorf("resourceKey(%q, %q, %q, %q) = %q, want %q",
				tt.apiVersion, tt.kind, tt.namespace, tt.name, got, tt.want)
		}
	}
}

func TestResourceKeyFromObj(t *testing.T) {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rds.aws.upbound.io/v1beta1",
			"kind":       "Instance",
			"metadata": map[string]interface{}{
				"name":      "app-db",
				"namespace": "crossplane-system",
			},
		},
	}

	key := resourceKeyFromObj(obj)
	want := "rds.aws.upbound.io/v1beta1/Instance/crossplane-system/app-db"
	if key != want {
		t.Errorf("resourceKeyFromObj = %q, want %q", key, want)
	}
}

func TestStateCacheBasicOperations(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	if cache.ResourceCount() != 0 {
		t.Errorf("empty cache should have 0 resources")
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "test-bucket"},
		},
	}

	cache.onAdd(obj)

	if cache.ResourceCount() != 1 {
		t.Errorf("expected 1 resource after add, got %d", cache.ResourceCount())
	}

	got := cache.GetResource("s3.aws.upbound.io/v1beta1", "Bucket", "", "test-bucket")
	if got == nil {
		t.Fatal("GetResource returned nil for existing resource")
	}
	if got.GetName() != "test-bucket" {
		t.Errorf("GetResource returned name %q, want %q", got.GetName(), "test-bucket")
	}

	cache.onDelete(obj)

	if cache.ResourceCount() != 0 {
		t.Errorf("expected 0 resources after delete, got %d", cache.ResourceCount())
	}

	got = cache.GetResource("s3.aws.upbound.io/v1beta1", "Bucket", "", "test-bucket")
	if got != nil {
		t.Error("GetResource should return nil after delete")
	}
}

func TestStateCacheGetResourceSetClassification(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	resources := []*unstructured.Unstructured{
		{Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "bucket-1"},
		}},
		{Object: map[string]interface{}{
			"apiVersion": "apiextensions.crossplane.io/v1",
			"kind":       "CompositeResourceDefinition",
			"metadata":   map[string]interface{}{"name": "xrd-1"},
		}},
		{Object: map[string]interface{}{
			"apiVersion": "apiextensions.crossplane.io/v1",
			"kind":       "Composition",
			"metadata":   map[string]interface{}{"name": "comp-1"},
		}},
	}

	for _, r := range resources {
		cache.onAdd(r)
	}

	rs := cache.GetResourceSet()

	if len(rs.ManagedResources) != 1 {
		t.Errorf("expected 1 managed resource, got %d", len(rs.ManagedResources))
	}
	if len(rs.XRDs) != 1 {
		t.Errorf("expected 1 XRD, got %d", len(rs.XRDs))
	}
	if len(rs.Compositions) != 1 {
		t.Errorf("expected 1 composition, got %d", len(rs.Compositions))
	}
}

func TestStateCacheAllResources(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	for i := 0; i < 5; i++ {
		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "s3.aws.upbound.io/v1beta1",
				"kind":       "Bucket",
				"metadata":   map[string]interface{}{"name": "bucket-" + string(rune('a'+i))},
			},
		}
		cache.onAdd(obj)
	}

	all := cache.AllResources()
	if len(all) != 5 {
		t.Errorf("AllResources returned %d, want 5", len(all))
	}
}

func TestStateCacheDeepCopy(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "test"},
			"spec":       map[string]interface{}{"forProvider": map[string]interface{}{"region": "us-east-1"}},
		},
	}
	cache.onAdd(obj)

	got := cache.GetResource("s3.aws.upbound.io/v1beta1", "Bucket", "", "test")
	got.Object["spec"].(map[string]interface{})["forProvider"].(map[string]interface{})["region"] = "eu-west-1"

	original := cache.GetResource("s3.aws.upbound.io/v1beta1", "Bucket", "", "test")
	region := original.Object["spec"].(map[string]interface{})["forProvider"].(map[string]interface{})["region"]
	if region != "us-east-1" {
		t.Errorf("modifying returned resource should not affect cache, got region=%v", region)
	}
}

func TestStateCacheGetResourceNotFound(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	got := cache.GetResource("v1", "Pod", "default", "nonexistent")
	if got != nil {
		t.Error("GetResource should return nil for nonexistent resource")
	}
}

func TestStateCacheUpdateOverwrites(t *testing.T) {
	cache := &StateCache{
		resources: make(map[string]*unstructured.Unstructured),
	}

	v1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "test"},
			"spec":       map[string]interface{}{"forProvider": map[string]interface{}{"region": "us-east-1"}},
		},
	}
	cache.onAdd(v1)

	v2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "test"},
			"spec":       map[string]interface{}{"forProvider": map[string]interface{}{"region": "eu-west-1"}},
		},
	}
	cache.onAdd(v2)

	if cache.ResourceCount() != 1 {
		t.Errorf("update should not increase count, got %d", cache.ResourceCount())
	}

	got := cache.GetResource("s3.aws.upbound.io/v1beta1", "Bucket", "", "test")
	region := got.Object["spec"].(map[string]interface{})["forProvider"].(map[string]interface{})["region"]
	if region != "eu-west-1" {
		t.Errorf("expected updated region eu-west-1, got %v", region)
	}
}
