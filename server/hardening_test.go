package server_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

// newLimitedHarness builds a memstore-backed server with the given
// Config overrides applied on top of the standard test wiring.
func newLimitedHarness(t *testing.T, mutate func(cfg *server.Config)) *harness {
	t.Helper()
	cfg := server.Config{
		BaseURL: "https://example.com/scim/v2",
		ResourceTypes: []server.Registration{
			{Type: scim.UserResourceType(), Store: newToyStore(scim.UserResourceType())},
			{Type: scim.GroupResourceType(), Store: newToyStore(scim.GroupResourceType())},
		},
		Profile:          patch.Default,
		MaxFilterResults: 100,
		Logger:           discardLogger(),
	}
	mutate(&cfg)
	h, err := server.New(cfg)
	require.NoError(t, err)
	return &harness{t: t, server: newTestServer(t, h)}
}

// padJSON pads a JSON object with interior whitespace to exactly n bytes.
func padJSON(t *testing.T, body string, n int) string {
	t.Helper()
	require.GreaterOrEqual(t, n, len(body), "body already longer than target")
	return body[:len(body)-1] + strings.Repeat(" ", n-len(body)) + "}"
}

func TestMaxBodyBytes(t *testing.T) {
	const limit = 96
	h := newLimitedHarness(t, func(cfg *server.Config) { cfg.MaxBodyBytes = limit })
	id, _ := h.createUser("sized@example.com")
	patchOp := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","path":"displayName","value":"x"}]}`

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{
			name: "create at the limit is accepted", method: http.MethodPost, path: "/Users",
			body: padJSON(t, `{"userName":"at@example.com"}`, limit), wantStatus: http.StatusCreated,
		},
		{
			name: "create one byte over is 413", method: http.MethodPost, path: "/Users",
			body: padJSON(t, `{"userName":"over@example.com"}`, limit+1), wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "replace over the limit is 413", method: http.MethodPut, path: "/Users/" + id,
			body: padJSON(t, `{"userName":"sized@example.com"}`, limit+1), wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "patch over the limit is 413", method: http.MethodPatch, path: "/Users/" + id,
			body: patchOp, wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "search over the limit is 413", method: http.MethodPost, path: "/Users/.search",
			body: padJSON(t, `{"filter":"userName pr"}`, limit+1), wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name: "search at the limit is served", method: http.MethodPost, path: "/Users/.search",
			body: padJSON(t, `{"filter":"userName pr"}`, limit), wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(tt.method, tt.path, tt.body, nil)
			require.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			if tt.wantStatus == http.StatusRequestEntityTooLarge {
				scimErr := h.decodeError(resp.body)
				assert.Equal(t, http.StatusRequestEntityTooLarge, scimErr.Status, "413 carries a SCIM error body")
				assert.Equal(t, fmt.Sprintf("request body exceeds %d bytes", limit), scimErr.Detail)
			}
		})
	}
}

func TestMaxPageSizeClamp(t *testing.T) {
	h := newLimitedHarness(t, func(cfg *server.Config) {
		cfg.DefaultPageSize = 1
		cfg.MaxPageSize = 2
	})
	for _, name := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		h.createUser(name)
	}

	tests := []struct {
		name         string
		method       string
		path         string
		body         string
		wantItems    int
		wantPerPage  int
		wantTotal    int
		wantStartIdx int
	}{
		{
			name: "count above max is clamped not rejected", method: http.MethodGet, path: "/Users?count=50",
			wantItems: 2, wantPerPage: 2, wantTotal: 3, wantStartIdx: 1,
		},
		{
			name: "count at max is honored", method: http.MethodGet, path: "/Users?count=2",
			wantItems: 2, wantPerPage: 2, wantTotal: 3, wantStartIdx: 1,
		},
		{
			name: "absent count uses the default", method: http.MethodGet, path: "/Users",
			wantItems: 1, wantPerPage: 1, wantTotal: 3, wantStartIdx: 1,
		},
		{
			name: "clamp applies to search bodies too", method: http.MethodPost, path: "/Users/.search",
			body: `{"count":99,"startIndex":2}`, wantItems: 2, wantPerPage: 2, wantTotal: 3, wantStartIdx: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(tt.method, tt.path, tt.body, nil)
			require.Equal(t, http.StatusOK, resp.status, "%s", resp.body)
			var lr scim.ListResponse
			require.NoError(t, lr.UnmarshalJSON(resp.body))
			assert.Len(t, lr.Resources, tt.wantItems)
			assert.Equal(t, tt.wantPerPage, lr.ItemsPerPage, "itemsPerPage reflects the clamp")
			assert.Equal(t, tt.wantTotal, lr.TotalResults)
			assert.Equal(t, tt.wantStartIdx, lr.StartIndex)
		})
	}
}

func TestSortByUnknownAttribute(t *testing.T) {
	h := newHarness(t, patch.Default)
	h.createUser("a@example.com")

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "query parameter", method: http.MethodGet, path: "/Users?sortBy=nope"},
		{name: "sub-attribute of a simple attribute", method: http.MethodGet, path: "/Users?sortBy=userName.nope"},
		{name: "search body", method: http.MethodPost, path: "/Users/.search", body: `{"sortBy":"notAnAttribute"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(tt.method, tt.path, tt.body, nil)
			require.Equal(t, http.StatusBadRequest, resp.status, "%s", resp.body)
			scimErr := h.decodeError(resp.body)
			assert.Equal(t, scim.ScimTypeInvalidValue, scimErr.ScimType)
			assert.Contains(t, scimErr.Detail, "sortBy")
		})
	}
}

// aliasStore is a toyStore that hands back pointers into its own map
// from every read and write — the laziest legal Store. If the server
// stamps ETags into what it returns, the store's state gets corrupted.
type aliasStore struct {
	*toyStore
}

func (s aliasStore) Get(_ context.Context, id string) (*scim.Resource, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res, ok := s.items[id]
	if !ok {
		return nil, scim.ErrNotFound("resource " + id + " not found")
	}
	return res, nil
}

func (s aliasStore) Create(ctx context.Context, res *scim.Resource) (*scim.Resource, error) {
	created, err := s.toyStore.Create(ctx, res)
	if err != nil {
		return nil, err
	}
	id, _ := created.Get("id")
	return s.Get(ctx, id.(string))
}

func (s aliasStore) Replace(ctx context.Context, id string, res *scim.Resource) (*scim.Resource, error) {
	if _, err := s.toyStore.Replace(ctx, id, res); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s aliasStore) List(_ context.Context, q server.Query) (server.Page, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]*scim.Resource, 0, len(s.items))
	for _, res := range s.items {
		all = append(all, res)
	}
	return server.Page{Resources: toyPage(all, q.StartIndex, q.Count), TotalResults: len(all)}, nil
}

func (s aliasStore) storedVersion(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.items[id].Meta().Version
}

func TestAliasStoreETagContract(t *testing.T) {
	store := aliasStore{toyStore: newToyStore(scim.UserResourceType())}
	srv, err := server.New(server.Config{
		BaseURL:          "https://example.com/scim/v2",
		ResourceTypes:    []server.Registration{{Type: scim.UserResourceType(), Store: store}},
		Profile:          patch.Default,
		MaxFilterResults: 100,
		Logger:           discardLogger(),
	})
	require.NoError(t, err)
	h := &harness{t: t, server: newTestServer(t, srv)}

	id, created := h.createUser("alias@example.com")
	first := created.header.Get("ETag")
	require.NotEmpty(t, first)

	steps := []struct {
		name        string
		method      string
		body        string
		headers     map[string]string
		wantStatus  int
		wantSameTag bool // response ETag equals the create-time tag
	}{
		{name: "first GET returns the content hash", method: http.MethodGet, wantStatus: http.StatusOK, wantSameTag: true},
		{name: "second GET is stable", method: http.MethodGet, wantStatus: http.StatusOK, wantSameTag: true},
		{
			name: "If-None-Match on the unchanged resource is 304", method: http.MethodGet,
			headers: map[string]string{"If-None-Match": first}, wantStatus: http.StatusNotModified, wantSameTag: true,
		},
		{
			name: "PUT with the matching If-Match succeeds and changes the tag", method: http.MethodPut,
			body:    `{"userName":"alias@example.com","displayName":"Renamed"}`,
			headers: map[string]string{"If-Match": first}, wantStatus: http.StatusOK, wantSameTag: false,
		},
		{
			name: "GET after PUT reports the new tag", method: http.MethodGet,
			wantStatus: http.StatusOK, wantSameTag: false,
		},
		{
			name: "stale If-None-Match no longer short-circuits", method: http.MethodGet,
			headers: map[string]string{"If-None-Match": first}, wantStatus: http.StatusOK, wantSameTag: false,
		},
		{
			name: "stale If-Match is 412", method: http.MethodPut,
			body:    `{"userName":"alias@example.com"}`,
			headers: map[string]string{"If-Match": first}, wantStatus: http.StatusPreconditionFailed,
		},
		{name: "list stamps without leaking either", method: http.MethodGet, wantStatus: http.StatusOK, wantSameTag: false},
	}
	for _, st := range steps {
		t.Run(st.name, func(t *testing.T) {
			resp := h.do(st.method, "/Users/"+id, st.body, st.headers)
			require.Equal(t, st.wantStatus, resp.status, "%s", resp.body)
			assert.Empty(t, store.storedVersion(id), "server never writes meta.version into the store's resource")
			if st.wantStatus == http.StatusPreconditionFailed {
				return
			}
			tag := resp.header.Get("ETag")
			require.NotEmpty(t, tag)
			if st.wantSameTag {
				assert.Equal(t, first, tag)
			} else {
				assert.NotEqual(t, first, tag)
			}
			if st.wantStatus == http.StatusOK && st.method == http.MethodGet {
				res, err := scim.DecodeResource(resp.body, ptr(scim.UserResourceType()))
				require.NoError(t, err)
				assert.Equal(t, tag, res.Meta().Version, "body meta.version mirrors the ETag header")
			}
		})
	}

	list := h.do(http.MethodGet, "/Users", "", nil)
	require.Equal(t, http.StatusOK, list.status)
	assert.Empty(t, store.storedVersion(id), "list stamping does not leak into the store either")
}

func TestPatchStrictValidation(t *testing.T) {
	const nonCanonical = `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],` +
		`"Operations":[{"op":"add","path":"emails","value":[{"value":"w@example.com","type":"Work-ish"}]}]}`
	const canonical = `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],` +
		`"Operations":[{"op":"add","path":"emails","value":[{"value":"w@example.com","type":"work"}]}]}`

	tests := []struct {
		name         string
		harness      func(t *testing.T) *harness
		body         string
		wantStatus   int
		wantScimType string
	}{
		{
			name:    "native patcher under strict rejects non-canonical type",
			harness: func(t *testing.T) *harness { t.Helper(); return newHarness(t, patch.Strict) },
			body:    nonCanonical, wantStatus: http.StatusBadRequest, wantScimType: scim.ScimTypeInvalidValue,
		},
		{
			name:    "native patcher under strict accepts canonical type",
			harness: func(t *testing.T) *harness { t.Helper(); return newHarness(t, patch.Strict) },
			body:    canonical, wantStatus: http.StatusOK,
		},
		{
			name: "fallback under strict rejects non-canonical type",
			harness: func(t *testing.T) *harness {
				t.Helper()
				return newLimitedHarness(t, func(cfg *server.Config) { cfg.Profile = patch.Strict })
			},
			body: nonCanonical, wantStatus: http.StatusBadRequest, wantScimType: scim.ScimTypeInvalidValue,
		},
		{
			name:    "native patcher under default tolerates it",
			harness: func(t *testing.T) *harness { t.Helper(); return newHarness(t, patch.Default) },
			body:    nonCanonical, wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.harness(t)
			id, _ := h.createUser("strict@example.com")
			resp := h.do(http.MethodPatch, "/Users/"+id, tt.body, nil)
			require.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			if tt.wantScimType != "" {
				assert.Equal(t, tt.wantScimType, h.decodeError(resp.body).ScimType)
				got := h.do(http.MethodGet, "/Users/"+id, "", nil)
				res, err := scim.DecodeResource(got.body, ptr(scim.UserResourceType()))
				require.NoError(t, err)
				_, has := res.Get("emails")
				assert.False(t, has, "rejected patch persisted nothing")
			}
		})
	}
}

// noSortStore is a toyStore that reports it cannot sort by any path and
// ignores SortBy entirely — the server has to order results itself.
type noSortStore struct {
	*toyStore
}

func (noSortStore) SupportsSort(*filter.Path) bool { return false }

func (s noSortStore) List(ctx context.Context, q server.Query) (server.Page, error) {
	q.SortBy = nil // prove the server, not the store, sorts
	return s.toyStore.List(ctx, q)
}

func TestSortFallback(t *testing.T) {
	newSortServer := func(t *testing.T, maxResults int) *harness {
		t.Helper()
		store := noSortStore{toyStore: newToyStore(scim.UserResourceType())}
		h, err := server.New(server.Config{
			BaseURL:          "https://example.com/scim/v2",
			ResourceTypes:    []server.Registration{{Type: scim.UserResourceType(), Store: store}},
			Profile:          patch.Default,
			MaxFilterResults: maxResults,
			Logger:           discardLogger(),
		})
		require.NoError(t, err)
		return &harness{t: t, server: newTestServer(t, h)}
	}
	seed := func(h *harness) {
		for _, name := range []string{"carol@example.com", "alice@example.com", "bob@example.com"} {
			h.createUser(name)
		}
	}
	names := func(t *testing.T, body []byte) []string {
		t.Helper()
		var lr scim.ListResponse
		require.NoError(t, lr.UnmarshalJSON(body))
		out := make([]string, 0, len(lr.Resources))
		for _, raw := range lr.Resources {
			res, err := scim.DecodeResource(raw, ptr(scim.UserResourceType()))
			require.NoError(t, err)
			v, _ := res.Get("userName")
			out = append(out, v.(string))
		}
		return out
	}

	tests := []struct {
		name       string
		maxResults int
		path       string
		wantStatus int
		wantNames  []string
	}{
		{
			name: "server sorts the whole set", maxResults: 100, path: "/Users?sortBy=userName",
			wantStatus: http.StatusOK, wantNames: []string{"alice@example.com", "bob@example.com", "carol@example.com"},
		},
		{
			name: "pagination applies after the server-side sort", maxResults: 100,
			path:       "/Users?sortBy=userName&startIndex=2&count=1",
			wantStatus: http.StatusOK, wantNames: []string{"bob@example.com"},
		},
		{
			name: "filter is pushed down and the server still sorts", maxResults: 100,
			path:       "/Users?filter=" + url(`userName co "example.com"`) + "&sortBy=userName&sortOrder=descending",
			wantStatus: http.StatusOK, wantNames: []string{"carol@example.com", "bob@example.com", "alice@example.com"},
		},
		{
			name: "sorted set over the cap is tooMany", maxResults: 2, path: "/Users?sortBy=userName",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "unsorted list is unaffected by the cap", maxResults: 2, path: "/Users?count=10",
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newSortServer(t, tt.maxResults)
			seed(h)
			resp := h.do(http.MethodGet, tt.path, "", nil)
			require.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			if tt.wantStatus != http.StatusOK {
				assert.Equal(t, scim.ScimTypeTooMany, h.decodeError(resp.body).ScimType)
				return
			}
			if tt.wantNames != nil {
				assert.Equal(t, tt.wantNames, names(t, resp.body))
			}
		})
	}
}

// TestReplaceDecodesBeforeTouchingStore proves PUT rejects a malformed
// body before taking the lock and reading the store: panicStore's Get
// would turn any store access into a 500.
func TestReplaceDecodesBeforeTouchingStore(t *testing.T) {
	store := panicStore{toyStore: newToyStore(scim.UserResourceType())}
	srv, err := server.New(server.Config{
		BaseURL:          "https://example.com/scim/v2",
		ResourceTypes:    []server.Registration{{Type: scim.UserResourceType(), Store: store}},
		Profile:          patch.Default,
		MaxFilterResults: 100,
		Logger:           discardLogger(),
	})
	require.NoError(t, err)
	h := &harness{t: t, server: newTestServer(t, srv)}

	tests := []struct {
		name         string
		body         string
		wantStatus   int
		wantScimType string
	}{
		{name: "malformed JSON", body: `{"userName":`, wantStatus: http.StatusBadRequest, wantScimType: scim.ScimTypeInvalidSyntax},
		{name: "well-formed body reaches the store", body: `{"userName":"x@example.com"}`, wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodPut, "/Users/some-id", tt.body, nil)
			require.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			if tt.wantScimType != "" {
				assert.Equal(t, tt.wantScimType, h.decodeError(resp.body).ScimType)
			}
		})
	}
}
