package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

func newAuthHarness(t *testing.T, exemptSPC bool) *httptest.Server {
	t.Helper()
	verify := func(_ context.Context, token string) error {
		if token != "s3cret" {
			return errors.New("bad token")
		}
		return nil
	}
	h, err := server.New(server.Config{
		BaseURL:                     "https://example.com/scim/v2",
		ResourceTypes:               []server.Registration{{Type: scim.UserResourceType(), Store: memstore.New(scim.UserResourceType())}},
		Profile:                     patch.Default,
		Auth:                        server.BearerToken(verify),
		ExemptServiceProviderConfig: exemptSPC,
		MaxFilterResults:            100,
		Logger:                      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func TestBearerAuth(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		authHeader string
		exemptSPC  bool
		wantStatus int
	}{
		{name: "no header is 401", path: "/Users", wantStatus: http.StatusUnauthorized},
		{name: "wrong token is 401", path: "/Users", authHeader: "Bearer nope", wantStatus: http.StatusUnauthorized},
		{name: "valid token passes", path: "/Users", authHeader: "Bearer s3cret", wantStatus: http.StatusOK},
		{
			name: "case-insensitive scheme", path: "/Users",
			authHeader: "bearer s3cret", wantStatus: http.StatusOK,
		},
		{
			name: "SPC requires auth by default", path: "/ServiceProviderConfig",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "SPC exemption serves without auth", path: "/ServiceProviderConfig",
			exemptSPC: true, wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newAuthHarness(t, tt.exemptSPC)
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+tt.path, http.NoBody)
			require.NoError(t, err)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			resp, err := ts.Client().Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			assert.Equal(t, tt.wantStatus, resp.StatusCode)
			if tt.wantStatus == http.StatusUnauthorized {
				assert.NotEmpty(t, resp.Header.Get("WWW-Authenticate"), "401 carries a challenge")
			}
		})
	}
}
