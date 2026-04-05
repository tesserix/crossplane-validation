package operator

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// NewStateCacheForTest creates a StateCache without k8s clients, suitable for unit tests.
func NewStateCacheForTest() *StateCache {
	return &StateCache{
		resources:   make(map[string]*unstructured.Unstructured),
		watchedGVRs: make(map[string]bool),
		stopChan:    make(chan struct{}),
		startedAt:   time.Now(),
	}
}

// AddForTest adds a resource directly to the cache, bypassing informers.
func (c *StateCache) AddForTest(obj *unstructured.Unstructured) {
	c.onAdd(obj)
}
