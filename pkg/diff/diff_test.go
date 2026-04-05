package diff

import (
	"strings"
	"testing"

	"github.com/tesserix/crossplane-validation/pkg/renderer"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestComputeCreateAWSResources(t *testing.T) {
	base := &renderer.RenderedSet{}
	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("ec2.aws.upbound.io/v1beta1", "VPC", "main-vpc", map[string]interface{}{
				"region":    "us-east-1",
				"cidrBlock": "10.0.0.0/16",
			}),
			makeMR("iam.aws.upbound.io/v1beta1", "Role", "app-role", map[string]interface{}{
				"assumeRolePolicy": "{}",
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToAdd != 2 {
		t.Errorf("expected 2 adds, got %d", result.Summary.ToAdd)
	}
	if result.Summary.ToChange != 0 || result.Summary.ToDelete != 0 {
		t.Errorf("expected no changes or deletes, got change=%d delete=%d",
			result.Summary.ToChange, result.Summary.ToDelete)
	}

	for _, d := range result.Diffs {
		if d.Action != ActionCreate {
			t.Errorf("expected create action, got %s for %s", d.Action, d.ResourceKey)
		}
	}
}

func TestComputeUpdateGCPResources(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "app-db", map[string]interface{}{
				"region":          "asia-south1",
				"databaseVersion": "POSTGRES_15",
				"tier":            "db-f1-micro",
			}),
			makeMR("compute.gcp.upbound.io/v1beta1", "Network", "main-net", map[string]interface{}{
				"autoCreateSubnetworks": false,
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "app-db", map[string]interface{}{
				"region":          "asia-south1",
				"databaseVersion": "POSTGRES_16",
				"tier":            "db-custom-4-16384",
			}),
			makeMR("compute.gcp.upbound.io/v1beta1", "Network", "main-net", map[string]interface{}{
				"autoCreateSubnetworks": false,
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Errorf("expected 1 change, got %d", result.Summary.ToChange)
	}
	if result.Summary.NoOp != 1 {
		t.Errorf("expected 1 no-op, got %d", result.Summary.NoOp)
	}

	for _, d := range result.Diffs {
		if d.Kind == "DatabaseInstance" {
			if len(d.FieldChanges) != 2 {
				t.Errorf("expected 2 field changes for DatabaseInstance, got %d", len(d.FieldChanges))
			}
		}
	}
}

func TestComputeDeleteAzureResources(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("azure.upbound.io/v1beta1", "ResourceGroup", "old-rg", map[string]interface{}{
				"location": "eastus",
			}),
			makeMR("network.azure.upbound.io/v1beta2", "VirtualNetwork", "old-vnet", map[string]interface{}{
				"location": "eastus",
			}),
			makeMR("storage.azure.upbound.io/v1beta2", "Account", "keep-storage", map[string]interface{}{
				"location": "eastus",
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("storage.azure.upbound.io/v1beta2", "Account", "keep-storage", map[string]interface{}{
				"location": "eastus",
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToDelete != 2 {
		t.Errorf("expected 2 deletes, got %d", result.Summary.ToDelete)
	}
	if result.Summary.NoOp != 1 {
		t.Errorf("expected 1 no-op, got %d", result.Summary.NoOp)
	}
}

func TestComputeMixedMultiProvider(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("s3.aws.upbound.io/v1beta1", "Bucket", "logs", map[string]interface{}{
				"region": "us-east-1",
				"acl":    "private",
			}),
			makeMR("compute.gcp.upbound.io/v1beta1", "Instance", "web-server", map[string]interface{}{
				"machineType": "e2-micro",
				"zone":        "asia-south1-a",
			}),
			makeMR("azure.upbound.io/v1beta1", "ResourceGroup", "deprecated-rg", map[string]interface{}{
				"location": "westus",
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			// AWS bucket updated
			makeMR("s3.aws.upbound.io/v1beta1", "Bucket", "logs", map[string]interface{}{
				"region": "us-east-1",
				"acl":    "public-read",
			}),
			// GCP instance unchanged
			makeMR("compute.gcp.upbound.io/v1beta1", "Instance", "web-server", map[string]interface{}{
				"machineType": "e2-micro",
				"zone":        "asia-south1-a",
			}),
			// Azure RG deleted (not in target)
			// New EKS cluster added
			makeMR("eks.aws.upbound.io/v1beta1", "Cluster", "new-cluster", map[string]interface{}{
				"region":  "us-east-1",
				"version": "1.29",
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToAdd != 1 {
		t.Errorf("expected 1 add, got %d", result.Summary.ToAdd)
	}
	if result.Summary.ToChange != 1 {
		t.Errorf("expected 1 change, got %d", result.Summary.ToChange)
	}
	if result.Summary.ToDelete != 1 {
		t.Errorf("expected 1 delete, got %d", result.Summary.ToDelete)
	}
	if result.Summary.NoOp != 1 {
		t.Errorf("expected 1 no-op, got %d", result.Summary.NoOp)
	}

	summary := result.Summary.String()
	if summary != "1 to add, 1 to change, 1 to destroy" {
		t.Errorf("unexpected summary: %s", summary)
	}
}

func TestComputeSortOrder(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("s3.aws.upbound.io/v1beta1", "Bucket", "delete-me", map[string]interface{}{}),
			makeMR("s3.aws.upbound.io/v1beta1", "Bucket", "update-me", map[string]interface{}{
				"acl": "private",
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("s3.aws.upbound.io/v1beta1", "Bucket", "update-me", map[string]interface{}{
				"acl": "public-read",
			}),
			makeMR("s3.aws.upbound.io/v1beta1", "Bucket", "create-me", map[string]interface{}{}),
		},
	}

	result := Compute(base, target)

	if len(result.Diffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d", len(result.Diffs))
	}

	// Deletes first, then updates, then creates
	if result.Diffs[0].Action != ActionDelete {
		t.Errorf("expected first diff to be delete, got %s", result.Diffs[0].Action)
	}
	if result.Diffs[1].Action != ActionUpdate {
		t.Errorf("expected second diff to be update, got %s", result.Diffs[1].Action)
	}
	if result.Diffs[2].Action != ActionCreate {
		t.Errorf("expected third diff to be create, got %s", result.Diffs[2].Action)
	}
}

func TestComputeNestedFieldChanges(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("iam.aws.upbound.io/v1beta1", "Role", "app", map[string]interface{}{
				"tags": map[string]interface{}{
					"Environment": "staging",
					"Team":        "platform",
					"Deprecated":  "true",
				},
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("iam.aws.upbound.io/v1beta1", "Role", "app", map[string]interface{}{
				"tags": map[string]interface{}{
					"Environment": "production",
					"Team":        "platform",
					"CostCenter":  "engineering",
				},
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Fatalf("expected 1 change, got %d", result.Summary.ToChange)
	}

	changes := result.Diffs[0].FieldChanges
	if len(changes) != 3 {
		t.Errorf("expected 3 field changes (update Env, delete Deprecated, add CostCenter), got %d", len(changes))
		for _, c := range changes {
			t.Logf("  %s: %s (%v -> %v)", c.Action, c.Path, c.OldValue, c.NewValue)
		}
	}
}

func TestComputeEmptyBase(t *testing.T) {
	result := Compute(nil, &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("s3.aws.upbound.io/v1beta1", "Bucket", "new", map[string]interface{}{}),
		},
	})

	if result.Summary.ToAdd != 1 {
		t.Errorf("expected 1 add from nil base, got %d", result.Summary.ToAdd)
	}
}

func TestComputeEmptyTarget(t *testing.T) {
	result := Compute(&renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("s3.aws.upbound.io/v1beta1", "Bucket", "old", map[string]interface{}{}),
		},
	}, nil)

	if result.Summary.ToDelete != 1 {
		t.Errorf("expected 1 delete from nil target, got %d", result.Summary.ToDelete)
	}
}

func TestComputeBothEmpty(t *testing.T) {
	result := Compute(nil, nil)
	if len(result.Diffs) != 0 {
		t.Errorf("expected no diffs from nil inputs, got %d", len(result.Diffs))
	}
}

func TestComputeAWSIAMPolicyChange(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("iam.aws.upbound.io/v1beta1", "RolePolicyAttachment", "cluster-policy", map[string]interface{}{
				"policyArn": "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy",
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("iam.aws.upbound.io/v1beta1", "RolePolicyAttachment", "cluster-policy", map[string]interface{}{
				"policyArn": "arn:aws:iam::aws:policy/AdministratorAccess",
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Fatalf("expected 1 change, got %d", result.Summary.ToChange)
	}

	fc := result.Diffs[0].FieldChanges[0]
	if fc.OldValue != "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy" {
		t.Errorf("unexpected old value: %v", fc.OldValue)
	}
	if fc.NewValue != "arn:aws:iam::aws:policy/AdministratorAccess" {
		t.Errorf("unexpected new value: %v", fc.NewValue)
	}
}

func TestComputeGCPInstanceResize(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "db", map[string]interface{}{
				"tier":   "db-f1-micro",
				"region": "asia-south1",
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "db", map[string]interface{}{
				"tier":   "db-n1-standard-2",
				"region": "asia-south1",
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Fatalf("expected 1 change, got %d", result.Summary.ToChange)
	}
	if len(result.Diffs[0].FieldChanges) != 1 {
		t.Errorf("expected 1 field change (tier only), got %d", len(result.Diffs[0].FieldChanges))
	}
}

func TestComputeAzureStorageTierChange(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("storage.azure.upbound.io/v1beta2", "Account", "data", map[string]interface{}{
				"accountTier":            "Standard",
				"accountReplicationType": "LRS",
				"location":               "eastus",
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("storage.azure.upbound.io/v1beta2", "Account", "data", map[string]interface{}{
				"accountTier":            "Premium",
				"accountReplicationType": "GRS",
				"location":               "eastus",
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Fatalf("expected 1 change, got %d", result.Summary.ToChange)
	}
	if len(result.Diffs[0].FieldChanges) != 2 {
		t.Errorf("expected 2 field changes (tier + replication), got %d", len(result.Diffs[0].FieldChanges))
	}
}

func TestIsSensitiveField(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"spec.forProvider.password", true},
		{"spec.forProvider.masterPassword", true},
		{"spec.forProvider.apiKey", true},
		{"spec.forProvider.apikey", true},
		{"spec.forProvider.secretKey", true},
		{"spec.forProvider.accessKey", true},
		{"spec.forProvider.privateKey", true},
		{"spec.forProvider.connectionString", true},
		{"spec.forProvider.token", true},
		{"spec.forProvider.secret", true},
		{"spec.forProvider.credential", true},
		{"spec.forProvider.cert", true},
		{"spec.forProvider.certificate", true},
		{"spec.forProvider.dbPassword", true},
		{"spec.forProvider.region", false},
		{"spec.forProvider.cidrBlock", false},
		{"spec.forProvider.tier", false},
		{"spec.forProvider.location", false},
	}

	for _, tc := range tests {
		got := isSensitiveField(tc.path)
		if got != tc.expected {
			t.Errorf("isSensitiveField(%q) = %v, want %v", tc.path, got, tc.expected)
		}
	}
}

func TestSensitiveFieldMaskingOnCreate(t *testing.T) {
	ShowSensitive = false
	defer func() { ShowSensitive = false }()

	base := &renderer.RenderedSet{}
	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "db", map[string]interface{}{
				"region":         "us-east-1",
				"masterPassword": "PLACEHOLDER_TEST_VALUE",
				"apiKey":         "PLACEHOLDER_API_KEY",
			}),
		},
	}

	result := Compute(base, target)

	for _, d := range result.Diffs {
		for _, fc := range d.FieldChanges {
			if fc.Path == "spec.forProvider.masterPassword" || fc.Path == "spec.forProvider.apiKey" {
				if fc.NewValue != "(sensitive value)" {
					t.Errorf("expected masked create value for %s, got %v", fc.Path, fc.NewValue)
				}
			}
			if fc.Path == "spec.forProvider.region" {
				if fc.NewValue != "us-east-1" {
					t.Errorf("expected plain region value, got %v", fc.NewValue)
				}
			}
		}
	}
}

func TestSensitiveFieldMaskingOnUpdate(t *testing.T) {
	ShowSensitive = false
	defer func() { ShowSensitive = false }()

	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "db", map[string]interface{}{
				"region":   "us-east-1",
				"password": "PLACEHOLDER_OLD",
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "db", map[string]interface{}{
				"region":   "us-east-1",
				"password": "PLACEHOLDER_NEW",
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Fatalf("expected 1 change, got %d", result.Summary.ToChange)
	}

	fc := result.Diffs[0].FieldChanges[0]
	if fc.OldValue != "(sensitive value)" {
		t.Errorf("expected masked old value, got %v", fc.OldValue)
	}
	if fc.NewValue != "(sensitive value changed)" {
		t.Errorf("expected masked new value, got %v", fc.NewValue)
	}
}

func TestSensitiveFieldMaskingOnDelete(t *testing.T) {
	ShowSensitive = false
	defer func() { ShowSensitive = false }()

	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "db", map[string]interface{}{
				"region": "us-east-1",
				"token":  "PLACEHOLDER_TOKEN",
			}),
		},
	}

	target := &renderer.RenderedSet{}

	result := Compute(base, target)

	for _, d := range result.Diffs {
		for _, fc := range d.FieldChanges {
			if fc.Path == "spec.forProvider.token" {
				if fc.OldValue != "(sensitive value removed)" {
					t.Errorf("expected masked delete value for token, got %v", fc.OldValue)
				}
			}
		}
	}
}

func TestShowSensitiveBypassesMasking(t *testing.T) {
	ShowSensitive = true
	defer func() { ShowSensitive = false }()

	base := &renderer.RenderedSet{}
	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "db", map[string]interface{}{
				"password": "PLACEHOLDER_VISIBLE",
			}),
		},
	}

	result := Compute(base, target)

	for _, d := range result.Diffs {
		for _, fc := range d.FieldChanges {
			if fc.Path == "spec.forProvider.password" {
				if fc.NewValue != "PLACEHOLDER_VISIBLE" {
					t.Errorf("expected plain value with ShowSensitive=true, got %v", fc.NewValue)
				}
			}
		}
	}
}

func TestNamespaceScopedResourcesDontCollide(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMRWithNamespace("v1", "ConfigMap", "config", "namespace-a", map[string]interface{}{
				"data": "old-a",
			}),
			makeMRWithNamespace("v1", "ConfigMap", "config", "namespace-b", map[string]interface{}{
				"data": "old-b",
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMRWithNamespace("v1", "ConfigMap", "config", "namespace-a", map[string]interface{}{
				"data": "new-a",
			}),
			makeMRWithNamespace("v1", "ConfigMap", "config", "namespace-b", map[string]interface{}{
				"data": "old-b",
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Errorf("expected 1 change, got %d", result.Summary.ToChange)
	}
	if result.Summary.NoOp != 1 {
		t.Errorf("expected 1 no-op, got %d", result.Summary.NoOp)
	}

	// Verify the changed resource is from namespace-a
	for _, d := range result.Diffs {
		if d.Action == ActionUpdate {
			if d.Namespace != "namespace-a" {
				t.Errorf("expected update in namespace-a, got namespace %q", d.Namespace)
			}
		}
	}
}

func TestNamespacedResourceKeyFormat(t *testing.T) {
	r := makeMRWithNamespace("v1", "ConfigMap", "test", "my-ns", nil)
	key := r.ResourceKey()
	expected := "v1/ConfigMap/my-ns/test"
	if key != expected {
		t.Errorf("ResourceKey() = %q, want %q", key, expected)
	}

	// Cluster-scoped (no namespace)
	r2 := makeMR("v1", "Namespace", "test", nil)
	key2 := r2.ResourceKey()
	expected2 := "v1/Namespace//test"
	if key2 != expected2 {
		t.Errorf("ResourceKey() = %q, want %q", key2, expected2)
	}
}

func TestDiffPrimitiveArrays(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("compute.gcp.upbound.io/v1beta1", "Firewall", "web", map[string]interface{}{
				"sourceRanges": []interface{}{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10"},
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("compute.gcp.upbound.io/v1beta1", "Firewall", "web", map[string]interface{}{
				"sourceRanges": []interface{}{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "203.0.113.0/24"},
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Fatalf("expected 1 change, got %d", result.Summary.ToChange)
	}

	changes := result.Diffs[0].FieldChanges
	// Should have separate add and remove entries, not an atomic update
	hasAdd := false
	hasRemove := false
	for _, fc := range changes {
		if fc.Action == ActionCreate {
			hasAdd = true
			newVal, ok := fc.NewValue.(string)
			if !ok || !containsSubstring(newVal, "203.0.113.0/24") {
				t.Errorf("expected added element 203.0.113.0/24, got %v", fc.NewValue)
			}
		}
		if fc.Action == ActionDelete {
			hasRemove = true
			oldVal, ok := fc.OldValue.(string)
			if !ok || !containsSubstring(oldVal, "100.64.0.0/10") {
				t.Errorf("expected removed element 100.64.0.0/10, got %v", fc.OldValue)
			}
		}
	}
	if !hasAdd || !hasRemove {
		t.Errorf("expected both add and remove changes for primitive array diff, got changes: %+v", changes)
	}
}

func TestDiffShortArraysAtomic(t *testing.T) {
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("compute.gcp.upbound.io/v1beta1", "Firewall", "web", map[string]interface{}{
				"sourceRanges": []interface{}{"10.0.0.0/8"},
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("compute.gcp.upbound.io/v1beta1", "Firewall", "web", map[string]interface{}{
				"sourceRanges": []interface{}{"192.168.0.0/16"},
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Fatalf("expected 1 change, got %d", result.Summary.ToChange)
	}

	// Short arrays should produce a single atomic update
	changes := result.Diffs[0].FieldChanges
	if len(changes) != 1 {
		t.Fatalf("expected 1 field change for short array, got %d", len(changes))
	}
	if changes[0].Action != ActionUpdate {
		t.Errorf("expected update action for short array, got %s", changes[0].Action)
	}
}

func TestDiffMapArraysByKey(t *testing.T) {
	// Use unique keys per element so matching works cleanly.
	// Need 4+ elements per array to bypass the short-array atomic fallback.
	base := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("ec2.aws.upbound.io/v1beta1", "SecurityGroup", "web-sg", map[string]interface{}{
				"ingress": []interface{}{
					map[string]interface{}{"protocol": "tcp", "fromPort": float64(80), "toPort": float64(80), "cidr": "0.0.0.0/0"},
					map[string]interface{}{"protocol": "udp", "fromPort": float64(53), "toPort": float64(53), "cidr": "10.0.0.0/8"},
					map[string]interface{}{"protocol": "ssh", "fromPort": float64(22), "toPort": float64(22), "cidr": "10.0.0.0/8"},
					map[string]interface{}{"protocol": "dns", "fromPort": float64(53), "toPort": float64(53), "cidr": "10.0.0.0/8"},
				},
			}),
		},
	}

	target := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeMR("ec2.aws.upbound.io/v1beta1", "SecurityGroup", "web-sg", map[string]interface{}{
				"ingress": []interface{}{
					map[string]interface{}{"protocol": "tcp", "fromPort": float64(80), "toPort": float64(80), "cidr": "10.0.0.0/8"},
					map[string]interface{}{"protocol": "udp", "fromPort": float64(53), "toPort": float64(53), "cidr": "10.0.0.0/8"},
					map[string]interface{}{"protocol": "icmp", "fromPort": float64(-1), "toPort": float64(-1), "cidr": "10.0.0.0/8"},
					map[string]interface{}{"protocol": "dns", "fromPort": float64(53), "toPort": float64(53), "cidr": "10.0.0.0/8"},
				},
			}),
		},
	}

	result := Compute(base, target)

	if result.Summary.ToChange != 1 {
		t.Fatalf("expected 1 change, got %d", result.Summary.ToChange)
	}

	changes := result.Diffs[0].FieldChanges
	// tcp matched by key: cidr changed. ssh removed. icmp added.
	hasFieldUpdate := false
	hasRemove := false
	hasAdd := false
	for _, fc := range changes {
		if fc.Action == ActionUpdate {
			hasFieldUpdate = true
		}
		if fc.Action == ActionDelete {
			hasRemove = true
		}
		if fc.Action == ActionCreate {
			hasAdd = true
		}
	}
	if !hasFieldUpdate {
		t.Errorf("expected field-level update for matched map element, changes: %+v", changes)
	}
	if !hasRemove {
		t.Errorf("expected remove for unmatched old element, changes: %+v", changes)
	}
	if !hasAdd {
		t.Errorf("expected add for unmatched new element, changes: %+v", changes)
	}
}

func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

func makeMR(apiVersion, kind, name string, forProvider map[string]interface{}) renderer.RenderedResource {
	obj := map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": name},
		"spec":       map[string]interface{}{"forProvider": forProvider},
	}
	return renderer.RenderedResource{
		Source:   "direct",
		Resource: unstructured.Unstructured{Object: obj},
	}
}

func makeMRWithNamespace(apiVersion, kind, name, namespace string, forProvider map[string]interface{}) renderer.RenderedResource {
	if forProvider == nil {
		forProvider = map[string]interface{}{}
	}
	obj := map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
		"spec":       map[string]interface{}{"forProvider": forProvider},
	}
	return renderer.RenderedResource{
		Source:   "direct",
		Resource: unstructured.Unstructured{Object: obj},
	}
}
