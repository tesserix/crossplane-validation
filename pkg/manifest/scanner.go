package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// ResourceSet holds all parsed Crossplane resources grouped by type.
type ResourceSet struct {
	XRDs           []unstructured.Unstructured
	Compositions   []unstructured.Unstructured
	Claims         []unstructured.Unstructured
	XRs            []unstructured.Unstructured
	ManagedResources []unstructured.Unstructured
	ProviderConfigs  []unstructured.Unstructured
	Functions      []unstructured.Unstructured
	Other          []unstructured.Unstructured
}

// Scan reads all YAML files from the given directories and classifies them.
func Scan(dirs []string) (*ResourceSet, error) {
	rs := &ResourceSet{}

	for _, dir := range dirs {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".yaml" && ext != ".yml" {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", path, err)
			}

			resources, err := parseMultiDoc(data)
			if err != nil {
				return fmt.Errorf("parsing %s: %w", path, err)
			}

			for _, r := range resources {
				rs.classify(r)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return rs, nil
}

// ScanFromGitRef reads manifests from a specific git ref (branch/tag/commit).
func ScanFromGitRef(dirs []string, ref string) (*ResourceSet, error) {
	if ref == "HEAD" {
		return Scan(dirs)
	}

	rs := &ResourceSet{}

	for _, dir := range dirs {
		files, err := gitListFiles(ref, dir)
		if err != nil {
			return nil, fmt.Errorf("listing files at %s:%s: %w", ref, dir, err)
		}

		for _, file := range files {
			ext := strings.ToLower(filepath.Ext(file))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}

			data, err := gitShowFile(ref, file)
			if err != nil {
				continue // file may not exist in this ref
			}

			resources, err := parseMultiDoc(data)
			if err != nil {
				continue
			}

			for _, r := range resources {
				rs.classify(r)
			}
		}
	}

	return rs, nil
}

// AllResources returns all resources as a flat slice.
func (rs *ResourceSet) AllResources() []unstructured.Unstructured {
	var all []unstructured.Unstructured
	all = append(all, rs.XRDs...)
	all = append(all, rs.Compositions...)
	all = append(all, rs.Claims...)
	all = append(all, rs.XRs...)
	all = append(all, rs.ManagedResources...)
	all = append(all, rs.ProviderConfigs...)
	all = append(all, rs.Functions...)
	all = append(all, rs.Other...)
	return all
}

// Summary returns a human-readable summary of the resource set.
func (rs *ResourceSet) Summary() string {
	parts := []string{}
	if n := len(rs.XRDs); n > 0 {
		parts = append(parts, fmt.Sprintf("%d XRDs", n))
	}
	if n := len(rs.Compositions); n > 0 {
		parts = append(parts, fmt.Sprintf("%d Compositions", n))
	}
	if n := len(rs.Claims); n > 0 {
		parts = append(parts, fmt.Sprintf("%d Claims", n))
	}
	if n := len(rs.XRs); n > 0 {
		parts = append(parts, fmt.Sprintf("%d XRs", n))
	}
	if n := len(rs.ManagedResources); n > 0 {
		parts = append(parts, fmt.Sprintf("%d ManagedResources", n))
	}
	if n := len(rs.ProviderConfigs); n > 0 {
		parts = append(parts, fmt.Sprintf("%d ProviderConfigs", n))
	}
	if n := len(rs.Functions); n > 0 {
		parts = append(parts, fmt.Sprintf("%d Functions", n))
	}
	return strings.Join(parts, ", ")
}

func (rs *ResourceSet) classify(obj unstructured.Unstructured) {
	apiVersion := obj.GetAPIVersion()
	kind := obj.GetKind()

	switch {
	case apiVersion == "apiextensions.crossplane.io/v1" && kind == "CompositeResourceDefinition":
		rs.XRDs = append(rs.XRDs, obj)
	case apiVersion == "apiextensions.crossplane.io/v1" && kind == "Composition":
		rs.Compositions = append(rs.Compositions, obj)
	case apiVersion == "pkg.crossplane.io/v1" && kind == "Function":
		rs.Functions = append(rs.Functions, obj)
	case strings.HasSuffix(kind, "Claim"):
		rs.Claims = append(rs.Claims, obj)
	case isProviderConfig(apiVersion, kind):
		rs.ProviderConfigs = append(rs.ProviderConfigs, obj)
	case isManagedResource(apiVersion):
		rs.ManagedResources = append(rs.ManagedResources, obj)
	case isCompositeResource(apiVersion, kind):
		rs.XRs = append(rs.XRs, obj)
	default:
		rs.Other = append(rs.Other, obj)
	}
}

func isProviderConfig(apiVersion, kind string) bool {
	return kind == "ProviderConfig" || kind == "StoreConfig"
}

func isManagedResource(apiVersion string) bool {
	providerPrefixes := []string{
		"s3.aws.upbound.io",
		"ec2.aws.upbound.io",
		"iam.aws.upbound.io",
		"rds.aws.upbound.io",
		"eks.aws.upbound.io",
		"lambda.aws.upbound.io",
		"storage.gcp.upbound.io",
		"compute.gcp.upbound.io",
		"container.gcp.upbound.io",
		"sql.gcp.upbound.io",
		"iam.gcp.upbound.io",
		"azure.upbound.io",
		"network.azure.upbound.io",
		"compute.azure.upbound.io",
		"storage.azure.upbound.io",
	}
	for _, prefix := range providerPrefixes {
		if strings.Contains(apiVersion, prefix) {
			return true
		}
	}
	return strings.Contains(apiVersion, ".aws.crossplane.io") ||
		strings.Contains(apiVersion, ".gcp.crossplane.io") ||
		strings.Contains(apiVersion, ".azure.crossplane.io")
}

func isCompositeResource(apiVersion, kind string) bool {
	// XRs typically have custom API groups and don't end in "Claim"
	return strings.Contains(apiVersion, ".crossplane.io") &&
		!strings.HasPrefix(apiVersion, "apiextensions.") &&
		!strings.HasPrefix(apiVersion, "pkg.") &&
		!strings.HasSuffix(kind, "Claim")
}

func parseMultiDoc(data []byte) ([]unstructured.Unstructured, error) {
	var resources []unstructured.Unstructured
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(doc), len(doc)).Decode(obj); err != nil {
			continue // skip unparseable docs
		}

		if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
			continue
		}

		resources = append(resources, *obj)
	}

	return resources, nil
}

func gitListFiles(ref, dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-tree", "-r", "--name-only", ref, dir)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func gitShowFile(ref, path string) ([]byte, error) {
	cmd := exec.Command("git", "show", ref+":"+path)
	return cmd.Output()
}
