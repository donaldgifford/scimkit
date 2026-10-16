package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// TestErrorConformance checks RFC 7644 Table 8 status mapping and Table 9
// error-body scimType keywords across the endpoints.
func TestErrorConformance(t *testing.T) {
	h := newHarness(t, patch.Default)
	h.createUser("taken@example.com")
	existingID, _ := h.createUser("patchable@example.com")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantScim   string
	}{
		{
			name: "missing resource is 404", method: http.MethodGet,
			path: "/Users/nonexistent", wantStatus: http.StatusNotFound,
		},
		{
			name: "duplicate userName is 409 uniqueness", method: http.MethodPost,
			path: "/Users", body: `{"userName":"taken@example.com"}`,
			wantStatus: http.StatusConflict, wantScim: scim.ScimTypeUniqueness,
		},
		{
			name: "bad filter is 400 invalidFilter", method: http.MethodGet,
			path:       `/Users?filter=` + url("userName zz \"x\""),
			wantStatus: http.StatusBadRequest, wantScim: scim.ScimTypeInvalidFilter,
		},
		{
			name: "missing required attribute is 400 invalidValue", method: http.MethodPost,
			path: "/Users", body: `{"displayName":"no username"}`,
			wantStatus: http.StatusBadRequest, wantScim: scim.ScimTypeInvalidValue,
		},
		{
			name: "malformed body is 400 invalidSyntax", method: http.MethodPost,
			path: "/Users", body: `{"userName":`,
			wantStatus: http.StatusBadRequest, wantScim: scim.ScimTypeInvalidSyntax,
		},
		{
			name: "bad patch path is 400 invalidPath", method: http.MethodPatch,
			path: "/Users/" + existingID,
			body: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],` +
				`"Operations":[{"op":"remove","path":"mystery"}]}`,
			wantStatus: http.StatusBadRequest, wantScim: scim.ScimTypeInvalidPath,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(tt.method, tt.path, tt.body, nil)
			require.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			assertErrorBody(t, h, resp.body, tt.wantStatus, tt.wantScim)
		})
	}
}

// assertErrorBody verifies the wire error carries the Error schema URN,
// the string-form status, and the expected scimType.
func assertErrorBody(t *testing.T, h *harness, body []byte, status int, scimType string) {
	t.Helper()
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw), "error body is JSON: %s", body)
	var schemas []string
	require.NoError(t, json.Unmarshal(raw["schemas"], &schemas))
	assert.Equal(t, []string{scim.MessageError}, schemas)

	scimErr := h.decodeError(body)
	assert.Equal(t, status, scimErr.Status, "status field mirrors HTTP status")
	if scimType != "" {
		assert.Equal(t, scimType, scimErr.ScimType)
	}
}

func TestUnsupportedEndpoints(t *testing.T) {
	h := newHarness(t, patch.Default)
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "bulk is 501", method: http.MethodPost, path: "/Bulk", body: `{}`, wantStatus: http.StatusNotImplemented},
		{name: "me is 501", method: http.MethodGet, path: "/Me", wantStatus: http.StatusNotImplemented},
		{name: "unknown path is 404", method: http.MethodGet, path: "/Widgets", wantStatus: http.StatusNotFound},
		{
			name: "wrong media type is 415", method: http.MethodPost, path: "/Users",
			body: `{"userName":"x@example.com"}`, wantStatus: http.StatusUnsupportedMediaType,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var headers map[string]string
			if tt.name == "wrong media type is 415" {
				headers = map[string]string{"Content-Type": "text/plain"}
			}
			resp := h.doRaw(tt.method, tt.path, tt.body, headers)
			assert.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
		})
	}
}
