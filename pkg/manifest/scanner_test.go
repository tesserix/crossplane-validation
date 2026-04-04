package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestScanAWSManifests(t *testing.T) {
	rs, err := Scan([]string{testdataPath("aws-manifests")})
	if err != nil {
		t.Fatalf("scanning AWS manifests: %v", err)
	}

	if got := len(rs.ManagedResources); got != 10 {
		t.Errorf("expected 10 AWS managed resources, got %d", got)
		for _, mr := range rs.ManagedResources {
			t.Logf("  %s/%s", mr.GetKind(), mr.GetName())
		}
	}

	kinds := countKinds(rs.ManagedResources)
	assertKindCount(t, kinds, "VPC", 1)
	assertKindCount(t, kinds, "Subnet", 1)
	assertKindCount(t, kinds, "SecurityGroup", 1)
	assertKindCount(t, kinds, "Role", 1)
	assertKindCount(t, kinds, "RolePolicyAttachment", 1)
	assertKindCount(t, kinds, "Policy", 1)
	assertKindCount(t, kinds, "Cluster", 1)
	assertKindCount(t, kinds, "NodeGroup", 1)
	assertKindCount(t, kinds, "Instance", 1)
	assertKindCount(t, kinds, "SubnetGroup", 1)
}

func TestScanGCPManifests(t *testing.T) {
	rs, err := Scan([]string{testdataPath("gcp-manifests")})
	if err != nil {
		t.Fatalf("scanning GCP manifests: %v", err)
	}

	if got := len(rs.ManagedResources); got != 9 {
		t.Errorf("expected 9 GCP managed resources, got %d", got)
		for _, mr := range rs.ManagedResources {
			t.Logf("  %s/%s (%s)", mr.GetKind(), mr.GetName(), mr.GetAPIVersion())
		}
	}

	kinds := countKinds(rs.ManagedResources)
	assertKindCount(t, kinds, "Network", 1)
	assertKindCount(t, kinds, "Subnetwork", 1)
	assertKindCount(t, kinds, "Firewall", 1)
	assertKindCount(t, kinds, "Cluster", 1)
	assertKindCount(t, kinds, "NodePool", 1)
	assertKindCount(t, kinds, "DatabaseInstance", 1)
	assertKindCount(t, kinds, "Database", 1)
	assertKindCount(t, kinds, "User", 1)
	assertKindCount(t, kinds, "ServiceAccount", 1)
}

func TestScanAzureManifests(t *testing.T) {
	rs, err := Scan([]string{testdataPath("azure-manifests")})
	if err != nil {
		t.Fatalf("scanning Azure manifests: %v", err)
	}

	if got := len(rs.ManagedResources); got != 6 {
		t.Errorf("expected 6 Azure managed resources, got %d", got)
		for _, mr := range rs.ManagedResources {
			t.Logf("  %s/%s (%s)", mr.GetKind(), mr.GetName(), mr.GetAPIVersion())
		}
	}

	kinds := countKinds(rs.ManagedResources)
	assertKindCount(t, kinds, "ResourceGroup", 1)
	assertKindCount(t, kinds, "VirtualNetwork", 1)
	assertKindCount(t, kinds, "Subnet", 1)
	assertKindCount(t, kinds, "Account", 1)
	assertKindCount(t, kinds, "Container", 1)
	assertKindCount(t, kinds, "LinuxVirtualMachine", 1)
}

func TestScanMultiProvider(t *testing.T) {
	rs, err := Scan([]string{
		testdataPath("aws-manifests"),
		testdataPath("gcp-manifests"),
		testdataPath("azure-manifests"),
	})
	if err != nil {
		t.Fatalf("scanning multi-provider manifests: %v", err)
	}

	total := len(rs.ManagedResources)
	if total != 25 {
		t.Errorf("expected 25 total managed resources, got %d", total)
	}

	if len(rs.ProviderConfigs) != 0 {
		t.Errorf("expected 0 provider configs, got %d", len(rs.ProviderConfigs))
	}
}

func TestScanCompositionResources(t *testing.T) {
	rs, err := Scan([]string{testdataPath("sample-manifests")})
	if err != nil {
		t.Fatalf("scanning sample manifests: %v", err)
	}

	if len(rs.XRDs) != 1 {
		t.Errorf("expected 1 XRD, got %d", len(rs.XRDs))
	}
	if len(rs.Compositions) != 1 {
		t.Errorf("expected 1 Composition, got %d", len(rs.Compositions))
	}
	if len(rs.ManagedResources) != 1 {
		t.Errorf("expected 1 direct MR, got %d", len(rs.ManagedResources))
	}
}

func TestClassifyProviderConfig(t *testing.T) {
	yaml := `
apiVersion: aws.upbound.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
`
	resources, err := parseMultiDoc([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}

	rs := &ResourceSet{}
	for _, r := range resources {
		rs.classify(r)
	}

	if len(rs.ProviderConfigs) != 1 {
		t.Errorf("expected 1 ProviderConfig, got %d", len(rs.ProviderConfigs))
	}
}

func TestClassifyFunction(t *testing.T) {
	yaml := `
apiVersion: pkg.crossplane.io/v1
kind: Function
metadata:
  name: function-patch-and-transform
spec:
  package: xpkg.upbound.io/crossplane-contrib/function-patch-and-transform:v0.5.0
`
	resources, err := parseMultiDoc([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}

	rs := &ResourceSet{}
	for _, r := range resources {
		rs.classify(r)
	}

	if len(rs.Functions) != 1 {
		t.Errorf("expected 1 Function, got %d", len(rs.Functions))
	}
}

func TestParseMultiDocYAML(t *testing.T) {
	yaml := `
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: bucket-1
---
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: bucket-2
---
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: bucket-3
`
	resources, err := parseMultiDoc([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}

	if len(resources) != 3 {
		t.Errorf("expected 3 resources, got %d", len(resources))
	}
}

func TestParseMultiDocSkipsEmpty(t *testing.T) {
	yaml := `
---

---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test
---

`
	resources, err := parseMultiDoc([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}

	if len(resources) != 1 {
		t.Errorf("expected 1 resource (skipping empty docs), got %d", len(resources))
	}
}

func TestIsManagedResourceOlderAPIVersions(t *testing.T) {
	tests := []struct {
		apiVersion string
		want       bool
	}{
		{"s3.aws.upbound.io/v1beta1", true},
		{"ec2.aws.upbound.io/v1beta1", true},
		{"iam.aws.upbound.io/v1beta1", true},
		{"compute.gcp.upbound.io/v1beta1", true},
		{"container.gcp.upbound.io/v1beta2", true},
		{"network.azure.upbound.io/v1beta2", true},
		{"s3.aws.crossplane.io/v1alpha1", true},
		{"compute.gcp.crossplane.io/v1beta1", true},
		{"network.azure.crossplane.io/v1alpha1", true},
		{"apiextensions.crossplane.io/v1", false},
		{"pkg.crossplane.io/v1", false},
		{"apps/v1", false},
		{"v1", false},
	}

	for _, tt := range tests {
		got := isManagedResource(tt.apiVersion)
		if got != tt.want {
			t.Errorf("isManagedResource(%q) = %v, want %v", tt.apiVersion, got, tt.want)
		}
	}
}

func TestSummary(t *testing.T) {
	rs, _ := Scan([]string{testdataPath("sample-manifests")})
	summary := rs.Summary()

	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

func testdataPath(dir string) string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "..", "..", "testdata", dir)
}

func countKinds(resources []unstructured.Unstructured) map[string]int {
	m := make(map[string]int)
	for _, r := range resources {
		m[r.GetKind()]++
	}
	return m
}

func assertKindCount(t *testing.T, kinds map[string]int, kind string, expected int) {
	t.Helper()
	if got := kinds[kind]; got != expected {
		t.Errorf("expected %d %s resources, got %d", expected, kind, got)
	}
}
