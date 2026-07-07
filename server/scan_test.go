package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

// noFilterStore is a toyStore that reports it can evaluate no filter, so
// the server falls back to scanning with filter.Match. Its List ignores
// the filter entirely — the fallback is responsible for correctness.
type noFilterStore struct {
	*toyStore
}

func (noFilterStore) SupportsFilter(filter.Expr) bool { return false }

func (s noFilterStore) List(ctx context.Context, q server.Query) (server.Page, error) {
	q.Filter = nil // prove the server, not the store, applies the filter
	return s.toyStore.List(ctx, q)
}

func newScanServer(t *testing.T, maxResults int) *harness {
	t.Helper()
	store := noFilterStore{toyStore: newToyStore(scim.UserResourceType())}
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

func TestFilterScanFallback(t *testing.T) {
	h := newScanServer(t, 100)
	h.createUser("alice@example.com")
	h.createUser("bob@example.com")
	h.createUser("carol@example.com")

	// The store cannot filter, so the server scans and matches in memory.
	resp := h.do(http.MethodGet, `/Users?filter=`+url("userName eq \"bob@example.com\""), "", nil)
	require.Equal(t, http.StatusOK, resp.status, "%s", resp.body)
	var lr scim.ListResponse
	require.NoError(t, lr.UnmarshalJSON(resp.body))
	assert.Equal(t, 1, lr.TotalResults, "server scan-and-match found the one match")

	// Sorted scan fallback: order is the server's responsibility here.
	sorted := h.do(http.MethodGet, `/Users?filter=`+url("userName co \"example.com\"")+"&sortBy=userName", "", nil)
	require.Equal(t, http.StatusOK, sorted.status)
	require.NoError(t, lr.UnmarshalJSON(sorted.body))
	assert.Equal(t, 3, lr.TotalResults)
}

func TestFilterScanTooMany(t *testing.T) {
	h := newScanServer(t, 2)
	h.createUser("a@example.com")
	h.createUser("b@example.com")
	h.createUser("c@example.com")

	// Three resources exceed the max-results cap on the scan fallback.
	resp := h.do(http.MethodGet, `/Users?filter=`+url("userName pr"), "", nil)
	require.Equal(t, http.StatusBadRequest, resp.status, "%s", resp.body)
	assert.Equal(t, scim.ScimTypeTooMany, h.decodeError(resp.body).ScimType)
}
