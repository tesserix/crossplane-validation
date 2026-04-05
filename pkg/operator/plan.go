package operator

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/manifest"
	"github.com/tesserix/crossplane-validation/pkg/plan"
	"github.com/tesserix/crossplane-validation/pkg/renderer"
	"github.com/tesserix/crossplane-validation/pkg/validate"
)

// LivePlanRequest configures a live plan computation against cluster state.
type LivePlanRequest struct {
	ProposedYAML  []byte
	ShowSensitive bool
	CloudMode     bool
}

// ComputeLivePlan diffs proposed manifests against the live cluster state from the cache.
// Unlike git-based plan which diffs ALL resources between two branches, live plan is
// scoped: it only considers resources that appear in the proposed manifests.
// Resources in the cluster that are NOT in the proposed set are ignored (they're not
// being changed by this PR). Resources in the proposed set that are NOT in the cluster
// are shown as additions.
func ComputeLivePlan(cache *StateCache, req LivePlanRequest) (*plan.Result, []DriftWarning, error) {
	proposed, err := manifest.ParseBytes(req.ProposedYAML)
	if err != nil {
		return nil, nil, err
	}

	live := cache.GetResourceSet()

	proposedRendered, err := renderer.Render(proposed)
	if err != nil {
		return nil, nil, err
	}

	// Scope the live state to only include resources that overlap with proposed.
	// This prevents the diff from treating every unrelated cluster resource as a deletion.
	scopedLive := scopeToProposed(live, proposed)

	scopedLiveRendered, err := renderer.Render(scopedLive)
	if err != nil {
		return nil, nil, err
	}

	diff.ShowSensitive = req.ShowSensitive
	structDiff := diff.Compute(scopedLiveRendered, proposedRendered)
	issues := validate.Validate(proposed)

	result := &plan.Result{
		StructuralDiff:   structDiff,
		ValidationIssues: issues,
	}

	driftWarnings := detectDrift(live, proposed)

	return result, driftWarnings, nil
}

// scopeToProposed returns a ResourceSet containing only the live resources
// whose apiVersion/kind/name match a resource in the proposed set.
func scopeToProposed(live, proposed *manifest.ResourceSet) *manifest.ResourceSet {
	proposedKeys := make(map[string]bool)
	for _, r := range proposed.AllResources() {
		key := resourceKey(r.GetAPIVersion(), r.GetKind(), r.GetNamespace(), r.GetName())
		proposedKeys[key] = true
	}

	var scoped []unstructured.Unstructured
	for _, r := range live.AllResources() {
		key := resourceKey(r.GetAPIVersion(), r.GetKind(), r.GetNamespace(), r.GetName())
		if proposedKeys[key] {
			scoped = append(scoped, r)
		}
	}

	return manifest.FromUnstructuredList(scoped)
}
