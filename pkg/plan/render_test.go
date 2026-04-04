package plan

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/tofu"
)

func TestRenderTerminalNoChanges(t *testing.T) {
	r := &Result{
		StructuralDiff: &diff.DiffResult{},
	}

	var buf bytes.Buffer
	if err := Render(r, "terminal", &buf); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(buf.String(), "No changes") {
		t.Errorf("expected 'No changes' message, got: %s", buf.String())
	}
}

func TestRenderTerminalWithChanges(t *testing.T) {
	r := &Result{
		StructuralDiff: &diff.DiffResult{
			Diffs: []diff.ResourceDiff{
				{
					Action: diff.ActionCreate,
					Kind:   "Bucket",
					Name:   "logs",
					FieldChanges: []diff.FieldChange{
						{Path: "spec.forProvider.region", NewValue: "us-east-1", Action: diff.ActionCreate},
					},
				},
				{
					Action: diff.ActionUpdate,
					Kind:   "Role",
					Name:   "app-role",
					FieldChanges: []diff.FieldChange{
						{
							Path:     "spec.forProvider.tags.Environment",
							OldValue: "staging",
							NewValue: "production",
							Action:   diff.ActionUpdate,
						},
					},
				},
				{
					Action: diff.ActionDelete,
					Kind:   "VPC",
					Name:   "old-vpc",
				},
			},
			Summary: diff.DiffSummary{ToAdd: 1, ToChange: 1, ToDelete: 1},
		},
	}

	var buf bytes.Buffer
	if err := Render(r, "terminal", &buf); err != nil {
		t.Fatal(err)
	}

	output := StripColors(buf.String())
	if !strings.Contains(output, "Structural Changes") {
		t.Error("missing 'Structural Changes' header")
	}
	if !strings.Contains(output, "+ Bucket/logs") {
		t.Error("missing create entry")
	}
	if !strings.Contains(output, "~ Role/app-role") {
		t.Error("missing update entry")
	}
	if !strings.Contains(output, "- VPC/old-vpc") {
		t.Error("missing delete entry")
	}
	if !strings.Contains(output, "1 to add, 1 to change, 1 to destroy") {
		t.Error("missing summary line")
	}
}

func TestRenderMarkdownFormat(t *testing.T) {
	r := &Result{
		StructuralDiff: &diff.DiffResult{
			Diffs: []diff.ResourceDiff{
				{
					Action: diff.ActionCreate,
					Kind:   "Bucket",
					Name:   "new-bucket",
					FieldChanges: []diff.FieldChange{
						{Path: "region", NewValue: "us-east-1", Action: diff.ActionCreate},
					},
				},
			},
			Summary: diff.DiffSummary{ToAdd: 1},
		},
	}

	var buf bytes.Buffer
	if err := Render(r, "markdown", &buf); err != nil {
		t.Fatal(err)
	}

	output := buf.String()

	for _, expected := range []string{
		"### Crossplane Validation",
		"**Plan:**",
		"<details>",
		"```diff",
		"+ Bucket/new-bucket",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("markdown output missing %q", expected)
		}
	}
}

func TestRenderWithCloudPlan(t *testing.T) {
	r := &Result{
		StructuralDiff: &diff.DiffResult{
			Diffs:   []diff.ResourceDiff{{Action: diff.ActionCreate, Kind: "Bucket", Name: "test"}},
			Summary: diff.DiffSummary{ToAdd: 1},
		},
		CloudPlan: &tofu.PlanResult{
			HasChanges: true,
			Changes: []tofu.ResourceChange{
				{
					Address:      "aws_s3_bucket.test",
					Action:       "create",
					ResourceType: "aws_s3_bucket",
					Name:         "test",
					After:        map[string]interface{}{"region": "us-east-1", "acl": "private"},
				},
			},
			Summary: tofu.PlanSummary{Add: 1},
		},
	}

	var buf bytes.Buffer
	if err := Render(r, "terminal", &buf); err != nil {
		t.Fatal(err)
	}

	output := StripColors(buf.String())
	if !strings.Contains(output, "Cloud Impact") {
		t.Error("missing 'Cloud Impact' section")
	}
	if !strings.Contains(output, "aws_s3_bucket.test") {
		t.Error("missing cloud resource address")
	}
}

func TestRenderMarkdownWithCloudPlan(t *testing.T) {
	r := &Result{
		StructuralDiff: &diff.DiffResult{
			Diffs:   []diff.ResourceDiff{{Action: diff.ActionUpdate, Kind: "DatabaseInstance", Name: "db"}},
			Summary: diff.DiffSummary{ToChange: 1},
		},
		CloudPlan: &tofu.PlanResult{
			HasChanges: true,
			Changes: []tofu.ResourceChange{
				{
					Address: "google_sql_database_instance.db",
					Action:  "update",
					Before:  map[string]interface{}{"tier": "db-f1-micro"},
					After:   map[string]interface{}{"tier": "db-n1-standard-2"},
				},
			},
			Summary: tofu.PlanSummary{Change: 1},
		},
	}

	var buf bytes.Buffer
	if err := Render(r, "markdown", &buf); err != nil {
		t.Fatal(err)
	}

	output := buf.String()
	if !strings.Contains(output, "Cloud Impact") {
		t.Error("missing cloud impact section in markdown")
	}
}

func TestRenderDiffOnly(t *testing.T) {
	d := &diff.DiffResult{
		Diffs: []diff.ResourceDiff{
			{Action: diff.ActionCreate, Kind: "Account", Name: "storage"},
		},
		Summary: diff.DiffSummary{ToAdd: 1},
	}

	var buf bytes.Buffer
	if err := RenderDiffOnly(d, &buf); err != nil {
		t.Fatal(err)
	}

	output := StripColors(buf.String())
	if !strings.Contains(output, "+ Account/storage") {
		t.Error("missing create entry in diff-only output")
	}
}

func TestStripColors(t *testing.T) {
	colored := green + "hello" + resetColor
	stripped := StripColors(colored)
	if stripped != "hello" {
		t.Errorf("StripColors(%q) = %q, want %q", colored, stripped, "hello")
	}
}
