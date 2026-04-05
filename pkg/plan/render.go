package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/tofu"
	"github.com/tesserix/crossplane-validation/pkg/validate"
)

type Result struct {
	StructuralDiff   *diff.DiffResult
	CloudPlan        *tofu.PlanResult
	ValidationIssues []validate.ValidationIssue
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

func renderValidationIssues(w io.Writer, issues []validate.ValidationIssue) {
	if len(issues) == 0 {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "═══ Validation Issues ═══")
	fmt.Fprintln(w)

	// Errors first
	for _, issue := range issues {
		if issue.Severity != "error" {
			continue
		}
		field := issue.Field
		if field != "" {
			field = " " + field + ":"
		}
		fmt.Fprintf(w, "  %s\u2717 %s%s %s%s\n", red, issue.Resource, field, issue.Message, resetColor)
	}

	// Then warnings
	for _, issue := range issues {
		if issue.Severity != "warning" {
			continue
		}
		field := issue.Field
		if field != "" {
			field = " " + field + ":"
		}
		fmt.Fprintf(w, "  %s\u26a0 %s%s %s%s\n", yellow, issue.Resource, field, issue.Message, resetColor)
	}

	fmt.Fprintln(w)
}

func renderTerminal(r *Result, w io.Writer) error {
	d := r.StructuralDiff

	// Render validation issues before structural changes
	renderValidationIssues(w, r.ValidationIssues)

	if d == nil || (len(d.Diffs) == 0 && r.CloudPlan == nil) {
		if len(r.ValidationIssues) == 0 {
			fmt.Fprintln(w, "No changes detected.")
		}
		return nil
	}

	deletions, highRisk := collectWarnings(d)
	renderWarningsTerminal(w, deletions, highRisk)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "═══ Structural Changes ═══")
	fmt.Fprintln(w)

	for _, rd := range d.Diffs {
		prefix, color := actionStyle(rd.Action)
		if rd.Namespace != "" {
			fmt.Fprintf(w, "  %s%s %s/%s (namespace: %s)%s\n", color, prefix, rd.Kind, rd.Name, rd.Namespace, resetColor)
		} else {
			fmt.Fprintf(w, "  %s%s %s/%s%s\n", color, prefix, rd.Kind, rd.Name, resetColor)
		}

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

	deletions, highRisk := collectWarnings(d)
	renderWarningsMarkdown(w, deletions, highRisk)

	if len(d.Diffs) > 0 {
		fmt.Fprintln(w, "<details>")
		fmt.Fprintln(w, "<summary>Structural Changes</summary>")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "```diff")

		for _, rd := range d.Diffs {
			prefix := actionPrefix(string(rd.Action))
			if rd.Namespace != "" {
				fmt.Fprintf(w, "%s %s/%s (namespace: %s)\n", prefix, rd.Kind, rd.Name, rd.Namespace)
			} else {
				fmt.Fprintf(w, "%s %s/%s\n", prefix, rd.Kind, rd.Name)
			}
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
	out := jsonOutput{
		Changes: []jsonChange{},
		Summary: jsonSummary{},
	}

	if r.StructuralDiff != nil {
		out.Summary.Add = r.StructuralDiff.Summary.ToAdd
		out.Summary.Change = r.StructuralDiff.Summary.ToChange
		out.Summary.Destroy = r.StructuralDiff.Summary.ToDelete

		for _, rd := range r.StructuralDiff.Diffs {
			jc := jsonChange{
				Action:    string(rd.Action),
				Kind:      rd.Kind,
				Name:      rd.Name,
				Namespace: rd.Namespace,
				Fields:    []jsonFieldChange{},
			}
			for _, fc := range rd.FieldChanges {
				jc.Fields = append(jc.Fields, jsonFieldChange{
					Path:     fc.Path,
					Action:   string(fc.Action),
					OldValue: fc.OldValue,
					NewValue: fc.NewValue,
				})
			}
			out.Changes = append(out.Changes, jc)
		}
	}

	if r.CloudPlan != nil {
		out.Cloud = &jsonCloudPlan{
			HasChanges: r.CloudPlan.HasChanges,
			Summary: jsonSummary{
				Add:     r.CloudPlan.Summary.Add,
				Change:  r.CloudPlan.Summary.Change,
				Destroy: r.CloudPlan.Summary.Destroy,
			},
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

type jsonOutput struct {
	Changes []jsonChange   `json:"changes"`
	Summary jsonSummary    `json:"summary"`
	Cloud   *jsonCloudPlan `json:"cloud,omitempty"`
}

type jsonChange struct {
	Action    string            `json:"action"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Fields    []jsonFieldChange `json:"fields"`
}

type jsonFieldChange struct {
	Path     string      `json:"path"`
	Action   string      `json:"action"`
	OldValue interface{} `json:"oldValue,omitempty"`
	NewValue interface{} `json:"newValue,omitempty"`
}

type jsonSummary struct {
	Add     int `json:"add"`
	Change  int `json:"change"`
	Destroy int `json:"destroy"`
}

type jsonCloudPlan struct {
	HasChanges bool        `json:"hasChanges"`
	Summary    jsonSummary `json:"summary"`
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

// highRiskFields contains field name suffixes that indicate high-impact changes.
var highRiskFields = []string{
	"instanceClass", "instanceType", "size", "vmSize",
	"engine", "engineVersion", "version",
	"cidrBlock", "cidrRange",
	"deletionPolicy",
	"publiclyAccessible",
}

type warning struct {
	Symbol  string // "-" for deletion, "~" for high-risk change
	Message string
}

func collectWarnings(d *diff.DiffResult) (deletions []warning, highRisk []warning) {
	if d == nil {
		return
	}

	for _, rd := range d.Diffs {
		if rd.Action == diff.ActionDelete {
			deletions = append(deletions, warning{
				Symbol:  "-",
				Message: fmt.Sprintf("%s/%s (%s)", rd.Kind, rd.Name, rd.APIVersion),
			})
		}

		if rd.Action == diff.ActionUpdate {
			for _, fc := range rd.FieldChanges {
				if fc.Action != diff.ActionUpdate {
					continue
				}
				fieldName := lastSegment(fc.Path)
				if isHighRiskField(fieldName, fc) {
					msg := fmt.Sprintf("%s/%s: %s changing (%v → %v)",
						rd.Kind, rd.Name, fieldName,
						formatValue(fc.OldValue), formatValue(fc.NewValue))
					if fieldName == "version" || fieldName == "engineVersion" {
						msg = fmt.Sprintf("%s/%s: %s upgrade (%v → %v)",
							rd.Kind, rd.Name, fieldName,
							formatValue(fc.OldValue), formatValue(fc.NewValue))
					}
					highRisk = append(highRisk, warning{
						Symbol:  "~",
						Message: msg,
					})
				}
			}
		}
	}
	return
}

func lastSegment(path string) string {
	parts := strings.Split(path, ".")
	return parts[len(parts)-1]
}

func isHighRiskField(fieldName string, fc diff.FieldChange) bool {
	for _, hrf := range highRiskFields {
		if fieldName == hrf {
			if fieldName == "publiclyAccessible" {
				return fmt.Sprintf("%v", fc.OldValue) == "false" && fmt.Sprintf("%v", fc.NewValue) == "true"
			}
			return true
		}
	}
	return false
}

func renderWarningsTerminal(w io.Writer, deletions, highRisk []warning) {
	if len(deletions) == 0 && len(highRisk) == 0 {
		return
	}

	fmt.Fprintln(w)

	if len(deletions) > 0 {
		fmt.Fprintf(w, "  %s⚠ WARNING: %d resource(s) will be DESTROYED%s\n", yellow, len(deletions), resetColor)
		for _, d := range deletions {
			fmt.Fprintf(w, "  %s  %s %s%s\n", yellow, d.Symbol, d.Message, resetColor)
		}
		fmt.Fprintln(w)
	}

	if len(highRisk) > 0 {
		fmt.Fprintf(w, "  %s⚠ ATTENTION: High-impact changes detected%s\n", yellow, resetColor)
		for _, hr := range highRisk {
			fmt.Fprintf(w, "  %s  %s %s%s\n", yellow, hr.Symbol, hr.Message, resetColor)
		}
		fmt.Fprintln(w)
	}
}

func renderWarningsMarkdown(w io.Writer, deletions, highRisk []warning) {
	if len(deletions) == 0 && len(highRisk) == 0 {
		return
	}

	if len(deletions) > 0 {
		fmt.Fprintf(w, "> ⚠ **WARNING: %d resource(s) will be DESTROYED**\n", len(deletions))
		for _, d := range deletions {
			fmt.Fprintf(w, ">   %s %s\n", d.Symbol, d.Message)
		}
		fmt.Fprintln(w)
	}

	if len(highRisk) > 0 {
		fmt.Fprintln(w, "> ⚠ **ATTENTION: High-impact changes detected**")
		for _, hr := range highRisk {
			fmt.Fprintf(w, ">   %s %s\n", hr.Symbol, hr.Message)
		}
		fmt.Fprintln(w)
	}
}

// StripColors removes ANSI color codes for non-terminal output.
func StripColors(s string) string {
	for _, code := range []string{green, red, yellow, resetColor} {
		s = strings.ReplaceAll(s, code, "")
	}
	return s
}
