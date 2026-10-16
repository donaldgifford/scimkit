package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/client"
	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

func newGroup(t *testing.T, h *harness, displayName string) string {
	t.Helper()
	rt := scim.GroupResourceType()
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("displayName", displayName))
	created, err := h.client.Groups().Create(context.Background(), res)
	require.NoError(t, err)
	id, _ := created.Get("id")
	return id.(string)
}

func TestClientGroupMembership(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	gid := newGroup(t, h, "Engineers")
	groups := h.client.Groups()

	t.Run("EnsureMember adds", func(t *testing.T) {
		res, err := groups.EnsureMember(ctx, gid, "u1")
		require.NoError(t, err)
		members, _ := res.Get("members")
		assert.Len(t, members, 1)
	})

	t.Run("EnsureMember is idempotent", func(t *testing.T) {
		res, err := groups.EnsureMember(ctx, gid, "u1")
		require.NoError(t, err)
		members, _ := res.Get("members")
		assert.Len(t, members, 1, "re-adding an existing member changes nothing")
	})

	t.Run("RemoveMember removes", func(t *testing.T) {
		res, err := groups.RemoveMember(ctx, gid, "u1")
		require.NoError(t, err)
		_, ok := res.Get("members")
		assert.False(t, ok, "last member removed leaves members unassigned")
	})

	t.Run("RemoveMember absent is success", func(t *testing.T) {
		_, err := groups.RemoveMember(ctx, gid, "never-a-member")
		require.NoError(t, err, "removing an absent member is not an error")
	})
}

func TestClientListPagination(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i := range 5 {
		_, err := h.client.Users().Create(ctx, newUser(t, fmt.Sprintf("user%d@example.com", i)))
		require.NoError(t, err)
	}

	// Page size 2 over 5 resources: three pages (2+2+1).
	seen := map[string]bool{}
	for res, err := range h.client.Users().List(ctx, client.Query{
		Count: 2, SortBy: mustPath(t, "userName"), Ascending: true,
	}) {
		require.NoError(t, err)
		name, _ := res.Get("userName")
		seen[name.(string)] = true
	}
	assert.Len(t, seen, 5, "iterator walked every page")
}

func TestClientListMidIterationMutation(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i := range 5 {
		_, err := h.client.Users().Create(ctx, newUser(t, fmt.Sprintf("m%d@example.com", i)))
		require.NoError(t, err)
	}

	// Insert a resource mid-iteration: the walk must terminate and is
	// tolerant of the resulting non-snapshot page boundaries.
	count := 0
	inserted := false
	for _, err := range h.client.Users().List(ctx, client.Query{Count: 2, SortBy: mustPath(t, "userName")}) {
		require.NoError(t, err)
		count++
		if !inserted {
			_, cerr := h.client.Users().Create(ctx, newUser(t, "zzz-late@example.com"))
			require.NoError(t, cerr)
			inserted = true
		}
	}
	assert.GreaterOrEqual(t, count, 5, "saw at least the original resources and terminated")
}

func TestClientCapabilitiesCached(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	caps, err := h.client.Capabilities(ctx)
	require.NoError(t, err)
	assert.True(t, caps.PatchSupported)
	assert.True(t, caps.FilterSupported)
	assert.True(t, caps.ETagSupported)
	assert.Equal(t, 100, caps.MaxFilterResults)
	assert.False(t, caps.BulkSupported)

	before := h.requests.Load()
	_, err = h.client.Capabilities(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, h.requests.Load(), "second call is served from cache, no request")

	h.client.InvalidateCapabilities()
	_, err = h.client.Capabilities(ctx)
	require.NoError(t, err)
	assert.Greater(t, h.requests.Load(), before, "invalidation forces a refetch")
}

func TestClientConflictOnStaleReplace(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	created, err := h.client.Users().Create(ctx, newUser(t, "cas@example.com"))
	require.NoError(t, err)
	id, _ := created.Get("id")

	// Force a stale version token so the server rejects the write.
	created.SetMeta(scim.Meta{Version: `W/"stale"`})
	require.NoError(t, created.Set("displayName", "X"))
	_, err = h.client.Users().Replace(ctx, id.(string), created)

	var conflict *client.ConflictError
	require.ErrorAs(t, err, &conflict, "412 surfaces as a typed conflict")
	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr, "and unwraps to the protocol error")
	assert.Equal(t, 412, scimErr.Status)
}

func mustPath(t *testing.T, s string) *filter.Path {
	t.Helper()
	p, err := filter.ParsePath(s)
	require.NoError(t, err)
	return &p
}

// cappingHandler always returns at most maxPage resources per page,
// regardless of the requested count, but reports the true total — the
// RFC-legal behavior that a naive "short page means done" walk would
// truncate after the first page.
type cappingHandler struct {
	total   int
	maxPage int
}

func (h cappingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	if start < 1 {
		start = 1
	}
	resources := make([]json.RawMessage, 0, h.maxPage)
	for i := start; i < start+h.maxPage && i <= h.total; i++ {
		resources = append(resources, json.RawMessage(
			fmt.Sprintf(`{"schemas":[%q],"id":"u%d","userName":"u%d@example.com"}`, scim.SchemaUser, i, i)))
	}
	w.Header().Set("Content-Type", "application/scim+json")
	writeJSON(w, map[string]any{
		"schemas":      []string{scim.MessageListResponse},
		"totalResults": h.total,
		"startIndex":   start,
		"itemsPerPage": len(resources),
		"Resources":    resources,
	})
}

func TestClientListServerCapsPageSize(t *testing.T) {
	// Server caps pages at 2 even though the client requests 100; the
	// walk must follow TotalResults to completion, not stop at page one.
	ts := httptest.NewServer(cappingHandler{total: 7, maxPage: 2})
	t.Cleanup(ts.Close)
	c, err := client.New(ts.URL, client.WithHTTPClient(ts.Client()))
	require.NoError(t, err)

	count := 0
	for _, err := range c.Users().List(context.Background(), client.Query{}) {
		require.NoError(t, err)
		count++
	}
	assert.Equal(t, 7, count, "iterator followed every capped page to the reported total")
}
