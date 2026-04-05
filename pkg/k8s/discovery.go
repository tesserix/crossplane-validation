// Package k8s provides Kubernetes utilities for CRD discovery and port-forwarding.
package k8s

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

var defaultWatchGroups = []string{
	"apiextensions.crossplane.io",
	"pkg.crossplane.io",
}

// IsCrossplaneGroup returns true if the API group belongs to Crossplane or an Upbound provider.
func IsCrossplaneGroup(group string) bool {
	if strings.HasSuffix(group, ".upbound.io") || group == "upbound.io" {
		return true
	}
	if strings.HasSuffix(group, ".crossplane.io") {
		return true
	}
	return false
}

// DiscoverCrossplaneResources queries the API server for all listable/watchable Crossplane resources.
func DiscoverCrossplaneResources(client discovery.DiscoveryInterface, extraGroups []string) ([]schema.GroupVersionResource, error) {
	_, apiResourceLists, err := client.ServerGroupsAndResources()
	if err != nil {
		if !discovery.IsGroupDiscoveryFailedError(err) {
			return nil, err
		}
	}

	watchGroups := make(map[string]bool)
	for _, g := range defaultWatchGroups {
		watchGroups[g] = true
	}
	for _, g := range extraGroups {
		watchGroups[g] = true
	}

	var resources []schema.GroupVersionResource

	for _, list := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}

		if !shouldWatch(gv.Group, watchGroups) {
			continue
		}

		for _, r := range list.APIResources {
			if !hasVerb(r, "list") || !hasVerb(r, "watch") {
				continue
			}
			if strings.Contains(r.Name, "/") {
				continue // skip subresources
			}

			resources = append(resources, schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: r.Name,
			})
		}
	}

	return resources, nil
}

// DiscoverXRGroups finds API groups that contain Composite Resource definitions.
func DiscoverXRGroups(client discovery.DiscoveryInterface) ([]string, error) {
	_, apiResourceLists, err := client.ServerGroupsAndResources()
	if err != nil {
		if !discovery.IsGroupDiscoveryFailedError(err) {
			return nil, err
		}
	}

	var groups []string
	seen := make(map[string]bool)

	for _, list := range apiResourceLists {
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}

		if seen[gv.Group] {
			continue
		}

		for _, r := range list.APIResources {
			if isXRAPIGroup(gv.Group, r) {
				groups = append(groups, gv.Group)
				seen[gv.Group] = true
				break
			}
		}
	}

	return groups, nil
}

func shouldWatch(group string, extraGroups map[string]bool) bool {
	if IsCrossplaneGroup(group) {
		return true
	}
	if extraGroups[group] {
		return true
	}
	return false
}

// excludedGroups are API groups that are NOT Crossplane XR types.
var excludedGroups = map[string]bool{
	"argoproj.io": true, "fluxcd.io": true,
	"velero.io": true, "kargo.akuity.io": true,
	"knative.dev": true, "serving.knative.dev": true, "sources.knative.dev": true,
	"eventing.knative.dev": true, "messaging.knative.dev": true,
	"gatekeeper.sh": true, "templates.gatekeeper.sh": true, "constraints.gatekeeper.sh": true,
	"config.gatekeeper.sh": true, "status.gatekeeper.sh": true,
	"crd.projectcalico.org": true, "projectcalico.org": true,
	"istio.io": true, "networking.istio.io": true, "security.istio.io": true,
	"telemetry.istio.io": true, "extensions.istio.io": true,
	"cert-manager.io": true, "acme.cert-manager.io": true,
	"monitoring.coreos.com": true, "logging.banzaicloud.io": true,
	"keda.sh": true, "snapshot.storage.k8s.io": true,
	"metrics.k8s.io": true, "external-secrets.io": true,
	"generators.external-secrets.io": true,
}

func isExcludedGroup(group string) bool {
	if excludedGroups[group] {
		return true
	}
	for excluded := range excludedGroups {
		if strings.HasSuffix(group, "."+excluded) {
			return true
		}
	}
	return false
}

func isXRAPIGroup(group string, r metav1.APIResource) bool {
	if IsCrossplaneGroup(group) {
		return false
	}
	if isCoreKubernetesGroup(group) {
		return false
	}
	if isExcludedGroup(group) {
		return false
	}
	if strings.Contains(group, ".") && hasVerb(r, "list") {
		return true
	}
	return false
}

func isCoreKubernetesGroup(group string) bool {
	coreGroups := map[string]bool{
		"": true, "apps": true, "batch": true, "rbac.authorization.k8s.io": true,
		"networking.k8s.io": true, "policy": true, "storage.k8s.io": true,
		"admissionregistration.k8s.io": true, "certificates.k8s.io": true,
		"coordination.k8s.io": true, "events.k8s.io": true, "discovery.k8s.io": true,
		"node.k8s.io": true, "scheduling.k8s.io": true, "autoscaling": true,
		"apiextensions.k8s.io": true, "apiregistration.k8s.io": true,
	}
	return coreGroups[group]
}

func hasVerb(r metav1.APIResource, verb string) bool {
	for _, v := range r.Verbs {
		if v == verb {
			return true
		}
	}
	return false
}
