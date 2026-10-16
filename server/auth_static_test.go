package server_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

func newStaticAuthHarness(t *testing.T, token string) *harness {
	t.Helper()
	h, err := server.New(server.Config{
		BaseURL:          "https://example.com/scim/v2",
		ResourceTypes:    []server.Registration{{Type: scim.UserResourceType(), Store: memstore.New(scim.UserResourceType())}},
		Profile:          patch.Default,
		Auth:             server.StaticBearerToken(token),
		MaxFilterResults: 100,
		Logger:           discardLogger(),
	})
	require.NoError(t, err)
	return &harness{t: t, server: newTestServer(t, h)}
}

func TestStaticBearerToken(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		authHeader string
		wantStatus int
	}{
		{name: "exact token passes", configured: "s3cret", authHeader: "Bearer s3cret", wantStatus: http.StatusOK},
		{name: "prefix of the token is 401", configured: "s3cret", authHeader: "Bearer s3cre", wantStatus: http.StatusUnauthorized},
		{name: "longer token is 401", configured: "s3cret", authHeader: "Bearer s3cret1", wantStatus: http.StatusUnauthorized},
		{name: "missing header is 401", configured: "s3cret", wantStatus: http.StatusUnauthorized},
		{name: "empty configured token admits nobody", configured: "", authHeader: "Bearer ", wantStatus: http.StatusUnauthorized},
		{name: "empty configured token rejects any token", configured: "", authHeader: "Bearer x", wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newStaticAuthHarness(t, tt.configured)
			headers := map[string]string{}
			if tt.authHeader != "" {
				headers["Authorization"] = tt.authHeader
			}
			resp := h.do(http.MethodGet, "/Users", "", headers)
			assert.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			if tt.wantStatus == http.StatusUnauthorized {
				assert.Equal(t, `Bearer realm="scim"`, resp.header.Get("WWW-Authenticate"))
			}
		})
	}

	t.Run("advertises the bearer scheme", func(t *testing.T) {
		assert.Equal(t, "oauthbearertoken", server.StaticBearerToken("x").Scheme().Type)
	})
}
