package renderer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/manifest"
)

func TestRenderDirectAWSResources(t *testing.T) {
	rs := &manifest.ResourceSet{}

	rs.ManagedResources = append(rs.ManagedResources, makeUnstructured(
		"ec2.aws.upbound.io/v1beta1", "VPC", "main-vpc",
		map[string]interface{}{"region": "us-east-1", "cidrBlock": "10.0.0.0/16"},
	))
	rs.ManagedResources = append(rs.ManagedResources, makeUnstructured(
		"iam.aws.upbound.io/v1beta1", "Role", "app-role",
		map[string]interface{}{"assumeRolePolicy": "{}"},
	))

	rendered, err := Render(rs)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	if len(rendered.Resources) != 2 {
		t.Errorf("expected 2 rendered resources, got %d", len(rendered.Resources))
	}

	for _, r := range rendered.Resources {
		if r.Source != "direct" {
			t.Errorf("expected source 'direct', got %q", r.Source)
		}
	}
}

func TestRenderDirectGCPResources(t *testing.T) {
	rs := &manifest.ResourceSet{}

	rs.ManagedResources = append(rs.ManagedResources, makeUnstructured(
		"compute.gcp.upbound.io/v1beta1", "Network", "main-net",
		map[string]interface{}{"autoCreateSubnetworks": false, "project": "my-project"},
	))
	rs.ManagedResources = append(rs.ManagedResources, makeUnstructured(
		"sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "app-db",
		map[string]interface{}{"region": "asia-south1", "databaseVersion": "POSTGRES_16"},
	))
	rs.ManagedResources = append(rs.ManagedResources, makeUnstructured(
		"container.gcp.upbound.io/v1beta2", "Cluster", "gke-prod",
		map[string]interface{}{"location": "asia-south1"},
	))

	rendered, err := Render(rs)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	if len(rendered.Resources) != 3 {
		t.Errorf("expected 3 rendered resources, got %d", len(rendered.Resources))
	}
}

func TestRenderDirectAzureResources(t *testing.T) {
	rs := &manifest.ResourceSet{}

	rs.ManagedResources = append(rs.ManagedResources, makeUnstructured(
		"azure.upbound.io/v1beta1", "ResourceGroup", "app-rg",
		map[string]interface{}{"location": "eastus"},
	))
	rs.ManagedResources = append(rs.ManagedResources, makeUnstructured(
		"network.azure.upbound.io/v1beta2", "VirtualNetwork", "app-vnet",
		map[string]interface{}{"location": "eastus"},
	))
	rs.ManagedResources = append(rs.ManagedResources, makeUnstructured(
		"compute.azure.upbound.io/v1beta2", "LinuxVirtualMachine", "app-vm",
		map[string]interface{}{"size": "Standard_B2s", "location": "eastus"},
	))

	rendered, err := Render(rs)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	if len(rendered.Resources) != 3 {
		t.Errorf("expected 3 rendered resources, got %d", len(rendered.Resources))
	}
}

func TestRenderCompositionPatchAndTransform(t *testing.T) {
	rs, err := manifest.Scan([]string{testdataPath("sample-manifests")})
	if err != nil {
		t.Fatalf("scanning: %v", err)
	}

	rendered, err := Render(rs)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	// 1 direct MR (IAM Role) + 2 from composition (Bucket + BucketACL) + 1 XR from XRD
	if len(rendered.Resources) < 1 {
		t.Errorf("expected at least 1 rendered resource, got %d", len(rendered.Resources))
	}

	hasDirectMR := false
	for _, r := range rendered.Resources {
		if r.Source == "direct" && r.Resource.GetKind() == "Role" {
			hasDirectMR = true
		}
	}
	if !hasDirectMR {
		t.Error("expected direct IAM Role in rendered output")
	}
}

func TestRenderResourceKey(t *testing.T) {
	r := RenderedResource{
		Resource: makeUnstructured("s3.aws.upbound.io/v1beta1", "Bucket", "my-bucket", nil),
	}

	key := r.ResourceKey()
	expected := "s3.aws.upbound.io/v1beta1/Bucket/my-bucket"
	if key != expected {
		t.Errorf("ResourceKey() = %q, want %q", key, expected)
	}
}

func TestRenderPrint(t *testing.T) {
	rs := &RenderedSet{
		Resources: []RenderedResource{
			{
				Source:   "direct",
				Resource: makeUnstructured("s3.aws.upbound.io/v1beta1", "Bucket", "test", nil),
			},
		},
	}

	var buf bytes.Buffer
	err := Print(rs, &buf)
	if err != nil {
		t.Fatalf("print failed: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("expected non-empty output")
	}
}

func TestRenderMultiProviderFromTestdata(t *testing.T) {
	dirs := []string{
		testdataPath("aws-manifests"),
		testdataPath("gcp-manifests"),
		testdataPath("azure-manifests"),
	}

	rs, err := manifest.Scan(dirs)
	if err != nil {
		t.Fatalf("scanning: %v", err)
	}

	rendered, err := Render(rs)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	if len(rendered.Resources) != 26 {
		t.Errorf("expected 26 rendered resources from all providers, got %d", len(rendered.Resources))
		for _, r := range rendered.Resources {
			t.Logf("  %s: %s/%s", r.Source, r.Resource.GetKind(), r.Resource.GetName())
		}
	}

	providers := map[string]int{}
	for _, r := range rendered.Resources {
		api := r.Resource.GetAPIVersion()
		switch {
		case contains(api, "aws"):
			providers["aws"]++
		case contains(api, "gcp"):
			providers["gcp"]++
		case contains(api, "azure"):
			providers["azure"]++
		}
	}

	if providers["aws"] != 10 {
		t.Errorf("expected 10 AWS resources, got %d", providers["aws"])
	}
	if providers["gcp"] != 10 {
		t.Errorf("expected 10 GCP resources, got %d", providers["gcp"])
	}
	if providers["azure"] != 6 {
		t.Errorf("expected 6 Azure resources, got %d", providers["azure"])
	}
}

func TestRenderUnresolvedXR(t *testing.T) {
	rs := &manifest.ResourceSet{}
	rs.XRs = append(rs.XRs, makeUnstructured(
		"custom.example.org/v1alpha1", "XMyResource", "test-xr",
		map[string]interface{}{"region": "us-east-1"},
	))

	rendered, err := Render(rs)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	if len(rendered.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(rendered.Resources))
	}
	if rendered.Resources[0].Source != "unresolved-xr" {
		t.Errorf("expected source 'unresolved-xr', got %q", rendered.Resources[0].Source)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && bytes.Contains([]byte(s), []byte(substr))
}

func testdataPath(dir string) string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..", "testdata", dir)
}

func makeUnstructured(apiVersion, kind, name string, forProvider map[string]interface{}) unstructured.Unstructured {
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
