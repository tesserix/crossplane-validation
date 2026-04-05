package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type kustomization struct {
	Resources []string `yaml:"resources"`
}

// ScanWithKustomize reads a kustomization.yaml and recursively follows all
// resource paths to discover Crossplane manifests. If the directory has no
// kustomization.yaml, it falls back to scanning all YAML files recursively.
func ScanWithKustomize(dirs []string) (*ResourceSet, error) {
	rs := &ResourceSet{}
	visited := map[string]bool{}

	for _, dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}

		kustomizePath := findKustomization(absDir)
		if kustomizePath != "" {
			if err := scanKustomizeTree(absDir, rs, visited); err != nil {
				return nil, err
			}
		} else {
			plainRS, err := Scan([]string{dir})
			if err != nil {
				return nil, err
			}
			mergeResourceSets(rs, plainRS)
		}
	}

	return rs, nil
}

func scanKustomizeTree(dir string, rs *ResourceSet, visited map[string]bool) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	if visited[absDir] {
		return nil
	}
	visited[absDir] = true

	kPath := findKustomization(absDir)
	if kPath == "" {
		return scanDirectYAMLs(dir, rs)
	}

	data, err := os.ReadFile(kPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", kPath, err)
	}

	var k kustomization
	if err := yaml.Unmarshal(data, &k); err != nil {
		return fmt.Errorf("parsing %s: %w", kPath, err)
	}

	for _, res := range k.Resources {
		resPath := filepath.Join(absDir, res)

		info, err := os.Stat(resPath)
		if err != nil {
			continue
		}

		if info.IsDir() {
			if err := scanKustomizeTree(resPath, rs, visited); err != nil {
				return err
			}
		} else {
			ext := strings.ToLower(filepath.Ext(resPath))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}

			fileData, err := os.ReadFile(resPath)
			if err != nil {
				continue
			}

			resources, err := parseMultiDoc(fileData)
			if err != nil {
				continue
			}

			for _, r := range resources {
				rs.classify(r)
			}
		}
	}

	return nil
}

func scanDirectYAMLs(dir string, rs *ResourceSet) error {
	plainRS, err := Scan([]string{dir})
	if err != nil {
		return err
	}
	mergeResourceSets(rs, plainRS)
	return nil
}

func findKustomization(dir string) string {
	for _, name := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func mergeResourceSets(dst, src *ResourceSet) {
	dst.XRDs = append(dst.XRDs, src.XRDs...)
	dst.Compositions = append(dst.Compositions, src.Compositions...)
	dst.Claims = append(dst.Claims, src.Claims...)
	dst.XRs = append(dst.XRs, src.XRs...)
	dst.ManagedResources = append(dst.ManagedResources, src.ManagedResources...)
	dst.ProviderConfigs = append(dst.ProviderConfigs, src.ProviderConfigs...)
	dst.Functions = append(dst.Functions, src.Functions...)
	dst.Other = append(dst.Other, src.Other...)
}
