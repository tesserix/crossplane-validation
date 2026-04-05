package lint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Tool represents an external linting/validation tool.
type Tool struct {
	Name    string
	Binary  string
	Check   func() bool
	Run     func(dirs []string) ([]Issue, error)
	Purpose string
}

// Issue is a finding from an external tool.
type Issue struct {
	Tool     string `json:"tool"`
	Severity string `json:"severity"` // error, warning, info
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Resource string `json:"resource,omitempty"`
	Message  string `json:"message"`
}

// Result aggregates findings from all tools.
type Result struct {
	Tools  []string `json:"toolsRun"`
	Issues []Issue  `json:"issues"`
}

// AvailableTools returns all supported external tools.
func AvailableTools() []Tool {
	return []Tool{
		yamllintTool(),
		kubeconformTool(),
		plutoTool(),
		kubelinterTool(),
		crossplaneValidateTool(),
	}
}

// Run executes all available tools (or a specific subset) against the given directories.
func Run(dirs []string, toolNames []string) (*Result, error) {
	allTools := AvailableTools()
	result := &Result{}

	for _, tool := range allTools {
		if len(toolNames) > 0 && !contains(toolNames, tool.Name) {
			continue
		}

		if !tool.Check() {
			continue
		}

		result.Tools = append(result.Tools, tool.Name)

		issues, err := tool.Run(dirs)
		if err != nil {
			result.Issues = append(result.Issues, Issue{
				Tool:     tool.Name,
				Severity: "warning",
				Message:  fmt.Sprintf("tool execution failed: %v", err),
			})
			continue
		}

		result.Issues = append(result.Issues, issues...)
	}

	return result, nil
}

// DetectTools returns which tools are available on the system.
func DetectTools() map[string]bool {
	available := map[string]bool{}
	for _, tool := range AvailableTools() {
		available[tool.Name] = tool.Check()
	}
	return available
}

func binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

// yamllint — YAML syntax and style checking
func yamllintTool() Tool {
	return Tool{
		Name:    "yamllint",
		Binary:  "yamllint",
		Purpose: "YAML syntax and style validation",
		Check:   func() bool { return binaryExists("yamllint") },
		Run: func(dirs []string) ([]Issue, error) {
			args := []string{"-f", "parsable", "-d", "{extends: relaxed, rules: {line-length: disable}}"}
			args = append(args, dirs...)

			cmd := exec.Command("yamllint", args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			cmd.Run() // yamllint returns non-zero on findings

			var issues []Issue
			for _, line := range strings.Split(stdout.String(), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				// Format: file:line:col: [severity] message
				issue := parseYamllintLine(line)
				if issue != nil {
					issues = append(issues, *issue)
				}
			}
			return issues, nil
		},
	}
}

func parseYamllintLine(line string) *Issue {
	// file:line:col: [severity] message (description)
	parts := strings.SplitN(line, ": ", 2)
	if len(parts) < 2 {
		return nil
	}

	filePart := parts[0]
	rest := parts[1]

	// Extract file from file:line:col
	fileFields := strings.Split(filePart, ":")
	file := fileFields[0]

	severity := "warning"
	if strings.HasPrefix(rest, "[error]") {
		severity = "error"
		rest = strings.TrimPrefix(rest, "[error] ")
	} else if strings.HasPrefix(rest, "[warning]") {
		rest = strings.TrimPrefix(rest, "[warning] ")
	}

	return &Issue{
		Tool:     "yamllint",
		Severity: severity,
		File:     file,
		Message:  rest,
	}
}

// kubeconform — Kubernetes schema validation
func kubeconformTool() Tool {
	return Tool{
		Name:    "kubeconform",
		Binary:  "kubeconform",
		Purpose: "Kubernetes manifest schema validation",
		Check:   func() bool { return binaryExists("kubeconform") },
		Run: func(dirs []string) ([]Issue, error) {
			args := []string{
				"-output", "json",
				"-summary",
				"-skip", "CompositeResourceDefinition,Composition,Function,ProviderConfig",
				"-ignore-missing-schemas",
			}
			args = append(args, dirs...)

			cmd := exec.Command("kubeconform", args...)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Run()

			var issues []Issue
			// kubeconform JSON output is one object per line
			for _, line := range strings.Split(stdout.String(), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || line == "{}" {
					continue
				}

				var result kubeconformResult
				if err := json.Unmarshal([]byte(line), &result); err != nil {
					continue
				}

				for _, r := range result.Resources {
					if r.Status == "statusValid" || r.Status == "statusSkipped" {
						continue
					}
					severity := "error"
					if r.Status == "statusError" {
						severity = "warning" // schema not found
					}
					issues = append(issues, Issue{
						Tool:     "kubeconform",
						Severity: severity,
						File:     r.Filename,
						Resource: fmt.Sprintf("%s/%s", r.Kind, r.Name),
						Message:  r.Msg,
					})
				}
			}
			return issues, nil
		},
	}
}

type kubeconformResult struct {
	Resources []kubeconformResource `json:"resources"`
}

type kubeconformResource struct {
	Filename string `json:"filename"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Status   string `json:"status"`
	Msg      string `json:"msg"`
}

// pluto — deprecated API version detection
func plutoTool() Tool {
	return Tool{
		Name:    "pluto",
		Binary:  "pluto",
		Purpose: "Deprecated Kubernetes API version detection",
		Check:   func() bool { return binaryExists("pluto") },
		Run: func(dirs []string) ([]Issue, error) {
			var allIssues []Issue
			for _, dir := range dirs {
				args := []string{"detect-files", "-d", dir, "-o", "json"}
				cmd := exec.Command("pluto", args...)
				var stdout bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Run()

				var result plutoResult
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					continue
				}

				for _, item := range result.Items {
					severity := "warning"
					if item.Removed {
						severity = "error"
					}

					msg := fmt.Sprintf("API %s %s/%s deprecated in %s",
						item.APIVersion, item.Kind, item.Name, item.DeprecatedIn)
					if item.Removed {
						msg += fmt.Sprintf(", removed in %s", item.RemovedIn)
					}
					msg += fmt.Sprintf(" — use %s instead", item.ReplacementAPI)

					allIssues = append(allIssues, Issue{
						Tool:     "pluto",
						Severity: severity,
						File:     item.FilePath,
						Resource: fmt.Sprintf("%s/%s", item.Kind, item.Name),
						Message:  msg,
					})
				}
			}
			return allIssues, nil
		},
	}
}

type plutoResult struct {
	Items []plutoItem `json:"items"`
}

type plutoItem struct {
	Name           string `json:"name"`
	FilePath       string `json:"filePath"`
	Kind           string `json:"kind"`
	APIVersion     string `json:"api-version"`
	DeprecatedIn   string `json:"deprecated-in"`
	RemovedIn      string `json:"removed-in"`
	ReplacementAPI string `json:"replacement-api"`
	Removed        bool   `json:"removed"`
}

// kube-linter — static analysis for K8s best practices
func kubelinterTool() Tool {
	return Tool{
		Name:    "kube-linter",
		Binary:  "kube-linter",
		Purpose: "Kubernetes best practices and security analysis",
		Check:   func() bool { return binaryExists("kube-linter") },
		Run: func(dirs []string) ([]Issue, error) {
			args := []string{"lint", "--format", "json"}
			args = append(args, dirs...)

			cmd := exec.Command("kube-linter", args...)
			var stdout bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Run()

			var result kubelinterResult
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				return nil, nil // no findings or parse error
			}

			var issues []Issue
			for _, r := range result.Reports {
				severity := "warning"
				if r.Diagnostic.Severity == "error" {
					severity = "error"
				}

				issues = append(issues, Issue{
					Tool:     "kube-linter",
					Severity: severity,
					File:     r.Object.Metadata.FilePath,
					Resource: fmt.Sprintf("%s/%s", r.Object.Kind, r.Object.Metadata.Name),
					Message:  fmt.Sprintf("[%s] %s", r.Check, r.Diagnostic.Message),
				})
			}
			return issues, nil
		},
	}
}

type kubelinterResult struct {
	Reports []kubelinterReport `json:"Reports"`
}

type kubelinterReport struct {
	Check      string `json:"Check"`
	Diagnostic struct {
		Message  string `json:"Message"`
		Severity string `json:"Severity"`
	} `json:"Diagnostic"`
	Object struct {
		Kind     string `json:"Kind"`
		Metadata struct {
			Name     string `json:"Name"`
			FilePath string `json:"FilePath"`
		} `json:"Metadata"`
	} `json:"Object"`
}

// crossplane beta validate — Crossplane-specific validation
func crossplaneValidateTool() Tool {
	return Tool{
		Name:    "crossplane-validate",
		Binary:  "crossplane",
		Purpose: "Crossplane composition and XRD validation",
		Check: func() bool {
			if !binaryExists("crossplane") {
				return false
			}
			// Check if beta validate subcommand exists
			cmd := exec.Command("crossplane", "beta", "validate", "--help")
			return cmd.Run() == nil
		},
		Run: func(dirs []string) ([]Issue, error) {
			if len(dirs) < 1 {
				return nil, nil
			}

			// crossplane beta validate requires separate extensions and resources dirs
			// We try each directory pair combination
			var issues []Issue
			for _, dir := range dirs {
				cmd := exec.Command("crossplane", "beta", "validate", dir, dir)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				cmd.Run()

				output := stdout.String() + stderr.String()
				for _, line := range strings.Split(output, "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					// Parse crossplane validate text output
					if strings.Contains(line, "error") || strings.Contains(line, "invalid") {
						issues = append(issues, Issue{
							Tool:     "crossplane-validate",
							Severity: "error",
							Message:  line,
						})
					} else if strings.Contains(line, "warning") || strings.Contains(line, "skipped") {
						issues = append(issues, Issue{
							Tool:     "crossplane-validate",
							Severity: "warning",
							Message:  line,
						})
					}
				}
			}
			return issues, nil
		},
	}
}
