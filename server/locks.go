package server

import (
	"hash/fnv"
	"sync"
)

// lockStripes bounds lock memory regardless of resource count; 32 is
// plenty for the handful of concurrent writers IdPs run per tenant.
const lockStripes = 32

// stripedLocks serializes read-modify-write cycles per resource id. Two
// ids may share a stripe — that costs contention, never correctness.
type stripedLocks struct {
	stripes [lockStripes]sync.Mutex
}

// lock acquires the stripe for a resource and returns its unlocker.
func (s *stripedLocks) lock(endpoint, id string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(endpoint))
	_, _ = h.Write([]byte{'/'})
	_, _ = h.Write([]byte(id))
	mu := &s.stripes[h.Sum32()%lockStripes]
	mu.Lock()
	return mu.Unlock
}
