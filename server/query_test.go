package server_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

func TestListParamValidation(t *testing.T) {
	h := newHarness(t, patch.Default)
	h.createUser("a@example.com")
	h.createUser("b@example.com")

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantItems  int
	}{
		{name: "bad startIndex is 400", path: "/Users?startIndex=abc", wantStatus: http.StatusBadRequest},
		{name: "bad count is 400", path: "/Users?count=xyz", wantStatus: http.StatusBadRequest},
		{name: "bad sortBy path is 400", path: "/Users?sortBy=..bad", wantStatus: http.StatusBadRequest},
		{name: "unknown sortOrder is 400", path: "/Users?sortOrder=sideways", wantStatus: http.StatusBadRequest},
		{
			name: "negative count returns totals only", path: "/Users?count=-5",
			wantStatus: http.StatusOK, wantItems: 0,
		},
		{
			name: "startIndex below one is clamped", path: "/Users?startIndex=0&sortBy=userName",
			wantStatus: http.StatusOK, wantItems: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(http.MethodGet, tt.path, "", nil)
			require.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			if tt.wantStatus == http.StatusOK {
				var lr scim.ListResponse
				require.NoError(t, lr.UnmarshalJSON(resp.body))
				assert.Len(t, lr.Resources, tt.wantItems)
			}
		})
	}
}

func TestSearchBody(t *testing.T) {
	h := newHarness(t, patch.Default)
	h.createUser("alice@example.com")
	h.createUser("bob@example.com")
	h.createUser("carol@example.com")

	t.Run("full search request maps every field", func(t *testing.T) {
		body := `{"filter":"userName co \"example.com\"","sortBy":"userName",` +
			`"sortOrder":"descending","startIndex":1,"count":2,"attributes":["userName"]}`
		resp := h.do(http.MethodPost, "/Users/.search", body, nil)
		require.Equal(t, http.StatusOK, resp.status, "%s", resp.body)
		var lr scim.ListResponse
		require.NoError(t, lr.UnmarshalJSON(resp.body))
		assert.Equal(t, 3, lr.TotalResults)
		require.Len(t, lr.Resources, 2)
		// Descending: carol first.
		first, err := scim.DecodeResource(lr.Resources[0], ptr(scim.UserResourceType()))
		require.NoError(t, err)
		name, _ := first.Get("userName")
		assert.Equal(t, "carol@example.com", name)
	})

	t.Run("malformed search body is 400 invalidSyntax", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/Users/.search", `{"filter":`, nil)
		require.Equal(t, http.StatusBadRequest, resp.status)
		assert.Equal(t, scim.ScimTypeInvalidSyntax, h.decodeError(resp.body).ScimType)
	})

	t.Run("bad filter in search body is 400 invalidFilter", func(t *testing.T) {
		resp := h.do(http.MethodPost, "/Users/.search", `{"filter":"userName zz \"x\""}`, nil)
		require.Equal(t, http.StatusBadRequest, resp.status)
		assert.Equal(t, scim.ScimTypeInvalidFilter, h.decodeError(resp.body).ScimType)
	})
}

// panicStore panics on Get so the recover middleware can be exercised.
type panicStore struct{ *toyStore }

func (panicStore) Get(context.Context, string) (*scim.Resource, error) {
	panic("boom")
}

func TestPanicRecovery(t *testing.T) {
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

	resp := h.do(http.MethodGet, "/Users/anything", "", nil)
	assert.Equal(t, http.StatusInternalServerError, resp.status, "panic becomes a 500")
	assert.Equal(t, 500, h.decodeError(resp.body).Status, "500 body is a SCIM error")
}
