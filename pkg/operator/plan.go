package operator

import (
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
func ComputeLivePlan(cache *StateCache, req LivePlanRequest) (*plan.Result, []DriftWarning, error) {
	proposed, err := manifest.ParseBytes(req.ProposedYAML)
	if err != nil {
		return nil, nil, err
	}

	live := cache.GetResourceSet()

	liveRendered, err := renderer.Render(live)
	if err != nil {
		return nil, nil, err
	}

	proposedRendered, err := renderer.Render(proposed)
	if err != nil {
		return nil, nil, err
	}

	diff.ShowSensitive = req.ShowSensitive
	structDiff := diff.Compute(liveRendered, proposedRendered)
	issues := validate.Validate(proposed)

	result := &plan.Result{
		StructuralDiff:   structDiff,
		ValidationIssues: issues,
	}

	driftWarnings := detectDrift(live, proposed)

	return result, driftWarnings, nil
}
