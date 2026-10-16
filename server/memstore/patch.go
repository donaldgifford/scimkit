package memstore

import (
	"context"
	"time"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// Patch is the native Patcher capability: ops apply atomically under
// the store's own lock through the patch engine, whose value-identity
// deduplication gives group members set semantics — re-adding an
// existing member changes nothing. Version and lastModified bump only
// when something actually changed; an idempotent PatchOp returns the
// stored resource untouched.
func (s *Store) Patch(_ context.Context, id string, ops []patch.Op) (*scim.Resource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return nil, scim.ErrNotFound("resource " + id + " not found")
	}
	result, err := patch.Apply(cur, ops, s.profile)
	if err != nil {
		return nil, err
	}
	if !result.Changed {
		return cur.Clone(), nil
	}
	stored := result.Resource
	if err := s.checkUnique(id, stored); err != nil {
		return nil, err
	}
	m := stored.Meta()
	m.LastModified = time.Now().UTC()
	stored.SetMeta(m)
	s.bumpVersion(id, stored)
	s.reindex(id, cur, stored)
	s.items[id] = stored
	return stored.Clone(), nil
}
