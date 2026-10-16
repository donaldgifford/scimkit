package memstore

import (
	"context"

	"github.com/donaldgifford/scimkit/scim"
)

// Create stores a new resource. The server has already assigned id and
// meta; a missing id gets one here so the store also works standalone.
// An id collision or a uniqueness-constrained value already in use is
// scim.ErrUniqueness.
func (s *Store) Create(_ context.Context, res *scim.Resource) (*scim.Resource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := res.Clone()
	id, ok := stored.Get("id")
	idStr, isStr := id.(string)
	if !ok || !isStr || idStr == "" {
		generated, err := scim.NewID()
		if err != nil {
			return nil, err
		}
		idStr = generated
		if err := stored.Set("id", idStr); err != nil {
			return nil, err
		}
	}
	if _, exists := s.items[idStr]; exists {
		return nil, scim.ErrUniqueness("id " + idStr + " already exists")
	}
	if err := s.checkUnique(idStr, stored); err != nil {
		return nil, err
	}
	s.bumpVersion(idStr, stored)
	s.items[idStr] = stored
	s.reindex(idStr, nil, stored)
	return stored.Clone(), nil
}

// Get returns a clone of the resource, or scim.ErrNotFound.
func (s *Store) Get(_ context.Context, id string) (*scim.Resource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res, ok := s.items[id]
	if !ok {
		return nil, scim.ErrNotFound("resource " + id + " not found")
	}
	return res.Clone(), nil
}

// Replace swaps the stored resource. The incoming resource's
// meta.version is a compare-and-swap token: when set, a mismatch with
// the stored version fails with scim.ErrPreconditionFailed; the store
// bumps the version itself on success.
func (s *Store) Replace(_ context.Context, id string, res *scim.Resource) (*scim.Resource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return nil, scim.ErrNotFound("resource " + id + " not found")
	}
	if token := res.Meta().Version; token != "" && token != cur.Meta().Version {
		return nil, scim.ErrPreconditionFailed("version mismatch for resource " + id)
	}
	stored := res.Clone()
	if err := stored.Set("id", id); err != nil {
		return nil, err
	}
	if err := s.checkUnique(id, stored); err != nil {
		return nil, err
	}
	s.bumpVersion(id, stored)
	s.reindex(id, cur, stored)
	s.items[id] = stored
	return stored.Clone(), nil
}

// Delete removes the resource, or scim.ErrNotFound.
func (s *Store) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.items[id]
	if !ok {
		return scim.ErrNotFound("resource " + id + " not found")
	}
	s.reindex(id, cur, nil)
	delete(s.items, id)
	delete(s.seq, id)
	return nil
}
