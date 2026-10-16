package server_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

// toyStore is the smallest possible Store: the five required methods and
// nothing else. It tracks no versions (so the server hashes ETags) and
// implements no Patcher (so PATCH runs the Get → Apply → Replace
// fallback) — it exists to prove the minimal contract is sufficient.
type toyStore struct {
	rt    scim.ResourceType
	mu    sync.RWMutex
	items map[string]*scim.Resource
}

func newToyStore(rt scim.ResourceType) *toyStore { //nolint:gocritic // by-value rt matches scim's constructor style
	return &toyStore{rt: rt, items: make(map[string]*scim.Resource)}
}

func (s *toyStore) Create(_ context.Context, res *scim.Resource) (*scim.Resource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := res.Clone()
	id, _ := stored.Get("id")
	idStr, _ := id.(string)
	if idStr == "" {
		gen, err := scim.NewID()
		if err != nil {
			return nil, err
		}
		idStr = gen
		if err := stored.Set("id", idStr); err != nil {
			return nil, err
		}
	}
	if _, exists := s.items[idStr]; exists {
		return nil, scim.ErrUniqueness("id already exists")
	}
	s.items[idStr] = stored
	return stored.Clone(), nil
}

func (s *toyStore) Get(_ context.Context, id string) (*scim.Resource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res, ok := s.items[id]
	if !ok {
		return nil, scim.ErrNotFound("resource " + id + " not found")
	}
	return res.Clone(), nil
}

func (s *toyStore) List(_ context.Context, q server.Query) (server.Page, error) {
	s.mu.RLock()
	all := make([]*scim.Resource, 0, len(s.items))
	for _, res := range s.items {
		all = append(all, res.Clone())
	}
	s.mu.RUnlock()

	matched := make([]*scim.Resource, 0, len(all))
	for _, res := range all {
		if q.Filter != nil {
			ok, err := filter.Match(res, q.Filter)
			if err != nil {
				return server.Page{}, err
			}
			if !ok {
				continue
			}
		}
		matched = append(matched, res)
	}
	if q.SortBy != nil {
		server.SortResources(matched, &s.rt, q.SortBy, q.Ascending)
	}
	total := len(matched)
	return server.Page{Resources: toyPage(matched, q.StartIndex, q.Count), TotalResults: total}, nil
}

func toyPage(list []*scim.Resource, startIndex, count int) []*scim.Resource {
	offset := startIndex - 1
	if offset < 0 || offset >= len(list) || count == 0 {
		return nil
	}
	end := min(offset+count, len(list))
	return list[offset:end]
}

func (s *toyStore) Replace(_ context.Context, id string, res *scim.Resource) (*scim.Resource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return nil, scim.ErrNotFound("resource " + id + " not found")
	}
	stored := res.Clone()
	if err := stored.Set("id", id); err != nil {
		return nil, err
	}
	s.items[id] = stored
	return stored.Clone(), nil
}

func (s *toyStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return scim.ErrNotFound("resource " + id + " not found")
	}
	delete(s.items, id)
	return nil
}

// newToyHarness serves User and Group backed entirely by toyStore.
func newToyHarness(t *testing.T) *harness {
	t.Helper()
	h, err := server.New(server.Config{
		BaseURL: "https://example.com/scim/v2",
		ResourceTypes: []server.Registration{
			{Type: scim.UserResourceType(), Store: newToyStore(scim.UserResourceType())},
			{Type: scim.GroupResourceType(), Store: newToyStore(scim.GroupResourceType())},
		},
		Profile:          patch.Default,
		MaxFilterResults: 100,
		Logger:           discardLogger(),
	})
	require.NoError(t, err)
	ts := newTestServer(t, h)
	return &harness{t: t, server: ts}
}

func TestToyStoreLifecycle(t *testing.T) {
	h := newToyHarness(t)

	// Create → the server hashes the ETag since the store tracks no version.
	id, created := h.createUser("toy@example.com")
	etag := created.header.Get("ETag")
	require.NotEmpty(t, etag, "server computes a weak ETag from content")
	assert.Contains(t, etag, `W/"`)

	// Get, filtered list, PATCH via fallback, delete.
	got := h.do(http.MethodGet, "/Users/"+id, "", nil)
	assert.Equal(t, http.StatusOK, got.status)

	list := h.do(http.MethodGet, `/Users?filter=`+url("userName eq \"toy@example.com\""), "", nil)
	require.Equal(t, http.StatusOK, list.status)
	var lr scim.ListResponse
	require.NoError(t, lr.UnmarshalJSON(list.body))
	assert.Equal(t, 1, lr.TotalResults, "filter push-down works against a minimal store")

	patchBody := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],` +
		`"Operations":[{"op":"replace","path":"displayName","value":"Toy User"}]}`
	patched := h.do(http.MethodPatch, "/Users/"+id, patchBody, nil)
	require.Equal(t, http.StatusOK, patched.status, "%s", patched.body)
	pRes, err := scim.DecodeResource(patched.body, ptr(scim.UserResourceType()))
	require.NoError(t, err)
	dn, _ := pRes.Get("displayName")
	assert.Equal(t, "Toy User", dn, "PATCH fallback applied the op")

	del := h.do(http.MethodDelete, "/Users/"+id, "", nil)
	assert.Equal(t, http.StatusNoContent, del.status)
}

// TestToyStorePatchStormFallback drives the server's striped-lock
// Get → Apply → Replace fallback under concurrency: no native Patcher, so
// serialization is the server's responsibility alone.
func TestToyStorePatchStormFallback(t *testing.T) {
	h := newToyHarness(t)
	created := h.do(http.MethodPost, "/Groups", `{"displayName":"ToyStorm"}`, nil)
	require.Equal(t, http.StatusCreated, created.status)
	gRes, err := scim.DecodeResource(created.body, ptr(scim.GroupResourceType()))
	require.NoError(t, err)
	gID, _ := gRes.Get("id")

	const workers = 16
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(n int) {
			defer wg.Done()
			body := fmt.Sprintf(
				`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],`+
					`"Operations":[{"op":"add","path":"members","value":[{"value":"u-%d"}]}]}`, n)
			resp := h.do(http.MethodPatch, "/Groups/"+gID.(string), body, nil)
			assert.Equal(t, http.StatusOK, resp.status)
		}(i)
	}
	wg.Wait()

	final := h.do(http.MethodGet, "/Groups/"+gID.(string), "", nil)
	finalRes, err := scim.DecodeResource(final.body, ptr(scim.GroupResourceType()))
	require.NoError(t, err)
	members, ok := finalRes.Get("members")
	require.True(t, ok)
	assert.Len(t, members, workers, "striped-lock fallback lost no updates")
}
