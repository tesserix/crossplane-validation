package operator

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/manifest"
	"github.com/tesserix/crossplane-validation/pkg/renderer"
)

// DriftType categorizes how a resource differs between git and cluster.
type DriftType string

const (
	DriftMissingInCluster DriftType = "missing_in_cluster"
	DriftMissingInGit     DriftType = "missing_in_git"
	DriftSpecChanged      DriftType = "spec_drift"
)

// DriftResult represents a single drifted resource with its field-level changes.
type DriftResult struct {
	ResourceKey string
	Kind        string
	Name        string
	Namespace   string
	DriftType   DriftType
	Changes     []diff.FieldChange
}

// DriftSummary counts drifted resources by category.
type DriftSummary struct {
	MissingInCluster int
	MissingInGit     int
	SpecDrift        int
	Total            int
}

// DriftWarning flags a resource where proposed state differs from live state.
type DriftWarning struct {
	ResourceKey string
	Message     string
	Severity    string
}

// ComputeDrift compares git manifests against live cluster state and returns differences.
func ComputeDrift(cache *StateCache, gitYAML []byte) ([]DriftResult, *DriftSummary, error) {
	gitRS, err := manifest.ParseBytes(gitYAML)
	if err != nil {
		return nil, nil, err
	}

	liveRS := cache.GetResourceSet()

	liveRendered, err := renderer.Render(liveRS)
	if err != nil {
		return nil, nil, err
	}

	gitRendered, err := renderer.Render(gitRS)
	if err != nil {
		return nil, nil, err
	}

	diffResult := diff.Compute(liveRendered, gitRendered)

	var drifts []DriftResult
	summary := &DriftSummary{}

	for _, d := range diffResult.Diffs {
		switch d.Action {
		case diff.ActionCreate:
			drifts = append(drifts, DriftResult{
				ResourceKey: d.ResourceKey,
				Kind:        d.Kind,
				Name:        d.Name,
				Namespace:   d.Namespace,
				DriftType:   DriftMissingInCluster,
				Changes:     d.FieldChanges,
			})
			summary.MissingInCluster++

		case diff.ActionDelete:
			drifts = append(drifts, DriftResult{
				ResourceKey: d.ResourceKey,
				Kind:        d.Kind,
				Name:        d.Name,
				Namespace:   d.Namespace,
				DriftType:   DriftMissingInGit,
				Changes:     d.FieldChanges,
			})
			summary.MissingInGit++

		case diff.ActionUpdate:
			drifts = append(drifts, DriftResult{
				ResourceKey: d.ResourceKey,
				Kind:        d.Kind,
				Name:        d.Name,
				Namespace:   d.Namespace,
				DriftType:   DriftSpecChanged,
				Changes:     d.FieldChanges,
			})
			summary.SpecDrift++
		}
	}

	summary.Total = summary.MissingInCluster + summary.MissingInGit + summary.SpecDrift
	return drifts, summary, nil
}

func detectDrift(liveRS, proposedRS *manifest.ResourceSet) []DriftWarning {
	var warnings []DriftWarning

	liveIndex := indexResources(liveRS.AllResources())
	proposedIndex := indexResources(proposedRS.AllResources())

	for key := range proposedIndex {
		if _, exists := liveIndex[key]; !exists {
			continue
		}

		liveObj := liveIndex[key]
		proposedObj := proposedIndex[key]

		liveFP, _, _ := unstructured.NestedMap(liveObj.Object, "spec", "forProvider")
		proposedFP, _, _ := unstructured.NestedMap(proposedObj.Object, "spec", "forProvider")

		if liveFP == nil || proposedFP == nil {
			continue
		}

		for field, liveVal := range liveFP {
			if proposedVal, ok := proposedFP[field]; ok {
				if fmt.Sprintf("%v", liveVal) != fmt.Sprintf("%v", proposedVal) {
					warnings = append(warnings, DriftWarning{
						ResourceKey: key,
						Message: fmt.Sprintf("%s/%s: field %s differs (cluster: %v, proposed: %v)",
							liveObj.GetKind(), liveObj.GetName(), field, liveVal, proposedVal),
						Severity: "warning",
					})
				}
			}
		}
	}

	return warnings
}

func indexResources(resources []unstructured.Unstructured) map[string]unstructured.Unstructured {
	idx := make(map[string]unstructured.Unstructured, len(resources))
	for _, r := range resources {
		key := resourceKey(r.GetAPIVersion(), r.GetKind(), r.GetNamespace(), r.GetName())
		idx[key] = r
	}
	return idx
}
