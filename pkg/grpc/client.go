// Package grpc provides the gRPC server and client for communication between
// the crossplane-validate CLI and the in-cluster operator.
package grpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/operator"
	"github.com/tesserix/crossplane-validation/pkg/plan"
	"github.com/tesserix/crossplane-validation/pkg/validate"
)

// Client connects to the operator's API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiToken   string
}

// ConnectOptions configures the client connection.
type ConnectOptions struct {
	Address  string
	Timeout  time.Duration
	TLS      bool
	APIToken string
}

// Connect establishes a connection to the operator.
func Connect(ctx context.Context, opts ConnectOptions) (*Client, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}

	scheme := "http"
	if opts.TLS {
		scheme = "https"
	}

	client := &Client{
		baseURL: fmt.Sprintf("%s://%s", scheme, opts.Address),
		httpClient: &http.Client{
			Timeout: opts.Timeout,
		},
		apiToken: opts.APIToken,
	}

	healthCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	_, err := client.Health(healthCtx)
	if err != nil {
		return nil, fmt.Errorf("operator at %s not reachable: %w", opts.Address, err)
	}

	return client, nil
}

// Close is a no-op for HTTP clients but satisfies the interface.
func (c *Client) Close() error {
	return nil
}

// Health returns the operator health status.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.get(ctx, "/api/v1/health", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ComputePlan sends proposed manifests to the operator and receives a plan.
func (c *Client) ComputePlan(ctx context.Context, proposedYAML []byte, showSensitive bool) (*LivePlanResult, error) {
	path := "/api/v1/plan"
	if showSensitive {
		path += "?showSensitive=true"
	}

	var resp ComputePlanResponse
	if err := c.post(ctx, path, proposedYAML, &resp); err != nil {
		return nil, err
	}
	return convertComputePlanResponse(&resp), nil
}

// GetDrift sends git manifests to the operator and receives a drift report.
func (c *Client) GetDrift(ctx context.Context, gitYAML []byte) (*LiveDriftResult, error) {
	var resp GetDriftResponse
	if err := c.post(ctx, "/api/v1/drift", gitYAML, &resp); err != nil {
		return nil, err
	}
	return convertDriftResponse(&resp), nil
}

// GetClusterState returns all cached Crossplane resources from the operator.
func (c *Client) GetClusterState(ctx context.Context, namespace, kind, apiGroup string) (*GetClusterStateResponse, error) {
	path := "/api/v1/state?"
	if namespace != "" {
		path += "namespace=" + namespace + "&"
	}
	if kind != "" {
		path += "kind=" + kind + "&"
	}
	if apiGroup != "" {
		path += "apiGroup=" + apiGroup + "&"
	}

	var resp GetClusterStateResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetResourceStatus returns detailed status for a specific resource.
func (c *Client) GetResourceStatus(ctx context.Context, apiVersion, kind, name, namespace string) (*GetResourceStatusResponse, error) {
	path := fmt.Sprintf("/api/v1/resource?apiVersion=%s&kind=%s&name=%s&namespace=%s",
		apiVersion, kind, name, namespace)

	var resp GetResourceStatusResponse
	if err := c.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) get(ctx context.Context, path string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp map[string]string
		json.Unmarshal(body, &errResp)
		if msg, ok := errResp["error"]; ok {
			return fmt.Errorf("operator error: %s", msg)
		}
		return fmt.Errorf("operator returned %d", resp.StatusCode)
	}

	return json.Unmarshal(body, result)
}

func (c *Client) post(ctx context.Context, path string, data []byte, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-yaml")
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp map[string]string
		json.Unmarshal(body, &errResp)
		if msg, ok := errResp["error"]; ok {
			return fmt.Errorf("operator error: %s", msg)
		}
		return fmt.Errorf("operator returned %d", resp.StatusCode)
	}

	return json.Unmarshal(body, result)
}

func (c *Client) setAuth(req *http.Request) {
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}
}

// LivePlanResult holds the plan output along with drift warnings and cluster metadata.
type LivePlanResult struct {
	Plan          *plan.Result
	DriftWarnings []operator.DriftWarning
	ClusterInfo   *ClusterInfo
}

// LiveDriftResult holds the drift analysis results.
type LiveDriftResult struct {
	Drifts  []operator.DriftResult
	Summary *operator.DriftSummary
}

func convertComputePlanResponse(resp *ComputePlanResponse) *LivePlanResult {
	result := &LivePlanResult{
		ClusterInfo: resp.ClusterInfo,
	}

	planResult := &plan.Result{}

	if resp.Plan != nil {
		diffResult := &diff.DiffResult{
			Summary: diff.DiffSummary{
				ToAdd:    int(resp.Plan.Summary.ToAdd),
				ToChange: int(resp.Plan.Summary.ToChange),
				ToDelete: int(resp.Plan.Summary.ToDelete),
				NoOp:     int(resp.Plan.Summary.NoOp),
			},
		}

		for _, change := range resp.Plan.Changes {
			rd := diff.ResourceDiff{
				Action:      diff.Action(change.Action),
				Kind:        change.Kind,
				Name:        change.Name,
				Namespace:   change.Namespace,
				APIVersion:  change.APIVersion,
				Source:      change.Source,
				ResourceKey: fmt.Sprintf("%s/%s/%s", change.APIVersion, change.Kind, change.Name),
			}

			for _, fc := range change.FieldChanges {
				rd.FieldChanges = append(rd.FieldChanges, diff.FieldChange{
					Path:     fc.Path,
					Action:   diff.Action(fc.Action),
					OldValue: fc.OldValue,
					NewValue: fc.NewValue,
				})
			}

			diffResult.Diffs = append(diffResult.Diffs, rd)
		}

		planResult.StructuralDiff = diffResult
	}

	for _, issue := range resp.ValidationIssues {
		planResult.ValidationIssues = append(planResult.ValidationIssues, validate.ValidationIssue{
			Severity: issue.Severity,
			Resource: issue.Resource,
			Field:    issue.Field,
			Message:  issue.Message,
		})
	}

	result.Plan = planResult

	for _, warn := range resp.DriftWarnings {
		result.DriftWarnings = append(result.DriftWarnings, operator.DriftWarning{
			ResourceKey: warn.ResourceKey,
			Message:     warn.Message,
			Severity:    warn.Severity,
		})
	}

	return result
}

func convertDriftResponse(resp *GetDriftResponse) *LiveDriftResult {
	result := &LiveDriftResult{}

	if resp.Summary != nil {
		result.Summary = &operator.DriftSummary{
			MissingInCluster: int(resp.Summary.MissingInCluster),
			MissingInGit:     int(resp.Summary.MissingInGit),
			SpecDrift:        int(resp.Summary.SpecDrift),
			Total:            int(resp.Summary.Total),
		}
	}

	for _, d := range resp.Drifts {
		dr := operator.DriftResult{
			ResourceKey: d.ResourceKey,
			Kind:        d.Kind,
			Name:        d.Name,
			Namespace:   d.Namespace,
			DriftType:   operator.DriftType(d.DriftType),
		}
		for _, fc := range d.FieldChanges {
			dr.Changes = append(dr.Changes, diff.FieldChange{
				Path:     fc.Path,
				Action:   diff.Action(fc.Action),
				OldValue: fc.OldValue,
				NewValue: fc.NewValue,
			})
		}
		result.Drifts = append(result.Drifts, dr)
	}

	return result
}
