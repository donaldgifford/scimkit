package client_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestWithBearerToken(t *testing.T) {
	var gotAuth atomic.Value
	gotAuth.Store("")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/scim+json")
		_, _ = io.WriteString(w, `{"schemas":["`+scim.SchemaUser+`"],"id":"u1","userName":"a@example.com"}`)
	}))
	t.Cleanup(ts.Close)

	c, err := client.New(ts.URL, client.WithHTTPClient(ts.Client()), client.WithBearerToken("s3cret"))
	require.NoError(t, err)
	_, err = c.Users().Get(context.Background(), "u1")
	require.NoError(t, err)
	assert.Equal(t, "Bearer s3cret", gotAuth.Load())
}

func TestSearchTransport(t *testing.T) {
	rt := scim.UserResourceType()
	handler, err := server.New(server.Config{
		BaseURL:          "https://example.com/scim/v2",
		ResourceTypes:    []server.Registration{{Type: rt, Store: memstore.New(rt)}},
		Profile:          patch.Default,
		MaxFilterResults: 100,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)

	var searchHits atomic.Int32
	counted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/.search") {
			searchHits.Add(1)
		}
		handler.ServeHTTP(w, r)
	})
	ts := httptest.NewServer(counted)
	t.Cleanup(ts.Close)

	seed := scim.NewResource(&rt)
	require.NoError(t, seed.Set("userName", "search@example.com"))

	t.Run("client-wide search transport", func(t *testing.T) {
		c, err := client.New(ts.URL, client.WithHTTPClient(ts.Client()), client.WithSearchTransport(true))
		require.NoError(t, err)
		_, err = c.Users().Create(context.Background(), seed)
		require.NoError(t, err)

		var got *scim.Resource
		for res, err := range c.Users().List(context.Background(), client.Query{
			Filter: filter.Eq("userName", "search@example.com"),
		}) {
			require.NoError(t, err)
			got = res
		}
		require.NotNil(t, got)
		assert.Positive(t, searchHits.Load(), "list used POST /.search")
	})

	t.Run("per-call search override", func(t *testing.T) {
		before := searchHits.Load()
		c, err := client.New(ts.URL, client.WithHTTPClient(ts.Client()))
		require.NoError(t, err)
		for _, err := range c.Users().List(context.Background(), client.Query{
			Filter: filter.Eq("userName", "search@example.com"), Search: true,
		}) {
			require.NoError(t, err)
		}
		assert.Greater(t, searchHits.Load(), before, "Query.Search forced the search transport")
	})
}

func TestWithLoggerAccepted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := client.New("https://example.com/scim/v2", client.WithLogger(logger))
	require.NoError(t, err)
	assert.NotNil(t, c)
}

func TestRemoveMemberToleratesNoTarget(t *testing.T) {
	// A target that wrongly answers noTarget on a non-matching valuePath
	// remove; RemoveMember must treat it as success.
	var patched atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/scim+json")
		switch {
		case r.URL.Path == "/ServiceProviderConfig":
			_, _ = io.WriteString(w, spcJSON(true))
		case r.Method == http.MethodPatch:
			patched.Store(true)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w,
				`{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"400","scimType":"noTarget"}`)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"schemas":["`+scim.SchemaGroup+`"],"id":"g1","displayName":"G"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)

	c, err := client.New(ts.URL, client.WithHTTPClient(ts.Client()))
	require.NoError(t, err)
	res, err := c.Groups().RemoveMember(context.Background(), "g1", "absent")
	require.NoError(t, err, "noTarget on remove is tolerated as success")
	require.NotNil(t, res)
	assert.True(t, patched.Load(), "the remove was attempted")
}

func spcJSON(patchSupported bool) string {
	return fmt.Sprintf(
		`{"schemas":["%s"],"patch":{"supported":%t},"bulk":{"supported":false},`+
			`"filter":{"supported":true,"maxResults":100},"changePassword":{"supported":false},`+
			`"sort":{"supported":true},"etag":{"supported":true}}`,
		scim.SchemaServiceProviderConfig, patchSupported)
}
