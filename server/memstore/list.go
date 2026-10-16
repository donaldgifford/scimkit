package memstore

import (
	"context"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

// List evaluates the query natively: an eq filter over an indexed
// attribute shortcuts candidate selection, everything else scans with
// filter.Match. TotalResults is always the filtered total; Count 0
// returns totals only.
func (s *Store) List(_ context.Context, q server.Query) (server.Page, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	matched, err := s.matchCandidates(q.Filter)
	if err != nil {
		return server.Page{}, err
	}
	server.SortResources(matched, &s.rt, q.SortBy, q.Ascending)
	total := len(matched)
	window := paginate(matched, q.StartIndex, q.Count)
	out := make([]*scim.Resource, len(window))
	for i, res := range window {
		out[i] = res.Clone()
	}
	return server.Page{Resources: out, TotalResults: total}, nil
}

// matchCandidates picks candidates (via the index when the filter
// allows) and confirms each with filter.Match — the index only narrows,
// it never decides, guarding against index drift.
func (s *Store) matchCandidates(f filter.Expr) ([]*scim.Resource, error) {
	candidates := s.candidateSet(f)
	matched := make([]*scim.Resource, 0, len(candidates))
	for _, res := range candidates {
		if f != nil {
			ok, err := filter.Match(res, f)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		matched = append(matched, res)
	}
	return matched, nil
}

// candidateSet returns the resources worth testing: the index bucket
// for a bare eq comparison on an indexed attribute, else everything.
func (s *Store) candidateSet(f filter.Expr) []*scim.Resource {
	if ids, ok := s.indexLookup(f); ok {
		out := make([]*scim.Resource, 0, len(ids))
		for id := range ids {
			if res, exists := s.items[id]; exists {
				out = append(out, res)
			}
		}
		return out
	}
	out := make([]*scim.Resource, 0, len(s.items))
	for _, res := range s.items {
		out = append(out, res)
	}
	return out
}

func (s *Store) indexLookup(f filter.Expr) (map[string]struct{}, bool) {
	cmp, ok := f.(filter.Compare)
	if !ok || cmp.Op != filter.OpEqual || cmp.Path.URN != "" || cmp.Path.SubAttr != "" {
		return nil, false
	}
	want, ok := cmp.Value.(string)
	if !ok {
		return nil, false
	}
	attr := scim.FoldName(cmp.Path.Attr)
	bucket, indexed := s.index[attr]
	if !indexed {
		return nil, false
	}
	return bucket[scim.FoldName(want)], true
}

// paginate applies the normalized 1-based window. Count 0 is a
// count-only query.
func paginate(list []*scim.Resource, startIndex, count int) []*scim.Resource {
	if startIndex < 1 {
		startIndex = 1
	}
	if count <= 0 {
		return nil
	}
	from := startIndex - 1
	if from >= len(list) {
		return nil
	}
	to := from + count
	if to > len(list) {
		to = len(list)
	}
	return list[from:to]
}
