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
	XRDs             []unstructured.Unstructured
	Compositions     []unstructured.Unstructured
	Claims           []unstructured.Unstructured
	XRs              []unstructured.Unstructured
	ManagedResources []unstructured.Unstructured
	ProviderConfigs  []unstructured.Unstructured
	Functions        []unstructured.Unstructured
	Other            []unstructured.Unstructured
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
	case isCrossplaneCore(apiVersion, kind):
		rs.classifyCrossplaneCore(apiVersion, kind, obj)
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

func (rs *ResourceSet) classifyCrossplaneCore(apiVersion, kind string, obj unstructured.Unstructured) {
	switch {
	case kind == "CompositeResourceDefinition":
		rs.XRDs = append(rs.XRDs, obj)
	case kind == "Composition":
		rs.Compositions = append(rs.Compositions, obj)
	case kind == "Function":
		rs.Functions = append(rs.Functions, obj)
	default:
		rs.Other = append(rs.Other, obj)
	}
}

func isCrossplaneCore(apiVersion, kind string) bool {
	return strings.HasPrefix(apiVersion, "apiextensions.crossplane.io/") ||
		strings.HasPrefix(apiVersion, "pkg.crossplane.io/")
}

func isProviderConfig(apiVersion, kind string) bool {
	return kind == "ProviderConfig" || kind == "StoreConfig"
}

func isManagedResource(apiVersion string) bool {
	group := extractGroup(apiVersion)

	// Any upbound.io provider resource is a managed resource
	if strings.HasSuffix(group, ".upbound.io") || group == "upbound.io" {
		return true
	}

	// Legacy crossplane.io provider resources
	if strings.Contains(group, ".aws.crossplane.io") ||
		strings.Contains(group, ".gcp.crossplane.io") ||
		strings.Contains(group, ".azure.crossplane.io") {
		return true
	}

	return false
}

func isCompositeResource(apiVersion, kind string) bool {
	group := extractGroup(apiVersion)

	// Skip core k8s and crossplane API groups
	if isCoreKubernetesGroup(group) || isCrossplaneCoreGroup(group) {
		return false
	}

	// Skip ArgoCD, Flux, and other non-Crossplane resources
	if strings.Contains(group, "argoproj.io") ||
		strings.Contains(group, "fluxcd.io") ||
		strings.Contains(group, "kustomize.config.k8s.io") {
		return false
	}

	// If it's an upbound.io resource, it's a managed resource not an XR
	if strings.Contains(group, "upbound.io") {
		return false
	}

	// Custom API groups with crossplane-style versioning are likely XRs
	// e.g., platform.civica.cloud/v1alpha1, storage.example.org/v1alpha1
	if strings.Contains(apiVersion, "/v1alpha1") ||
		strings.Contains(apiVersion, "/v1beta1") ||
		strings.Contains(apiVersion, "/v1beta2") ||
		strings.Contains(apiVersion, "/v1") {
		if !strings.HasSuffix(kind, "Claim") && group != "" && strings.Contains(group, ".") {
			return true
		}
	}

	return false
}

func extractGroup(apiVersion string) string {
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func isCoreKubernetesGroup(group string) bool {
	coreGroups := []string{
		"", "v1", "apps", "batch", "rbac.authorization.k8s.io",
		"networking.k8s.io", "policy", "storage.k8s.io",
		"admissionregistration.k8s.io", "certificates.k8s.io",
		"coordination.k8s.io", "events.k8s.io", "discovery.k8s.io",
		"node.k8s.io", "scheduling.k8s.io", "autoscaling",
	}
	for _, cg := range coreGroups {
		if group == cg {
			return true
		}
	}
	return false
}

func isCrossplaneCoreGroup(group string) bool {
	return strings.HasPrefix(group, "apiextensions.crossplane.io") ||
		strings.HasPrefix(group, "pkg.crossplane.io")
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
