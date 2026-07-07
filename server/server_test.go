package server_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// harness is a running SCIM service provider over httptest, backed by
// in-memory stores for User and Group.
type harness struct {
	t      *testing.T
	server *httptest.Server
}

// newHarness builds an open (unauthenticated) server with the given
// profile, backed by memstore for User and Group.
func newHarness(t *testing.T, profile patch.Profile) *harness {
	t.Helper()
	return newHarnessWith(t, profile, nil, false)
}

// newHarnessWith builds a server with an optional auth layer and the
// ServiceProviderConfig exemption toggle.
func newHarnessWith(t *testing.T, profile patch.Profile, auth server.Auth, exemptSPC bool) *harness {
	t.Helper()
	userStore := memstore.New(scim.UserResourceType(), memstore.WithProfile(profile))
	groupStore := memstore.New(scim.GroupResourceType(), memstore.WithProfile(profile))
	h, err := server.New(server.Config{
		BaseURL: "https://example.com/scim/v2",
		ResourceTypes: []server.Registration{
			{Type: scim.UserResourceType(), Store: userStore},
			{Type: scim.GroupResourceType(), Store: groupStore},
		},
		Profile:                     profile,
		Auth:                        auth,
		ExemptServiceProviderConfig: exemptSPC,
		MaxFilterResults:            100,
		Logger:                      discardLogger(),
	})
	require.NoError(t, err)
	return &harness{t: t, server: newTestServer(t, h)}
}

// discardLogger silences server logs during tests.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer starts an httptest server that is torn down with the test.
func newTestServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

// response is the decoded outcome of one request.
type response struct {
	status int
	header http.Header
	body   []byte
}

// do issues a request with the SCIM media type and returns the response.
func (h *harness) do(method, path, body string, headers map[string]string) response {
	h.t.Helper()
	if body != "" {
		if headers == nil {
			headers = map[string]string{}
		}
		if _, ok := headers["Content-Type"]; !ok {
			headers["Content-Type"] = "application/scim+json"
		}
	}
	return h.doRaw(method, path, body, headers)
}

// doRaw issues a request setting only the given headers — no media type
// is applied automatically, so callers can exercise content negotiation.
func (h *harness) doRaw(method, path, body string, headers map[string]string) response {
	h.t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, rdr)
	require.NoError(h.t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.server.Client().Do(req)
	require.NoError(h.t, err)
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	require.NoError(h.t, err)
	return response{status: resp.StatusCode, header: resp.Header, body: data}
}

// createUser posts a minimal user and returns the created resource id and
// the full response.
func (h *harness) createUser(userName string) (string, response) {
	h.t.Helper()
	resp := h.do(http.MethodPost, "/Users", `{"userName":"`+userName+`"}`, nil)
	require.Equal(h.t, http.StatusCreated, resp.status, "create user: %s", resp.body)
	return h.decodeID(resp.body), resp
}

func (h *harness) decodeID(body []byte) string {
	h.t.Helper()
	res, err := scim.DecodeResource(body, ptr(scim.UserResourceType()))
	require.NoError(h.t, err)
	id, ok := res.Get("id")
	require.True(h.t, ok, "response has id")
	return id.(string)
}

// decodeError parses a SCIM error body.
func (h *harness) decodeError(body []byte) *scim.Error {
	h.t.Helper()
	var scimErr scim.Error
	require.NoError(h.t, scimErr.UnmarshalJSON(body), "decode error body: %s", body)
	return &scimErr
}

func ptr[T any](v T) *T { return &v }

// url percent-encodes a filter expression for use in a query string.
func url(filter string) string { return neturl.QueryEscape(filter) }
