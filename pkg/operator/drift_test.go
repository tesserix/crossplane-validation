package operator

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/manifest"
)

func TestDetectDriftNoChanges(t *testing.T) {
	live := manifest.FromUnstructuredList([]unstructured.Unstructured{
		makeResource("s3.aws.upbound.io/v1beta1", "Bucket", "bucket-1", map[string]interface{}{
			"region": "us-east-1",
		}),
	})
	proposed := manifest.FromUnstructuredList([]unstructured.Unstructured{
		makeResource("s3.aws.upbound.io/v1beta1", "Bucket", "bucket-1", map[string]interface{}{
			"region": "us-east-1",
		}),
	})

	warnings := detectDrift(live, proposed)
	if len(warnings) != 0 {
		t.Errorf("expected 0 drift warnings, got %d", len(warnings))
	}
}

func TestDetectDriftFieldDifference(t *testing.T) {
	live := manifest.FromUnstructuredList([]unstructured.Unstructured{
		makeResource("s3.aws.upbound.io/v1beta1", "Bucket", "bucket-1", map[string]interface{}{
			"region": "us-east-1",
		}),
	})
	proposed := manifest.FromUnstructuredList([]unstructured.Unstructured{
		makeResource("s3.aws.upbound.io/v1beta1", "Bucket", "bucket-1", map[string]interface{}{
			"region": "eu-west-1",
		}),
	})

	warnings := detectDrift(live, proposed)
	if len(warnings) != 1 {
		t.Fatalf("expected 1 drift warning, got %d", len(warnings))
	}
	if warnings[0].Severity != "warning" {
		t.Errorf("expected severity 'warning', got %q", warnings[0].Severity)
	}
}

func TestDetectDriftNewResource(t *testing.T) {
	live := manifest.FromUnstructuredList(nil)
	proposed := manifest.FromUnstructuredList([]unstructured.Unstructured{
		makeResource("s3.aws.upbound.io/v1beta1", "Bucket", "new-bucket", map[string]interface{}{
			"region": "us-east-1",
		}),
	})

	warnings := detectDrift(live, proposed)
	if len(warnings) != 0 {
		t.Errorf("new resources should not produce drift warnings, got %d", len(warnings))
	}
}

func TestDetectDriftMultipleFields(t *testing.T) {
	live := manifest.FromUnstructuredList([]unstructured.Unstructured{
		makeResource("rds.aws.upbound.io/v1beta1", "Instance", "db-1", map[string]interface{}{
			"instanceClass":    "db.r6g.large",
			"allocatedStorage": int64(100),
		}),
	})
	proposed := manifest.FromUnstructuredList([]unstructured.Unstructured{
		makeResource("rds.aws.upbound.io/v1beta1", "Instance", "db-1", map[string]interface{}{
			"instanceClass":    "db.r6g.xlarge",
			"allocatedStorage": int64(200),
		}),
	})

	warnings := detectDrift(live, proposed)
	if len(warnings) != 2 {
		t.Errorf("expected 2 drift warnings, got %d", len(warnings))
	}
}

func TestIndexResources(t *testing.T) {
	resources := []unstructured.Unstructured{
		makeResource("s3.aws.upbound.io/v1beta1", "Bucket", "a", nil),
		makeResource("rds.aws.upbound.io/v1beta1", "Instance", "b", nil),
	}

	idx := indexResources(resources)
	if len(idx) != 2 {
		t.Errorf("expected 2 indexed resources, got %d", len(idx))
	}

	key := "s3.aws.upbound.io/v1beta1/Bucket/a"
	if _, ok := idx[key]; !ok {
		t.Errorf("expected key %q in index", key)
	}
}

func makeResource(apiVersion, kind, name string, forProvider map[string]interface{}) unstructured.Unstructured {
	obj := map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": name},
	}
	if forProvider != nil {
		obj["spec"] = map[string]interface{}{"forProvider": forProvider}
	}
	return unstructured.Unstructured{Object: obj}
}
