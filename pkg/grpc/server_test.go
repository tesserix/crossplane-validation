package grpc

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/operator"
)

func newTestCache(resources ...*unstructured.Unstructured) *operator.StateCache {
	cache := operator.NewStateCacheForTest()
	for _, r := range resources {
		cache.AddForTest(r)
	}
	return cache
}

func TestHealthReturnsStats(t *testing.T) {
	cache := newTestCache(
		makeUnstructured("s3.aws.upbound.io/v1beta1", "Bucket", "b1"),
		makeUnstructured("rds.aws.upbound.io/v1beta1", "Instance", "db1"),
	)

	svc := &ValidationServiceImpl{cache: cache}
	resp, err := svc.Health(context.Background())
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}

	if !resp.Healthy {
		t.Error("expected healthy=true")
	}
	if resp.CachedResources != 2 {
		t.Errorf("expected 2 cached resources, got %d", resp.CachedResources)
	}
}

func TestGetClusterStateNoFilter(t *testing.T) {
	cache := newTestCache(
		makeUnstructured("s3.aws.upbound.io/v1beta1", "Bucket", "b1"),
		makeUnstructured("rds.aws.upbound.io/v1beta1", "Instance", "db1"),
	)

	svc := &ValidationServiceImpl{cache: cache}
	resp, err := svc.GetClusterState(context.Background(), "", "", "")
	if err != nil {
		t.Fatalf("GetClusterState failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("expected 2 total resources, got %d", resp.Total)
	}
}

func TestGetClusterStateFilterByKind(t *testing.T) {
	cache := newTestCache(
		makeUnstructured("s3.aws.upbound.io/v1beta1", "Bucket", "b1"),
		makeUnstructured("rds.aws.upbound.io/v1beta1", "Instance", "db1"),
		makeUnstructured("s3.aws.upbound.io/v1beta1", "Bucket", "b2"),
	)

	svc := &ValidationServiceImpl{cache: cache}
	resp, err := svc.GetClusterState(context.Background(), "", "Bucket", "")
	if err != nil {
		t.Fatalf("GetClusterState failed: %v", err)
	}

	if resp.Total != 2 {
		t.Errorf("expected 2 Buckets, got %d", resp.Total)
	}
}

func TestComputePlanEmptyInput(t *testing.T) {
	cache := newTestCache()
	svc := &ValidationServiceImpl{cache: cache}

	_, err := svc.ComputePlan(context.Background(), []byte(""), false)
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestComputePlanOversizedInput(t *testing.T) {
	cache := newTestCache()
	svc := &ValidationServiceImpl{cache: cache}

	huge := make([]byte, maxRequestPayload+1)
	_, err := svc.ComputePlan(context.Background(), huge, false)
	if err == nil {
		t.Error("expected error for oversized input")
	}
}

func TestComputePlanValidInput(t *testing.T) {
	cache := newTestCache()
	svc := &ValidationServiceImpl{cache: cache}

	yaml := []byte(`
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: test-bucket
spec:
  forProvider:
    region: us-east-1
`)

	resp, err := svc.ComputePlan(context.Background(), yaml, false)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}
	if resp.Plan == nil {
		t.Fatal("expected non-nil plan")
	}
	if resp.Plan.Summary.ToAdd != 1 {
		t.Errorf("expected 1 to add, got %d", resp.Plan.Summary.ToAdd)
	}
	if resp.ClusterInfo == nil {
		t.Error("expected non-nil ClusterInfo")
	}
}

func TestGetDriftEmptyInput(t *testing.T) {
	cache := newTestCache()
	svc := &ValidationServiceImpl{cache: cache}

	_, err := svc.GetDrift(context.Background(), []byte(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestGetResourceStatusNotFound(t *testing.T) {
	cache := newTestCache()
	svc := &ValidationServiceImpl{cache: cache}

	_, err := svc.GetResourceStatus(context.Background(), "v1", "Pod", "nope", "default")
	if err == nil {
		t.Error("expected error for nonexistent resource")
	}
}

func TestGetResourceStatusFound(t *testing.T) {
	cache := newTestCache(
		makeUnstructured("s3.aws.upbound.io/v1beta1", "Bucket", "found-bucket"),
	)
	svc := &ValidationServiceImpl{cache: cache}

	resp, err := svc.GetResourceStatus(context.Background(), "s3.aws.upbound.io/v1beta1", "Bucket", "found-bucket", "")
	if err != nil {
		t.Fatalf("GetResourceStatus failed: %v", err)
	}
	if resp.Resource.Name != "found-bucket" {
		t.Errorf("expected name found-bucket, got %s", resp.Resource.Name)
	}
}

func TestExtractGroup(t *testing.T) {
	tests := []struct {
		apiVersion string
		want       string
	}{
		{"s3.aws.upbound.io/v1beta1", "s3.aws.upbound.io"},
		{"v1", ""},
		{"apps/v1", "apps"},
	}
	for _, tt := range tests {
		got := extractGroup(tt.apiVersion)
		if got != tt.want {
			t.Errorf("extractGroup(%q) = %q, want %q", tt.apiVersion, got, tt.want)
		}
	}
}

func TestExtractStatusWithConditions(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "test"},
			"status": map[string]interface{}{
				"conditions": []interface{}{
					map[string]interface{}{
						"type":   "Ready",
						"status": "True",
						"reason": "Available",
					},
					map[string]interface{}{
						"type":   "Synced",
						"status": "True",
						"reason": "ReconcileSuccess",
					},
				},
			},
		},
	}

	status := extractStatus(obj)
	if !status.Ready {
		t.Error("expected ready=true")
	}
	if !status.Synced {
		t.Error("expected synced=true")
	}
	if len(status.Conditions) != 2 {
		t.Errorf("expected 2 conditions, got %d", len(status.Conditions))
	}
}

func TestExtractStatusNoConditions(t *testing.T) {
	obj := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "s3.aws.upbound.io/v1beta1",
			"kind":       "Bucket",
			"metadata":   map[string]interface{}{"name": "test"},
		},
	}

	status := extractStatus(obj)
	if status.Ready {
		t.Error("expected ready=false with no status")
	}
	if len(status.Conditions) != 0 {
		t.Errorf("expected 0 conditions, got %d", len(status.Conditions))
	}
}

func makeUnstructured(apiVersion, kind, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   map[string]interface{}{"name": name},
		},
	}
}
