package hcl

import (
	"strings"
	"testing"

	"github.com/tesserix/crossplane-validation/pkg/config"
	"github.com/tesserix/crossplane-validation/pkg/renderer"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestMapResourceTypeAWS(t *testing.T) {
	tests := []struct {
		apiVersion string
		kind       string
		want       string
	}{
		{"s3.aws.upbound.io/v1beta1", "Bucket", "aws_s3_bucket"},
		{"s3.aws.upbound.io/v1beta1", "BucketPolicy", "aws_s3_bucket_policy"},
		{"s3.aws.upbound.io/v1beta1", "BucketACL", "aws_s3_bucket_acl"},
		{"s3.aws.upbound.io/v1beta1", "BucketVersioning", "aws_s3_bucket_versioning"},
		{"iam.aws.upbound.io/v1beta1", "Role", "aws_iam_role"},
		{"iam.aws.upbound.io/v1beta1", "RolePolicy", "aws_iam_role_policy"},
		{"iam.aws.upbound.io/v1beta1", "RolePolicyAttachment", "aws_iam_role_policy_attachment"},
		{"iam.aws.upbound.io/v1beta1", "Policy", "aws_iam_policy"},
		{"iam.aws.upbound.io/v1beta1", "User", "aws_iam_user"},
		{"iam.aws.upbound.io/v1beta1", "Group", "aws_iam_group"},
		{"ec2.aws.upbound.io/v1beta1", "VPC", "aws_vpc"},
		{"ec2.aws.upbound.io/v1beta1", "Subnet", "aws_subnet"},
		{"ec2.aws.upbound.io/v1beta1", "SecurityGroup", "aws_security_group"},
		{"ec2.aws.upbound.io/v1beta1", "Instance", "aws_instance"},
		{"ec2.aws.upbound.io/v1beta1", "InternetGateway", "aws_internet_gateway"},
		{"ec2.aws.upbound.io/v1beta1", "RouteTable", "aws_route_table"},
		{"ec2.aws.upbound.io/v1beta1", "NATGateway", "aws_nat_gateway"},
		{"ec2.aws.upbound.io/v1beta1", "EIP", "aws_eip"},
		{"rds.aws.upbound.io/v1beta1", "Instance", "aws_db_instance"},
		{"rds.aws.upbound.io/v1beta1", "Cluster", "aws_rds_cluster"},
		{"rds.aws.upbound.io/v1beta1", "SubnetGroup", "aws_db_subnet_group"},
		{"eks.aws.upbound.io/v1beta1", "Cluster", "aws_eks_cluster"},
		{"eks.aws.upbound.io/v1beta1", "NodeGroup", "aws_eks_node_group"},
	}

	for _, tt := range tests {
		got, err := mapResourceType(tt.apiVersion, tt.kind)
		if err != nil {
			t.Errorf("mapResourceType(%s, %s) error: %v", tt.apiVersion, tt.kind, err)
			continue
		}
		if got != tt.want {
			t.Errorf("mapResourceType(%s, %s) = %q, want %q", tt.apiVersion, tt.kind, got, tt.want)
		}
	}
}

func TestMapResourceTypeGCP(t *testing.T) {
	tests := []struct {
		apiVersion string
		kind       string
		want       string
	}{
		{"storage.gcp.upbound.io/v1beta1", "Bucket", "google_storage_bucket"},
		{"compute.gcp.upbound.io/v1beta1", "Instance", "google_compute_instance"},
		{"compute.gcp.upbound.io/v1beta1", "Network", "google_compute_network"},
		{"compute.gcp.upbound.io/v1beta1", "Subnetwork", "google_compute_subnetwork"},
		{"compute.gcp.upbound.io/v1beta1", "Firewall", "google_compute_firewall"},
		{"container.gcp.upbound.io/v1beta2", "Cluster", "google_container_cluster"},
		{"container.gcp.upbound.io/v1beta2", "NodePool", "google_container_node_pool"},
		{"sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "google_sql_database_instance"},
		{"sql.gcp.upbound.io/v1beta1", "Database", "google_sql_database"},
		{"sql.gcp.upbound.io/v1beta1", "User", "google_sql_user"},
		{"iam.gcp.upbound.io/v1beta1", "ServiceAccount", "google_service_account"},
	}

	for _, tt := range tests {
		got, err := mapResourceType(tt.apiVersion, tt.kind)
		if err != nil {
			t.Errorf("mapResourceType(%s, %s) error: %v", tt.apiVersion, tt.kind, err)
			continue
		}
		if got != tt.want {
			t.Errorf("mapResourceType(%s, %s) = %q, want %q", tt.apiVersion, tt.kind, got, tt.want)
		}
	}
}

func TestMapResourceTypeAzure(t *testing.T) {
	tests := []struct {
		apiVersion string
		kind       string
		want       string
	}{
		{"azure.upbound.io/v1beta1", "ResourceGroup", "azurerm_resource_group"},
		{"storage.azure.upbound.io/v1beta2", "Account", "azurerm_storage_account"},
		{"storage.azure.upbound.io/v1beta1", "Container", "azurerm_storage_container"},
		{"network.azure.upbound.io/v1beta2", "VirtualNetwork", "azurerm_virtual_network"},
		{"network.azure.upbound.io/v1beta2", "Subnet", "azurerm_subnet"},
		{"compute.azure.upbound.io/v1beta2", "LinuxVirtualMachine", "azurerm_linux_virtual_machine"},
	}

	for _, tt := range tests {
		got, err := mapResourceType(tt.apiVersion, tt.kind)
		if err != nil {
			t.Errorf("mapResourceType(%s, %s) error: %v", tt.apiVersion, tt.kind, err)
			continue
		}
		if got != tt.want {
			t.Errorf("mapResourceType(%s, %s) = %q, want %q", tt.apiVersion, tt.kind, got, tt.want)
		}
	}
}

func TestMapResourceTypeFallback(t *testing.T) {
	got, _ := mapResourceType("lambda.aws.upbound.io/v1beta1", "Function")
	if !strings.HasPrefix(got, "aws_") {
		t.Errorf("fallback for AWS resource should start with aws_, got %q", got)
	}

	got, _ = mapResourceType("pubsub.gcp.upbound.io/v1beta1", "Topic")
	if !strings.HasPrefix(got, "google_") {
		t.Errorf("fallback for GCP resource should start with google_, got %q", got)
	}

	got, _ = mapResourceType("keyvault.azure.upbound.io/v1beta1", "Vault")
	if !strings.HasPrefix(got, "azurerm_") {
		t.Errorf("fallback for Azure resource should start with azurerm_, got %q", got)
	}
}

func TestConvertAWSResources(t *testing.T) {
	rs := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeRenderedResource("ec2.aws.upbound.io/v1beta1", "VPC", "main-vpc", map[string]interface{}{
				"region":             "us-east-1",
				"cidrBlock":          "10.0.0.0/16",
				"enableDnsHostnames": true,
			}),
			makeRenderedResource("iam.aws.upbound.io/v1beta1", "Role", "app-role", map[string]interface{}{
				"assumeRolePolicy": "{}",
			}),
			makeRenderedResource("eks.aws.upbound.io/v1beta1", "Cluster", "prod", map[string]interface{}{
				"region":  "us-east-1",
				"version": "1.29",
			}),
		},
	}

	providers := map[string]config.Provider{
		"aws": {Region: "us-east-1"},
	}

	cs, err := Convert(rs, providers)
	if err != nil {
		t.Fatalf("converting AWS resources: %v", err)
	}

	if len(cs.ResourceBlocks) != 3 {
		t.Errorf("expected 3 resource blocks, got %d", len(cs.ResourceBlocks))
	}

	labels := map[string]bool{}
	for _, rb := range cs.ResourceBlocks {
		labels[rb.Label] = true
	}

	for _, expected := range []string{"aws_vpc", "aws_iam_role", "aws_eks_cluster"} {
		if !labels[expected] {
			t.Errorf("missing resource type %q in converted output", expected)
		}
	}
}

func TestConvertGCPResources(t *testing.T) {
	rs := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeRenderedResource("compute.gcp.upbound.io/v1beta1", "Network", "main-net", map[string]interface{}{
				"autoCreateSubnetworks": false,
				"project":               "my-project",
			}),
			makeRenderedResource("sql.gcp.upbound.io/v1beta2", "DatabaseInstance", "db", map[string]interface{}{
				"region":          "asia-south1",
				"databaseVersion": "POSTGRES_16",
			}),
			makeRenderedResource("container.gcp.upbound.io/v1beta2", "Cluster", "gke", map[string]interface{}{
				"location": "asia-south1",
				"project":  "my-project",
			}),
		},
	}

	providers := map[string]config.Provider{
		"gcp": {Project: "my-project"},
	}

	cs, err := Convert(rs, providers)
	if err != nil {
		t.Fatalf("converting GCP resources: %v", err)
	}

	if len(cs.ResourceBlocks) != 3 {
		t.Errorf("expected 3 resource blocks, got %d", len(cs.ResourceBlocks))
	}

	labels := map[string]bool{}
	for _, rb := range cs.ResourceBlocks {
		labels[rb.Label] = true
	}

	for _, expected := range []string{"google_compute_network", "google_sql_database_instance", "google_container_cluster"} {
		if !labels[expected] {
			t.Errorf("missing resource type %q in converted output", expected)
		}
	}
}

func TestConvertAzureResources(t *testing.T) {
	rs := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeRenderedResource("azure.upbound.io/v1beta1", "ResourceGroup", "rg", map[string]interface{}{
				"location": "eastus",
			}),
			makeRenderedResource("network.azure.upbound.io/v1beta2", "VirtualNetwork", "vnet", map[string]interface{}{
				"location": "eastus",
			}),
			makeRenderedResource("storage.azure.upbound.io/v1beta2", "Account", "storage", map[string]interface{}{
				"location":               "eastus",
				"accountTier":            "Standard",
				"accountReplicationType": "GRS",
			}),
		},
	}

	providers := map[string]config.Provider{
		"azure": {},
	}

	cs, err := Convert(rs, providers)
	if err != nil {
		t.Fatalf("converting Azure resources: %v", err)
	}

	if len(cs.ResourceBlocks) != 3 {
		t.Errorf("expected 3 resource blocks, got %d", len(cs.ResourceBlocks))
	}

	labels := map[string]bool{}
	for _, rb := range cs.ResourceBlocks {
		labels[rb.Label] = true
	}

	for _, expected := range []string{"azurerm_resource_group", "azurerm_virtual_network", "azurerm_storage_account"} {
		if !labels[expected] {
			t.Errorf("missing resource type %q in converted output", expected)
		}
	}
}

func TestConvertMultiProviderToHCL(t *testing.T) {
	rs := &renderer.RenderedSet{
		Resources: []renderer.RenderedResource{
			makeRenderedResource("s3.aws.upbound.io/v1beta1", "Bucket", "logs", map[string]interface{}{
				"region": "us-east-1",
			}),
			makeRenderedResource("storage.gcp.upbound.io/v1beta1", "Bucket", "assets", map[string]interface{}{
				"location": "US",
				"project":  "my-project",
			}),
			makeRenderedResource("storage.azure.upbound.io/v1beta2", "Account", "data", map[string]interface{}{
				"location":    "eastus",
				"accountTier": "Standard",
			}),
		},
	}

	providers := map[string]config.Provider{
		"aws":   {Region: "us-east-1"},
		"gcp":   {Project: "my-project"},
		"azure": {},
	}

	cs, err := Convert(rs, providers)
	if err != nil {
		t.Fatal(err)
	}

	hclOutput := cs.ToHCL()

	for _, expected := range []string{
		`resource "aws_s3_bucket"`,
		`resource "google_storage_bucket"`,
		`resource "azurerm_storage_account"`,
		`provider "aws"`,
		`provider "google"`,
		`provider "azurerm"`,
		`source = "hashicorp/aws"`,
		`source = "hashicorp/google"`,
		`source = "hashicorp/azurerm"`,
	} {
		if !strings.Contains(hclOutput, expected) {
			t.Errorf("HCL output missing %q", expected)
		}
	}
}

func TestProviderMapping(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"aws", "aws"},
		{"gcp", "google"},
		{"azure", "azurerm"},
		{"custom", "custom"},
	}

	for _, tt := range tests {
		if got := mapProviderName(tt.input); got != tt.want {
			t.Errorf("mapProviderName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestProviderSource(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"aws", "hashicorp/aws"},
		{"google", "hashicorp/google"},
		{"azurerm", "hashicorp/azurerm"},
	}

	for _, tt := range tests {
		if got := providerSource(tt.input); got != tt.want {
			t.Errorf("providerSource(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCamelToSnake(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"cidrBlock", "cidr_block"},
		{"enableDnsHostnames", "enable_dns_hostnames"},
		{"accountReplicationType", "account_replication_type"},
		{"vpcId", "vpc_id"},
		{"simple", "simple"},
		{"IPAddress", "i_p_address"},
	}

	for _, tt := range tests {
		if got := camelToSnake(tt.input); got != tt.want {
			t.Errorf("camelToSnake(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFlattenForTerraformSkipsRefs(t *testing.T) {
	input := map[string]interface{}{
		"region":    "us-east-1",
		"vpcIdRef":  map[string]interface{}{"name": "my-vpc"},
		"subnetIds": []interface{}{"a", "b"},
		"vpcIdSelector": map[string]interface{}{
			"matchLabels": map[string]interface{}{"env": "prod"},
		},
		"tags": map[string]interface{}{
			"Environment": "prod",
		},
	}

	result := flattenForTerraform(input)

	if _, ok := result["vpc_id_ref"]; ok {
		t.Error("expected Ref field to be stripped")
	}
	if _, ok := result["vpc_id_selector"]; ok {
		t.Error("expected Selector field to be stripped")
	}
	if _, ok := result["region"]; !ok {
		t.Error("expected region to be preserved")
	}
	if _, ok := result["tags"]; !ok {
		t.Error("expected tags to be preserved")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my-bucket", "my_bucket"},
		{"app.storage.main", "app_storage_main"},
		{"simple", "simple"},
		{"a-b.c-d", "a_b_c_d"},
	}

	for _, tt := range tests {
		if got := sanitizeName(tt.input); got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func makeRenderedResource(apiVersion, kind, name string, forProvider map[string]interface{}) renderer.RenderedResource {
	obj := map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]interface{}{
			"name": name,
		},
		"spec": map[string]interface{}{
			"forProvider": forProvider,
		},
	}
	return renderer.RenderedResource{
		Source:   "direct",
		Resource: unstructured.Unstructured{Object: obj},
	}
}
