package client_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/client"
	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// harness is a client wired to a real server+memstore over httptest.
type harness struct {
	client   *client.Client
	requests *atomic.Int32
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	handler, err := server.New(server.Config{
		BaseURL: "https://example.com/scim/v2",
		ResourceTypes: []server.Registration{
			{Type: scim.UserResourceType(), Store: memstore.New(scim.UserResourceType())},
			{Type: scim.GroupResourceType(), Store: memstore.New(scim.GroupResourceType())},
		},
		Profile:          patch.Okta,
		MaxFilterResults: 100,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)

	var count atomic.Int32
	counted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		handler.ServeHTTP(w, r)
	})
	ts := httptest.NewServer(counted)
	t.Cleanup(ts.Close)

	c, err := client.New(ts.URL, client.WithHTTPClient(ts.Client()))
	require.NoError(t, err)
	return &harness{client: c, requests: &count}
}

func newUser(t *testing.T, userName string) *scim.Resource {
	t.Helper()
	rt := scim.UserResourceType()
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("userName", userName))
	return res
}

func TestClientUserLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	users := h.client.Users()

	created, err := users.Create(ctx, newUser(t, "bjensen@example.com"))
	require.NoError(t, err)
	id, _ := created.Get("id")
	require.NotEmpty(t, id)

	got, err := users.Get(ctx, id.(string))
	require.NoError(t, err)
	name, _ := got.Get("userName")
	assert.Equal(t, "bjensen@example.com", name)

	require.NoError(t, got.Set("displayName", "Barbara"))
	replaced, err := users.Replace(ctx, id.(string), got)
	require.NoError(t, err)
	dn, _ := replaced.Get("displayName")
	assert.Equal(t, "Barbara", dn)

	patched, err := users.Patch(ctx, id.(string), []patch.Op{{
		Kind: patch.KindReplace, Path: &filter.Path{Attr: "displayName"}, Value: "Barb",
	}})
	require.NoError(t, err)
	pdn, _ := patched.Get("displayName")
	assert.Equal(t, "Barb", pdn)

	require.NoError(t, users.Delete(ctx, id.(string)))
	_, err = users.Get(ctx, id.(string))
	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr)
	assert.Equal(t, 404, scimErr.Status)
}

func TestClientDeactivate(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	created, err := h.client.Users().Create(ctx, newUser(t, "deact@example.com"))
	require.NoError(t, err)
	id, _ := created.Get("id")

	res, err := h.client.Users().Deactivate(ctx, id.(string))
	require.NoError(t, err)
	active, ok := res.Get("active")
	require.True(t, ok)
	assert.Equal(t, false, active)
}

func TestClientLookup(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, err := h.client.Users().Create(ctx, newUser(t, "unique@example.com"))
	require.NoError(t, err)

	t.Run("single match", func(t *testing.T) {
		res, err := h.client.Users().Lookup(ctx, filter.Eq("userName", "unique@example.com"))
		require.NoError(t, err)
		require.NotNil(t, res)
		name, _ := res.Get("userName")
		assert.Equal(t, "unique@example.com", name)
	})

	t.Run("no match is nil not error", func(t *testing.T) {
		res, err := h.client.Users().Lookup(ctx, filter.Eq("userName", "absent@example.com"))
		require.NoError(t, err)
		assert.Nil(t, res)
	})

	t.Run("multiple matches is ambiguous", func(t *testing.T) {
		for _, n := range []string{"dup1@example.com", "dup2@example.com"} {
			_, err := h.client.Users().Create(ctx, newUser(t, n))
			require.NoError(t, err)
		}
		_, err := h.client.Users().Lookup(ctx, filter.Co("userName", "dup"))
		assert.ErrorIs(t, err, client.ErrAmbiguousLookup)
	})
}
