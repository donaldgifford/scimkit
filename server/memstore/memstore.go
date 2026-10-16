// Package memstore is the in-memory reference implementation of the
// server.Store contract: native filter evaluation, case-folded value
// indexes, a native Patcher with set-semantics group members, and
// version-based compare-and-swap on Replace. It backs tests, the mock
// server binary, and any deployment whose resource set fits in memory.
package memstore

import (
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// Store holds one resource type's resources. All methods are safe for
// concurrent use; every resource crossing the boundary — in or out — is
// cloned, so callers can never alias internal state.
//
// Unsorted lists come back in insertion order, stable across calls, so
// clients can page a static data set without duplicates or gaps.
type Store struct {
	rt      scim.ResourceType
	profile patch.Profile

	mu     sync.RWMutex
	items  map[string]*scim.Resource
	seq    map[string]uint64
	order  []string          // ids in insertion order — the unsorted List order
	rank   map[string]uint64 // id → insertion rank, to order index hits the same way
	clock  uint64            // last rank handed out
	unique []string
	extra  []string
	index  map[string]map[string]map[string]struct{}
}

// Option configures a Store.
type Option func(*Store)

// WithProfile sets the compatibility profile the native Patcher applies
// ops under. Keep it aligned with the server's Config.Profile; the
// default is patch.Default.
func WithProfile(p patch.Profile) Option {
	return func(s *Store) { s.profile = p }
}

// WithIndex adds a case-folded value index on a top-level string
// attribute beyond the schema's uniqueness-constrained ones, so eq
// filters on it shortcut the scan.
func WithIndex(attr string) Option {
	return func(s *Store) { s.extra = append(s.extra, scim.FoldName(attr)) }
}

// New builds an empty store for one resource type. Attributes with a
// server or global uniqueness constraint are indexed automatically and
// enforced on write with scim.ErrUniqueness.
//
//nolint:gocritic // rt is passed by value on purpose: the store keeps its own copy, matching scim's constructor style
func New(rt scim.ResourceType, opts ...Option) *Store {
	s := &Store{
		rt:      rt,
		profile: patch.Default,
		items:   make(map[string]*scim.Resource),
		seq:     make(map[string]uint64),
		rank:    make(map[string]uint64),
		index:   make(map[string]map[string]map[string]struct{}),
	}
	for i := range rt.Schema.Attributes {
		a := &rt.Schema.Attributes[i]
		if a.Uniqueness != "" && a.Uniqueness != scim.UniquenessNone {
			s.unique = append(s.unique, scim.FoldName(a.Name))
		}
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// insert records a new id at the end of the insertion order. Caller holds
// the write lock.
func (s *Store) insert(id string, res *scim.Resource) {
	s.clock++
	s.rank[id] = s.clock
	s.order = append(s.order, id)
	s.items[id] = res
}

// remove drops an id from every structure. Caller holds the write lock.
func (s *Store) remove(id string) {
	delete(s.items, id)
	delete(s.seq, id)
	delete(s.rank, id)
	if i := slices.Index(s.order, id); i >= 0 {
		s.order = slices.Delete(s.order, i, i+1)
	}
}

// indexedAttrs returns every indexed attribute: unique ones plus
// explicitly configured ones.
func (s *Store) indexedAttrs() []string {
	out := make([]string, 0, len(s.unique)+len(s.extra))
	out = append(out, s.unique...)
	return append(out, s.extra...)
}

// indexValues extracts the case-folded index terms one resource
// contributes for an attribute: string values only, multi-valued
// attributes contribute every element.
func indexValues(res *scim.Resource, attr string) []string {
	v, ok := res.Get(attr)
	if !ok {
		return nil
	}
	switch t := v.(type) {
	case string:
		return []string{scim.FoldName(t)}
	case []string:
		out := make([]string, len(t))
		for i, e := range t {
			out[i] = scim.FoldName(e)
		}
		return out
	default:
		return nil
	}
}

// reindex removes old's terms and adds cur's for one id. Either side
// may be nil (create / delete).
func (s *Store) reindex(id string, old, cur *scim.Resource) {
	for _, attr := range s.indexedAttrs() {
		if old != nil {
			for _, term := range indexValues(old, attr) {
				if ids := s.index[attr][term]; ids != nil {
					delete(ids, id)
					if len(ids) == 0 {
						delete(s.index[attr], term)
					}
				}
			}
		}
		if cur == nil {
			continue
		}
		for _, term := range indexValues(cur, attr) {
			if s.index[attr] == nil {
				s.index[attr] = make(map[string]map[string]struct{})
			}
			if s.index[attr][term] == nil {
				s.index[attr][term] = make(map[string]struct{})
			}
			s.index[attr][term][id] = struct{}{}
		}
	}
}

// checkUnique rejects a write whose uniqueness-constrained values are
// already held by another resource.
func (s *Store) checkUnique(id string, res *scim.Resource) error {
	for _, attr := range s.unique {
		for _, term := range indexValues(res, attr) {
			for holder := range s.index[attr][term] {
				if holder != id {
					return scim.ErrUniqueness("value of " + attr + " is already in use")
				}
			}
		}
	}
	return nil
}

// bumpVersion advances a resource's version counter and stamps the
// resulting weak-ETag form into meta.version.
func (s *Store) bumpVersion(id string, res *scim.Resource) {
	s.seq[id]++
	m := res.Meta()
	m.Version = versionString(s.seq[id])
	res.SetMeta(m)
}

func versionString(n uint64) string {
	return `W/"` + strconv.FormatUint(n, 10) + `"`
}

// opaqueVersion reduces a version token to its opaque part for the
// RFC 7232 §2.3.2 weak comparison: W/"3", "3", and 3 all name the same
// version. Clients echo whatever ETag form they were given — or none of
// the decoration at all — and the store must not fail a swap over it.
func opaqueVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "W/")
	return strings.Trim(v, `"`)
}

// versionMatches is the compare-and-swap test: an empty token always
// matches (no precondition), otherwise opaque parts must agree.
func versionMatches(token, current string) bool {
	return token == "" || opaqueVersion(token) == opaqueVersion(current)
}
