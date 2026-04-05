// Package operator provides the in-cluster state cache, live plan computation,
// and drift detection for the crossplane-validate operator.
package operator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	k8sutil "github.com/tesserix/crossplane-validation/pkg/k8s"
	"github.com/tesserix/crossplane-validation/pkg/manifest"
)

const rediscoveryInterval = 60 * time.Second

// StateCache maintains an in-memory cache of all Crossplane resources in the cluster
// using dynamic informers. It auto-discovers Crossplane CRDs and watches them for changes.
type StateCache struct {
	mu            sync.RWMutex
	resources     map[string]*unstructured.Unstructured
	dynamicClient dynamic.Interface
	discovery     discovery.DiscoveryInterface
	extraGroups   []string
	watchedGVRs   map[string]bool
	stopChan      chan struct{}
	startedAt     time.Time
}

// NewStateCache creates a state cache that watches Crossplane resources via dynamic informers.
func NewStateCache(dynamicClient dynamic.Interface, discoveryClient discovery.DiscoveryInterface, extraGroups []string) *StateCache {
	return &StateCache{
		resources:     make(map[string]*unstructured.Unstructured),
		dynamicClient: dynamicClient,
		discovery:     discoveryClient,
		extraGroups:   extraGroups,
		watchedGVRs:   make(map[string]bool),
		stopChan:      make(chan struct{}),
		startedAt:     time.Now(),
	}
}

// Start begins watching Crossplane resources and populating the cache.
func (c *StateCache) Start(ctx context.Context) error {
	if err := c.startInformers(ctx); err != nil {
		return fmt.Errorf("starting informers: %w", err)
	}

	go c.rediscoveryLoop(ctx)

	return nil
}

// Stop shuts down all informers and stops the rediscovery loop.
func (c *StateCache) Stop() {
	close(c.stopChan)
}

// GetResourceSet returns a snapshot of all cached resources as a classified ResourceSet.
func (c *StateCache) GetResourceSet() *manifest.ResourceSet {
	c.mu.RLock()
	defer c.mu.RUnlock()

	items := make([]unstructured.Unstructured, 0, len(c.resources))
	for _, r := range c.resources {
		items = append(items, *r.DeepCopy())
	}

	return manifest.FromUnstructuredList(items)
}

// GetResource returns a deep copy of a specific cached resource, or nil if not found.
func (c *StateCache) GetResource(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := resourceKey(apiVersion, kind, namespace, name)
	if r, ok := c.resources[key]; ok {
		return r.DeepCopy()
	}
	return nil
}

// AllResources returns deep copies of all cached resources as a flat slice.
func (c *StateCache) AllResources() []unstructured.Unstructured {
	c.mu.RLock()
	defer c.mu.RUnlock()

	items := make([]unstructured.Unstructured, 0, len(c.resources))
	for _, r := range c.resources {
		items = append(items, *r.DeepCopy())
	}
	return items
}

// ResourceCount returns the number of resources currently in the cache.
func (c *StateCache) ResourceCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.resources)
}

// Uptime returns the duration since the cache was created.
func (c *StateCache) Uptime() time.Duration {
	return time.Since(c.startedAt)
}

// WatchedGroups returns the API groups currently being watched.
func (c *StateCache) WatchedGroups() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	groups := make(map[string]bool)
	for gvrKey := range c.watchedGVRs {
		parts := strings.SplitN(gvrKey, "/", 3)
		if len(parts) >= 1 {
			groups[parts[0]] = true
		}
	}

	result := make([]string, 0, len(groups))
	for g := range groups {
		result = append(result, g)
	}
	return result
}

func (c *StateCache) startInformers(ctx context.Context) error {
	gvrs, err := k8sutil.DiscoverCrossplaneResources(c.discovery, c.extraGroups)
	if err != nil {
		return err
	}

	xrGroups, err := k8sutil.DiscoverXRGroups(c.discovery)
	if err != nil {
		log.Printf("failed to discover XR groups: %v", err)
	}

	if len(xrGroups) > 0 {
		xrGVRs, err := k8sutil.DiscoverCrossplaneResources(c.discovery, xrGroups)
		if err == nil {
			gvrs = append(gvrs, xrGVRs...)
		}
	}

	if len(gvrs) == 0 {
		log.Println("no Crossplane resources discovered, will retry on next rediscovery cycle")
		return nil
	}

	factory := dynamicinformer.NewDynamicSharedInformerFactory(c.dynamicClient, 30*time.Second)

	for _, gvr := range gvrs {
		gvrKey := gvrToKey(gvr)
		if c.watchedGVRs[gvrKey] {
			continue
		}

		informer := factory.ForResource(gvr)
		informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.onAdd(obj)
			},
			UpdateFunc: func(_, obj interface{}) {
				c.onAdd(obj)
			},
			DeleteFunc: func(obj interface{}) {
				c.onDelete(obj)
			},
		})

		func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.watchedGVRs[gvrKey] = true
		}()
	}

	factory.Start(c.stopChan)
	factory.WaitForCacheSync(c.stopChan)

	log.Printf("watching %d resource types across %d API groups", len(gvrs), len(c.WatchedGroups()))

	return nil
}

func (c *StateCache) rediscoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(rediscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := c.startInformers(ctx); err != nil {
				log.Printf("rediscovery error: %v", err)
			}
		case <-c.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *StateCache) onAdd(obj interface{}) {
	u, ok := toUnstructured(obj)
	if !ok {
		return
	}

	key := resourceKeyFromObj(u)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resources[key] = u
}

func (c *StateCache) onDelete(obj interface{}) {
	u, ok := toUnstructured(obj)
	if !ok {
		if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			u, ok = toUnstructured(tombstone.Obj)
			if !ok {
				return
			}
		} else {
			return
		}
	}

	key := resourceKeyFromObj(u)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.resources, key)
}

func toUnstructured(obj interface{}) (*unstructured.Unstructured, bool) {
	u, ok := obj.(*unstructured.Unstructured)
	return u, ok
}

func resourceKey(apiVersion, kind, namespace, name string) string {
	if namespace != "" {
		return fmt.Sprintf("%s/%s/%s/%s", apiVersion, kind, namespace, name)
	}
	return fmt.Sprintf("%s/%s/%s", apiVersion, kind, name)
}

func resourceKeyFromObj(u *unstructured.Unstructured) string {
	return resourceKey(u.GetAPIVersion(), u.GetKind(), u.GetNamespace(), u.GetName())
}

func gvrToKey(gvr schema.GroupVersionResource) string {
	return fmt.Sprintf("%s/%s/%s", gvr.Group, gvr.Version, gvr.Resource)
}
