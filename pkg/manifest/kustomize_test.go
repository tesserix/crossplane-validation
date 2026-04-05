package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanWithKustomize(t *testing.T) {
	rs, err := ScanWithKustomize([]string{kustomizeTestPath("")})
	if err != nil {
		t.Fatalf("ScanWithKustomize: %v", err)
	}

	if len(rs.XRDs) != 1 {
		t.Errorf("expected 1 XRD, got %d", len(rs.XRDs))
	}
	if len(rs.Compositions) != 1 {
		t.Errorf("expected 1 Composition, got %d", len(rs.Compositions))
	}
	if len(rs.XRs) != 1 {
		t.Errorf("expected 1 XR, got %d", len(rs.XRs))
	}
	if len(rs.Functions) != 1 {
		t.Errorf("expected 1 Function, got %d", len(rs.Functions))
	}
}

func TestScanWithKustomizeFollowsResourcePaths(t *testing.T) {
	rs, err := ScanWithKustomize([]string{kustomizeTestPath("compositions")})
	if err != nil {
		t.Fatalf("ScanWithKustomize: %v", err)
	}

	if len(rs.XRDs) != 1 {
		t.Errorf("expected 1 XRD from compositions/, got %d", len(rs.XRDs))
	}
	if len(rs.Compositions) != 1 {
		t.Errorf("expected 1 Composition from compositions/, got %d", len(rs.Compositions))
	}
	if len(rs.Functions) != 0 {
		t.Errorf("expected 0 Functions from compositions/ only, got %d", len(rs.Functions))
	}
}

func TestScanWithKustomizeMultipleDirs(t *testing.T) {
	rs, err := ScanWithKustomize([]string{
		kustomizeTestPath("compositions"),
		kustomizeTestPath("claims"),
		kustomizeTestPath("functions"),
	})
	if err != nil {
		t.Fatalf("ScanWithKustomize: %v", err)
	}

	if len(rs.XRDs) != 1 {
		t.Errorf("expected 1 XRD, got %d", len(rs.XRDs))
	}
	if len(rs.XRs) != 1 {
		t.Errorf("expected 1 XR, got %d", len(rs.XRs))
	}
	if len(rs.Functions) != 1 {
		t.Errorf("expected 1 Function, got %d", len(rs.Functions))
	}
}

func TestScanWithKustomizeFallsBackToPlainScan(t *testing.T) {
	// testdata/aws-manifests has no kustomization.yaml — should fall back to plain scan
	rs, err := ScanWithKustomize([]string{testdataPath("aws-manifests")})
	if err != nil {
		t.Fatalf("ScanWithKustomize fallback: %v", err)
	}

	if len(rs.ManagedResources) != 10 {
		t.Errorf("expected 10 AWS MRs from fallback scan, got %d", len(rs.ManagedResources))
	}
}

func TestScanWithKustomizeSkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kustomization.yaml"), `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - resource.yaml
  - readme.txt
`)
	writeFile(t, filepath.Join(dir, "resource.yaml"), `
apiVersion: s3.aws.upbound.io/v1beta1
kind: Bucket
metadata:
  name: test
`)
	writeFile(t, filepath.Join(dir, "readme.txt"), "not a yaml file")

	rs, err := ScanWithKustomize([]string{dir})
	if err != nil {
		t.Fatalf("ScanWithKustomize: %v", err)
	}

	if len(rs.ManagedResources) != 1 {
		t.Errorf("expected 1 MR (skipping .txt), got %d", len(rs.ManagedResources))
	}
}

func TestScanWithKustomizeHandlesMissingResource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "kustomization.yaml"), `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - does-not-exist.yaml
  - exists.yaml
`)
	writeFile(t, filepath.Join(dir, "exists.yaml"), `
apiVersion: iam.aws.upbound.io/v1beta1
kind: Role
metadata:
  name: test-role
`)

	rs, err := ScanWithKustomize([]string{dir})
	if err != nil {
		t.Fatalf("ScanWithKustomize: %v", err)
	}

	if len(rs.ManagedResources) != 1 {
		t.Errorf("expected 1 MR (skipping missing file), got %d", len(rs.ManagedResources))
	}
}

func TestScanWithKustomizeNoDuplicates(t *testing.T) {
	// Scanning same dir twice should not produce duplicates
	rs, err := ScanWithKustomize([]string{
		kustomizeTestPath("compositions"),
		kustomizeTestPath("compositions"),
	})
	if err != nil {
		t.Fatalf("ScanWithKustomize: %v", err)
	}

	if len(rs.XRDs) != 1 {
		t.Errorf("expected 1 XRD (no duplicates), got %d", len(rs.XRDs))
	}
}

func TestFindKustomization(t *testing.T) {
	dir := t.TempDir()

	if findKustomization(dir) != "" {
		t.Error("expected no kustomization in empty dir")
	}

	writeFile(t, filepath.Join(dir, "kustomization.yaml"), "kind: Kustomization")
	if findKustomization(dir) == "" {
		t.Error("expected to find kustomization.yaml")
	}
}

func TestScanWithKustomizeNestedDirs(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "infra")
	os.MkdirAll(subDir, 0755)

	writeFile(t, filepath.Join(dir, "kustomization.yaml"), `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - infra/
`)
	writeFile(t, filepath.Join(subDir, "kustomization.yaml"), `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - vpc.yaml
  - subnet.yaml
`)
	writeFile(t, filepath.Join(subDir, "vpc.yaml"), `
apiVersion: ec2.aws.upbound.io/v1beta1
kind: VPC
metadata:
  name: main-vpc
`)
	writeFile(t, filepath.Join(subDir, "subnet.yaml"), `
apiVersion: ec2.aws.upbound.io/v1beta1
kind: Subnet
metadata:
  name: private-1
`)

	rs, err := ScanWithKustomize([]string{dir})
	if err != nil {
		t.Fatalf("ScanWithKustomize nested: %v", err)
	}

	if len(rs.ManagedResources) != 2 {
		t.Errorf("expected 2 MRs from nested kustomize, got %d", len(rs.ManagedResources))
	}
}

func kustomizeTestPath(subdir string) string {
	wd, _ := os.Getwd()
	base := filepath.Join(wd, "..", "..", "testdata", "kustomize-test")
	if subdir != "" {
		return filepath.Join(base, subdir)
	}
	return base
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
