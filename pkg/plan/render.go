package plan

import (
	"fmt"
	"io"
	"strings"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/tofu"
)

type Result struct {
	StructuralDiff *diff.DiffResult
	CloudPlan      *tofu.PlanResult
}

func Render(r *Result, format string, w io.Writer) error {
	switch format {
	case "markdown":
		return renderMarkdown(r, w)
	case "json":
		return renderJSON(r, w)
	default:
		return renderTerminal(r, w)
	}
}

func RenderDiffOnly(d *diff.DiffResult, w io.Writer) error {
	return renderTerminal(&Result{StructuralDiff: d}, w)
}

func renderTerminal(r *Result, w io.Writer) error {
	d := r.StructuralDiff
	if d == nil || (len(d.Diffs) == 0 && r.CloudPlan == nil) {
		fmt.Fprintln(w, "No changes detected.")
		return nil
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "═══ Structural Changes ═══")
	fmt.Fprintln(w)

	for _, rd := range d.Diffs {
		prefix, color := actionStyle(rd.Action)
		fmt.Fprintf(w, "  %s%s %s/%s%s\n", color, prefix, rd.Kind, rd.Name, resetColor)

		for _, fc := range rd.FieldChanges {
			renderFieldChange(w, fc, "      ")
		}
		fmt.Fprintln(w)
	}

	if r.CloudPlan != nil && r.CloudPlan.HasChanges {
		fmt.Fprintln(w, "═══ Cloud Impact ═══")
		fmt.Fprintln(w)

		for _, rc := range r.CloudPlan.Changes {
			if rc.Action == "no-op" {
				continue
			}
			prefix := actionPrefix(rc.Action)
			fmt.Fprintf(w, "  %s %s\n", prefix, rc.Address)
			renderCloudChanges(w, rc, "      ")
			fmt.Fprintln(w)
		}
	}

	fmt.Fprintf(w, "Plan: %s\n", d.Summary.String())
	if r.CloudPlan != nil {
		fmt.Fprintf(w, "Cloud: %d to add, %d to change, %d to destroy\n",
			r.CloudPlan.Summary.Add, r.CloudPlan.Summary.Change, r.CloudPlan.Summary.Destroy)
	}
	fmt.Fprintln(w)

	return nil
}

func renderMarkdown(r *Result, w io.Writer) error {
	d := r.StructuralDiff
	if d == nil || (len(d.Diffs) == 0 && r.CloudPlan == nil) {
		fmt.Fprintln(w, "### Crossplane Validation\n\nNo changes detected.")
		return nil
	}

	fmt.Fprintln(w, "### Crossplane Validation")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "**Plan:** %s\n\n", d.Summary.String())

	if len(d.Diffs) > 0 {
		fmt.Fprintln(w, "<details>")
		fmt.Fprintln(w, "<summary>Structural Changes</summary>")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "```diff")

		for _, rd := range d.Diffs {
			prefix := actionPrefix(string(rd.Action))
			fmt.Fprintf(w, "%s %s/%s\n", prefix, rd.Kind, rd.Name)
			for _, fc := range rd.FieldChanges {
				renderFieldChangeDiff(w, fc, "    ")
			}
		}

		fmt.Fprintln(w, "```")
		fmt.Fprintln(w, "</details>")
	}

	if r.CloudPlan != nil && r.CloudPlan.HasChanges {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "<details>")
		fmt.Fprintln(w, "<summary>Cloud Impact</summary>")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "```diff")

		for _, rc := range r.CloudPlan.Changes {
			if rc.Action == "no-op" {
				continue
			}
			prefix := actionPrefix(rc.Action)
			fmt.Fprintf(w, "%s %s\n", prefix, rc.Address)
			renderCloudChanges(w, rc, "    ")
		}

		fmt.Fprintln(w, "```")
		fmt.Fprintln(w, "</details>")
	}

	return nil
}

func renderJSON(r *Result, w io.Writer) error {
	// Minimal JSON output for programmatic consumption
	fmt.Fprintln(w, "{}")
	return nil
}

func renderFieldChange(w io.Writer, fc diff.FieldChange, indent string) {
	switch fc.Action {
	case diff.ActionCreate:
		fmt.Fprintf(w, "%s%s+ %s: %v%s\n", indent, green, fc.Path, formatValue(fc.NewValue), resetColor)
	case diff.ActionDelete:
		fmt.Fprintf(w, "%s%s- %s: %v%s\n", indent, red, fc.Path, formatValue(fc.OldValue), resetColor)
	case diff.ActionUpdate:
		fmt.Fprintf(w, "%s%s~ %s: %v → %v%s\n", indent, yellow, fc.Path,
			formatValue(fc.OldValue), formatValue(fc.NewValue), resetColor)
	}
}

func renderFieldChangeDiff(w io.Writer, fc diff.FieldChange, indent string) {
	switch fc.Action {
	case diff.ActionCreate:
		fmt.Fprintf(w, "%s+ %s: %v\n", indent, fc.Path, formatValue(fc.NewValue))
	case diff.ActionDelete:
		fmt.Fprintf(w, "%s- %s: %v\n", indent, fc.Path, formatValue(fc.OldValue))
	case diff.ActionUpdate:
		fmt.Fprintf(w, "%s- %s: %v\n", indent, fc.Path, formatValue(fc.OldValue))
		fmt.Fprintf(w, "%s+ %s: %v\n", indent, fc.Path, formatValue(fc.NewValue))
	}
}

func renderCloudChanges(w io.Writer, rc tofu.ResourceChange, indent string) {
	if rc.After != nil {
		for k, v := range rc.After {
			if rc.Before != nil {
				if oldV, ok := rc.Before[k]; ok {
					if fmt.Sprintf("%v", oldV) != fmt.Sprintf("%v", v) {
						fmt.Fprintf(w, "%s~ %s: %v → %v\n", indent, k, oldV, v)
						continue
					}
					continue
				}
			}
			fmt.Fprintf(w, "%s+ %s: %v\n", indent, k, v)
		}
	}
	if rc.Before != nil {
		for k, v := range rc.Before {
			if rc.After == nil {
				fmt.Fprintf(w, "%s- %s: %v\n", indent, k, v)
			} else if _, ok := rc.After[k]; !ok {
				fmt.Fprintf(w, "%s- %s: %v\n", indent, k, v)
			}
		}
	}
}

func actionStyle(a diff.Action) (string, string) {
	switch a {
	case diff.ActionCreate:
		return "+", green
	case diff.ActionDelete:
		return "-", red
	case diff.ActionUpdate:
		return "~", yellow
	default:
		return " ", ""
	}
}

func actionPrefix(action string) string {
	switch action {
	case "create":
		return "+"
	case "delete":
		return "-"
	case "update":
		return "~"
	default:
		return " "
	}
}

func formatValue(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	s := fmt.Sprintf("%v", v)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

const (
	green      = "\033[32m"
	red        = "\033[31m"
	yellow     = "\033[33m"
	resetColor = "\033[0m"
)

// StripColors removes ANSI color codes for non-terminal output.
func StripColors(s string) string {
	for _, code := range []string{green, red, yellow, resetColor} {
		s = strings.ReplaceAll(s, code, "")
	}
	return s
}
