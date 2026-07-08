package client_test

import (
	"context"
	"encoding/json"
	"io"
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
)

// stubTarget is a hand-rolled SCIM target for exercising client
// behavior a real memstore-backed server would not produce: a
// patch-disabled service and a 501-on-PATCH service.
type stubTarget struct {
	patchSupported bool
	patch501       bool
	putCalls       atomic.Int32
	patchCalls     atomic.Int32
}

func (s *stubTarget) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/scim+json")
	switch {
	case r.URL.Path == "/ServiceProviderConfig":
		s.writeSPC(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/Users/"):
		s.writeUser(w, `disp-old`)
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/Users/"):
		s.putCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/Users/"):
		s.patchCalls.Add(1)
		if s.patch501 {
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"501"}`))
			return
		}
		s.writeUser(w, `disp-patched`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *stubTarget) writeSPC(w http.ResponseWriter) {
	writeJSON(w, map[string]any{
		"schemas":        []string{scim.SchemaServiceProviderConfig},
		"patch":          map[string]any{"supported": s.patchSupported},
		"bulk":           map[string]any{"supported": false},
		"filter":         map[string]any{"supported": true, "maxResults": 100},
		"changePassword": map[string]any{"supported": false},
		"sort":           map[string]any{"supported": true},
		"etag":           map[string]any{"supported": true},
	})
}

func (*stubTarget) writeUser(w http.ResponseWriter, displayName string) {
	w.Header().Set("ETag", `W/"1"`)
	writeJSON(w, map[string]any{
		"schemas":     []string{scim.SchemaUser},
		"id":          "u1",
		"userName":    "u1@example.com",
		"displayName": displayName,
		"meta":        map[string]any{"resourceType": "User", "version": `W/"1"`},
	})
}

// writeJSON encodes a stub response, reporting an encode failure as a 500
// so the ignored-error linter stays satisfied.
func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newStubClient(t *testing.T, stub *stubTarget, opts ...client.Option) *client.Client {
	t.Helper()
	ts := httptest.NewServer(stub)
	t.Cleanup(ts.Close)
	all := append([]client.Option{client.WithHTTPClient(ts.Client())}, opts...)
	c, err := client.New(ts.URL, all...)
	require.NoError(t, err)
	return c
}

func patchOp() []patch.Op {
	return []patch.Op{{Kind: patch.KindReplace, Path: &filter.Path{Attr: "displayName"}, Value: "disp-new"}}
}

func TestPatchFallbackWhenUnsupported(t *testing.T) {
	stub := &stubTarget{patchSupported: false}
	c := newStubClient(t, stub)

	res, err := c.Users().Patch(context.Background(), "u1", patchOp())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Zero(t, stub.patchCalls.Load(), "advertised no PATCH, so none is attempted")
	assert.Equal(t, int32(1), stub.putCalls.Load(), "fell back to a PUT")
	dn, _ := res.Get("displayName")
	assert.Equal(t, "disp-new", dn, "the op was applied locally before the PUT")
}

func TestPatchFallbackOn501(t *testing.T) {
	stub := &stubTarget{patchSupported: true, patch501: true}
	c := newStubClient(t, stub)

	res, err := c.Users().Patch(context.Background(), "u1", patchOp())
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, int32(1), stub.patchCalls.Load(), "tried PATCH first")
	assert.Equal(t, int32(1), stub.putCalls.Load(), "fell back to PUT after 501")
}

func TestPatchFallbackDisabledSurfacesError(t *testing.T) {
	stub := &stubTarget{patchSupported: true, patch501: true}
	c := newStubClient(t, stub, client.WithPatchFallback(false))

	_, err := c.Users().Patch(context.Background(), "u1", patchOp())
	require.Error(t, err, "with fallback off, a 501 is surfaced")
	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr)
	assert.Equal(t, http.StatusNotImplemented, scimErr.Status)
	assert.Zero(t, stub.putCalls.Load(), "no PUT fallback attempted")
}
