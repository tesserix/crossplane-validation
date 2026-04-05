// Package grpc provides the gRPC server and client for communication between
// the crossplane-validate CLI and the in-cluster operator.
package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/tesserix/crossplane-validation/pkg/diff"
	"github.com/tesserix/crossplane-validation/pkg/manifest"
	"github.com/tesserix/crossplane-validation/pkg/notify"
	"github.com/tesserix/crossplane-validation/pkg/operator"
)

const maxRequestPayload = 10 * 1024 * 1024 // 10MB

// Server wraps a gRPC server that exposes the validation service.
type Server struct {
	cache    *operator.StateCache
	notifier notify.Notifier
	server   *grpc.Server
	port     int
	service  *ValidationServiceImpl
}

// ServerConfig holds the configuration needed to create a gRPC server.
type ServerConfig struct {
	Cache    *operator.StateCache
	Port     int
	Notifier notify.Notifier
}

// NewServer creates a new gRPC server with the given configuration.
func NewServer(cfg ServerConfig) *Server {
	return &Server{
		cache:    cfg.Cache,
		notifier: cfg.Notifier,
		port:     cfg.Port,
		service: &ValidationServiceImpl{
			cache:    cfg.Cache,
			notifier: cfg.Notifier,
		},
	}
}

// Start begins listening for gRPC connections. This blocks until the server is stopped.
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("listening on port %d: %w", s.port, err)
	}

	s.server = grpc.NewServer(
		grpc.MaxRecvMsgSize(maxRequestPayload),
	)

	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(s.server, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	log.Printf("gRPC server listening on :%d", s.port)
	return s.server.Serve(lis)
}

// Stop gracefully shuts down the gRPC server.
func (s *Server) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
	}
}

// Service returns the validation service implementation for direct in-process calls.
func (s *Server) Service() *ValidationServiceImpl {
	return s.service
}

// ValidationServiceImpl handles all validation RPCs backed by the state cache.
type ValidationServiceImpl struct {
	cache    *operator.StateCache
	notifier notify.Notifier
}

// Health returns the operator health status including cache statistics.
func (v *ValidationServiceImpl) Health(ctx context.Context) (*HealthResponse, error) {
	return &HealthResponse{
		Healthy:         true,
		CachedResources: int32(v.cache.ResourceCount()),
		Uptime:          v.cache.Uptime().Round(time.Second).String(),
		WatchedGroups:   v.cache.WatchedGroups(),
	}, nil
}

// GetClusterState returns all cached Crossplane resources, optionally filtered.
func (v *ValidationServiceImpl) GetClusterState(ctx context.Context, namespace, kind, apiGroup string) (*GetClusterStateResponse, error) {
	resources := v.cache.AllResources()

	var filtered []unstructured.Unstructured
	for _, res := range resources {
		if namespace != "" && res.GetNamespace() != namespace {
			continue
		}
		if kind != "" && res.GetKind() != kind {
			continue
		}
		if apiGroup != "" {
			group := extractGroup(res.GetAPIVersion())
			if group != apiGroup {
				continue
			}
		}
		filtered = append(filtered, res)
	}

	resp := &GetClusterStateResponse{
		Total: int32(len(filtered)),
	}

	for _, res := range filtered {
		protoRes, err := toProtoResource(res)
		if err != nil {
			log.Printf("skipping resource %s/%s: %v", res.GetKind(), res.GetName(), err)
			continue
		}
		resp.Resources = append(resp.Resources, protoRes)
	}

	return resp, nil
}

// ComputePlan accepts proposed YAML manifests and diffs them against live cluster state.
func (v *ValidationServiceImpl) ComputePlan(ctx context.Context, proposedYAML []byte, showSensitive bool) (*ComputePlanResponse, error) {
	if len(proposedYAML) == 0 {
		return nil, fmt.Errorf("proposed manifests cannot be empty")
	}
	if len(proposedYAML) > maxRequestPayload {
		return nil, fmt.Errorf("proposed manifests exceed maximum size of %d bytes", maxRequestPayload)
	}

	result, driftWarnings, err := operator.ComputeLivePlan(v.cache, operator.LivePlanRequest{
		ProposedYAML:  proposedYAML,
		ShowSensitive: showSensitive,
	})
	if err != nil {
		return nil, fmt.Errorf("computing plan: %w", err)
	}

	resp := &ComputePlanResponse{
		ClusterInfo: &ClusterInfo{
			CachedResources: int32(v.cache.ResourceCount()),
			CacheAge:        v.cache.Uptime().Round(time.Second).String(),
		},
	}

	if result.StructuralDiff != nil {
		resp.Plan = convertPlanResult(result.StructuralDiff)
	}

	for _, issue := range result.ValidationIssues {
		resp.ValidationIssues = append(resp.ValidationIssues, &ValidationIssue{
			Severity: issue.Severity,
			Resource: issue.Resource,
			Field:    issue.Field,
			Message:  issue.Message,
		})
	}

	for _, warn := range driftWarnings {
		resp.DriftWarnings = append(resp.DriftWarnings, &DriftWarning{
			ResourceKey: warn.ResourceKey,
			Message:     warn.Message,
			Severity:    warn.Severity,
		})
	}

	// Resolve composition changes for each changed resource
	if result.StructuralDiff != nil {
		proposed, _ := manifest.ParseBytes(proposedYAML)
		if proposed != nil {
			for _, res := range proposed.AllResources() {
				changedFields := make(map[string]operator.ComposedFieldChange)
				if resp.Plan != nil {
					for _, rc := range resp.Plan.Changes {
						if rc.Kind == res.GetKind() && rc.Name == res.GetName() && rc.Action == "update" {
							for _, fc := range rc.FieldChanges {
								if fc.Action == "update" {
									changedFields[fc.Path] = operator.ComposedFieldChange{
										Path:     fc.Path,
										OldValue: fc.OldValue,
										NewValue: fc.NewValue,
									}
								}
							}
						}
					}
				}
				if len(changedFields) > 0 {
					r := res
					composedChanges, tree := operator.ResolveComposedChanges(v.cache, &r, changedFields)
					for _, cc := range composedChanges {
						var fields []ComposedFieldChange
						for _, f := range cc.FieldChanges {
							fields = append(fields, ComposedFieldChange{
								Path:     f.Path,
								OldValue: f.OldValue,
								NewValue: f.NewValue,
							})
						}
						resp.ComposedChanges = append(resp.ComposedChanges, ComposedResourceChange{
							ResourceKind:    cc.ResourceKind,
							ResourceName:    cc.ResourceName,
							APIVersion:      cc.APIVersion,
							CompositionStep: cc.CompositionStep,
							Depth:           cc.Depth,
							FieldChanges:    fields,
						})
					}
					if tree != nil && len(tree.Children) > 0 {
						resp.ResourceTree = append(resp.ResourceTree, *convertTree(tree))
					}
				}
			}
		}
	}

	if v.notifier != nil && result.StructuralDiff != nil {
		go func() {
			if err := v.notifier.Send(result); err != nil {
				log.Printf("notification send failed: %v", err)
			}
		}()
	}

	return resp, nil
}

// GetDrift compares provided git manifests against live cluster state.
func (v *ValidationServiceImpl) GetDrift(ctx context.Context, gitYAML []byte) (*GetDriftResponse, error) {
	if len(gitYAML) == 0 {
		return nil, fmt.Errorf("git manifests cannot be empty")
	}
	if len(gitYAML) > maxRequestPayload {
		return nil, fmt.Errorf("git manifests exceed maximum size of %d bytes", maxRequestPayload)
	}

	drifts, summary, err := operator.ComputeDrift(v.cache, gitYAML)
	if err != nil {
		return nil, fmt.Errorf("computing drift: %w", err)
	}

	resp := &GetDriftResponse{
		Summary: &DriftSummaryProto{
			MissingInCluster: int32(summary.MissingInCluster),
			MissingInGit:     int32(summary.MissingInGit),
			SpecDrift:        int32(summary.SpecDrift),
			Total:            int32(summary.Total),
		},
	}

	for _, drift := range drifts {
		entry := &DriftEntry{
			ResourceKey: drift.ResourceKey,
			Kind:        drift.Kind,
			Name:        drift.Name,
			Namespace:   drift.Namespace,
			DriftType:   string(drift.DriftType),
		}
		for _, fc := range drift.Changes {
			entry.FieldChanges = append(entry.FieldChanges, &FieldChange{
				Path:     fc.Path,
				Action:   string(fc.Action),
				OldValue: fmt.Sprintf("%v", fc.OldValue),
				NewValue: fmt.Sprintf("%v", fc.NewValue),
			})
		}
		resp.Drifts = append(resp.Drifts, entry)
	}

	return resp, nil
}

// ResolveResources takes proposed manifests (Claims/XRs) and resolves them through
// the composition chain to return the actual managed resources from the live cluster.
func (v *ValidationServiceImpl) ResolveResources(ctx context.Context, proposedYAML []byte) (*ResolveResourcesResponse, error) {
	if len(proposedYAML) == 0 {
		return nil, fmt.Errorf("proposed manifests cannot be empty")
	}

	proposed, err := manifest.ParseBytes(proposedYAML)
	if err != nil {
		return nil, fmt.Errorf("parsing manifests: %w", err)
	}

	var allResolved []unstructured.Unstructured

	// Resolve Claims
	for _, claim := range proposed.Claims {
		resolved := operator.ResolveAllManagedResources(v.cache, &claim)
		allResolved = append(allResolved, resolved...)
	}

	// Resolve XRs
	for _, xr := range proposed.XRs {
		resolved := operator.ResolveAllManagedResources(v.cache, &xr)
		allResolved = append(allResolved, resolved...)
	}

	// Include direct managed resources as-is
	allResolved = append(allResolved, proposed.ManagedResources...)

	resp := &ResolveResourcesResponse{
		Total: int32(len(allResolved)),
	}

	for _, res := range allResolved {
		protoRes, err := toProtoResource(res)
		if err != nil {
			log.Printf("skipping resource %s/%s: %v", res.GetKind(), res.GetName(), err)
			continue
		}
		resp.Resources = append(resp.Resources, protoRes)
	}

	return resp, nil
}

// GetResourceStatus returns detailed status for a specific Crossplane resource.
func (v *ValidationServiceImpl) GetResourceStatus(ctx context.Context, apiVersion, kind, name, namespace string) (*GetResourceStatusResponse, error) {
	res := v.cache.GetResource(apiVersion, kind, namespace, name)
	if res == nil {
		return nil, fmt.Errorf("resource %s/%s not found", kind, name)
	}

	protoRes, err := toProtoResource(*res)
	if err != nil {
		return nil, fmt.Errorf("serializing resource: %w", err)
	}

	return &GetResourceStatusResponse{Resource: protoRes}, nil
}

func toProtoResource(res unstructured.Unstructured) (*Resource, error) {
	raw, err := json.Marshal(res.Object)
	if err != nil {
		return nil, fmt.Errorf("marshaling %s/%s: %w", res.GetKind(), res.GetName(), err)
	}

	return &Resource{
		APIVersion:   res.GetAPIVersion(),
		Kind:         res.GetKind(),
		Name:         res.GetName(),
		Namespace:    res.GetNamespace(),
		RawJSON:      raw,
		ExternalName: res.GetAnnotations()["crossplane.io/external-name"],
		Status:       extractStatus(res),
	}, nil
}

func extractGroup(apiVersion string) string {
	for i, ch := range apiVersion {
		if ch == '/' {
			return apiVersion[:i]
		}
	}
	return ""
}

func extractStatus(res unstructured.Unstructured) *ResourceStatus {
	status := &ResourceStatus{}

	conditions, found, _ := unstructured.NestedSlice(res.Object, "status", "conditions")
	if !found {
		return status
	}

	for _, item := range conditions {
		cond, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)
		reason, _ := cond["reason"].(string)
		message, _ := cond["message"].(string)
		lastTransition, _ := cond["lastTransitionTime"].(string)

		status.Conditions = append(status.Conditions, &Condition{
			Type:               condType,
			Status:             condStatus,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: lastTransition,
		})

		if condType == "Ready" && condStatus == "True" {
			status.Ready = true
		}
		if condType == "Synced" && condStatus == "True" {
			status.Synced = true
		}
	}

	return status
}

func convertPlanResult(diffResult *diff.DiffResult) *PlanResult {
	result := &PlanResult{
		Summary: &PlanSummary{
			ToAdd:    int32(diffResult.Summary.ToAdd),
			ToChange: int32(diffResult.Summary.ToChange),
			ToDelete: int32(diffResult.Summary.ToDelete),
			NoOp:     int32(diffResult.Summary.NoOp),
		},
	}

	for _, rd := range diffResult.Diffs {
		change := &ResourceChange{
			Action:     string(rd.Action),
			Kind:       rd.Kind,
			Name:       rd.Name,
			Namespace:  rd.Namespace,
			APIVersion: rd.APIVersion,
			Source:     rd.Source,
		}

		for _, fc := range rd.FieldChanges {
			change.FieldChanges = append(change.FieldChanges, &FieldChange{
				Path:     fc.Path,
				Action:   string(fc.Action),
				OldValue: fmt.Sprintf("%v", fc.OldValue),
				NewValue: fmt.Sprintf("%v", fc.NewValue),
			})
		}

		result.Changes = append(result.Changes, change)
	}

	return result
}

// HealthResponse contains operator health and cache statistics.
type HealthResponse struct {
	Healthy         bool
	CachedResources int32
	Uptime          string
	WatchedGroups   []string
}

// GetClusterStateResponse contains cached Crossplane resources.
type GetClusterStateResponse struct {
	Resources []*Resource
	Total     int32
}

// Resource represents a single Crossplane resource with status metadata.
type Resource struct {
	APIVersion   string
	Kind         string
	Name         string
	Namespace    string
	RawJSON      []byte
	ExternalName string
	Status       *ResourceStatus
}

// ResourceStatus holds the readiness and sync state of a Crossplane resource.
type ResourceStatus struct {
	Ready      bool
	Synced     bool
	Conditions []*Condition
}

// Condition represents a single Kubernetes-style status condition.
type Condition struct {
	Type               string
	Status             string
	Reason             string
	Message            string
	LastTransitionTime string
}

// ComputePlanResponse contains the plan result, validation issues, and drift warnings.
type ComputePlanResponse struct {
	Plan             *PlanResult
	ComposedChanges  []ComposedResourceChange `json:",omitempty"`
	ResourceTree     []ResourceTreeNode       `json:",omitempty"`
	ValidationIssues []*ValidationIssue
	DriftWarnings    []*DriftWarning
	ClusterInfo      *ClusterInfo
}

// ClusterInfo contains metadata about the operator's cluster connection.
type ClusterInfo struct {
	CachedResources int32
	CacheAge        string
	ClusterName     string
}

// PlanResult holds the computed changes between live state and proposed manifests.
type PlanResult struct {
	Changes []*ResourceChange
	Summary *PlanSummary
}

// ResourceChange describes a planned change to a single resource.
type ResourceChange struct {
	Action       string
	Kind         string
	Name         string
	Namespace    string
	APIVersion   string
	FieldChanges []*FieldChange
	Destructive  bool
	Source       string
}

// FieldChange describes a change to a single field within a resource.
type FieldChange struct {
	Path     string
	Action   string
	OldValue string
	NewValue string
}

// PlanSummary counts resources by planned action.
type PlanSummary struct {
	ToAdd    int32
	ToChange int32
	ToDelete int32
	NoOp     int32
}

// ValidationIssue represents a schema validation finding.
type ValidationIssue struct {
	Severity string
	Resource string
	Field    string
	Message  string
}

// DriftWarning flags a difference between proposed and live state.
type DriftWarning struct {
	ResourceKey string
	Message     string
	Severity    string
}

// GetDriftResponse contains drift analysis results.
type GetDriftResponse struct {
	Drifts  []*DriftEntry
	Summary *DriftSummaryProto
}

// DriftEntry represents a single drifted resource.
type DriftEntry struct {
	ResourceKey  string
	Kind         string
	Name         string
	Namespace    string
	DriftType    string
	FieldChanges []*FieldChange
}

// DriftSummaryProto counts drifted resources by category.
type DriftSummaryProto struct {
	MissingInCluster int32
	MissingInGit     int32
	SpecDrift        int32
	Total            int32
}

// GetResourceStatusResponse contains a resource and its composed children.
type GetResourceStatusResponse struct {
	Resource *Resource
	Children []*Resource
}

// convertTree converts an operator ResourceTreeNode to the server's type.
func convertTree(node *operator.ResourceTreeNode) *ResourceTreeNode {
	if node == nil {
		return nil
	}
	n := &ResourceTreeNode{
		Kind:       node.Kind,
		Name:       node.Name,
		Namespace:  node.Namespace,
		APIVersion: node.APIVersion,
	}
	for _, child := range node.Children {
		c := convertTree(child)
		if c != nil {
			n.Children = append(n.Children, *c)
		}
	}
	return n
}
