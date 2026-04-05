package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/operator"
	"github.com/tesserix/crossplane-validation/pkg/plan"
	"github.com/tesserix/crossplane-validation/pkg/validate"
)

// Client connects to the operator's gRPC service.
type Client struct {
	conn    *grpc.ClientConn
	service *ValidationServiceImpl
	address string
}

// ConnectOptions configures the gRPC client connection.
type ConnectOptions struct {
	Address string
	Timeout time.Duration
	TLS     bool
}

// Connect establishes a gRPC connection to the operator.
func Connect(ctx context.Context, opts ConnectOptions) (*Client, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}

	dialCtx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	var transportCreds grpc.DialOption
	if opts.TLS {
		transportCreds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS12,
		}))
	} else {
		transportCreds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	conn, err := grpc.DialContext(dialCtx, opts.Address,
		transportCreds,
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to operator at %s: %w", opts.Address, err)
	}

	return &Client{
		conn:    conn,
		address: opts.Address,
	}, nil
}

// Close terminates the gRPC connection.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Health returns the operator health status.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	if c.service != nil {
		return c.service.Health(ctx)
	}
	return nil, fmt.Errorf("not connected")
}

// ComputePlan sends proposed manifests to the operator and receives a plan.
func (c *Client) ComputePlan(ctx context.Context, proposedYAML []byte, showSensitive bool) (*LivePlanResult, error) {
	if c.service != nil {
		resp, err := c.service.ComputePlan(ctx, proposedYAML, showSensitive)
		if err != nil {
			return nil, err
		}
		return convertComputePlanResponse(resp), nil
	}
	return nil, fmt.Errorf("not connected")
}

// GetDrift sends git manifests to the operator and receives a drift report.
func (c *Client) GetDrift(ctx context.Context, gitYAML []byte) (*LiveDriftResult, error) {
	if c.service != nil {
		resp, err := c.service.GetDrift(ctx, gitYAML)
		if err != nil {
			return nil, err
		}
		return convertDriftResponse(resp), nil
	}
	return nil, fmt.Errorf("not connected")
}

// GetClusterState returns all cached Crossplane resources from the operator.
func (c *Client) GetClusterState(ctx context.Context, namespace, kind, apiGroup string) (*GetClusterStateResponse, error) {
	if c.service != nil {
		return c.service.GetClusterState(ctx, namespace, kind, apiGroup)
	}
	return nil, fmt.Errorf("not connected")
}

// GetResourceStatus returns detailed status for a specific resource.
func (c *Client) GetResourceStatus(ctx context.Context, apiVersion, kind, name, namespace string) (*GetResourceStatusResponse, error) {
	if c.service != nil {
		return c.service.GetResourceStatus(ctx, apiVersion, kind, name, namespace)
	}
	return nil, fmt.Errorf("not connected")
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

	for _, drift := range resp.Drifts {
		dr := operator.DriftResult{
			ResourceKey: drift.ResourceKey,
			Kind:        drift.Kind,
			Name:        drift.Name,
			Namespace:   drift.Namespace,
			DriftType:   operator.DriftType(drift.DriftType),
		}
		for _, fc := range drift.FieldChanges {
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
