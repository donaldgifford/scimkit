package server_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

func TestUserLifecycle(t *testing.T) {
	h := newHarness(t, patch.Default)

	// Create.
	id, created := h.createUser("bjensen@example.com")
	assert.NotEmpty(t, id)
	assert.NotEmpty(t, created.header.Get("ETag"), "create sets ETag")
	assert.Equal(t, "https://example.com/scim/v2/Users/"+id, created.header.Get("Location"))

	// Get.
	got := h.do(http.MethodGet, "/Users/"+id, "", nil)
	assert.Equal(t, http.StatusOK, got.status)
	res, err := scim.DecodeResource(got.body, ptr(scim.UserResourceType()))
	require.NoError(t, err)
	name, _ := res.Get("userName")
	assert.Equal(t, "bjensen@example.com", name)

	// Replace (PUT).
	put := h.do(http.MethodPut, "/Users/"+id, `{"userName":"bjensen2@example.com"}`, nil)
	require.Equal(t, http.StatusOK, put.status, "%s", put.body)
	putRes, err := scim.DecodeResource(put.body, ptr(scim.UserResourceType()))
	require.NoError(t, err)
	putName, _ := putRes.Get("userName")
	assert.Equal(t, "bjensen2@example.com", putName)
	putID, _ := putRes.Get("id")
	assert.Equal(t, id, putID, "PUT keeps the id")

	// PATCH.
	patchBody := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],` +
		`"Operations":[{"op":"replace","path":"userName","value":"patched@example.com"}]}`
	patched := h.do(http.MethodPatch, "/Users/"+id, patchBody, nil)
	require.Equal(t, http.StatusOK, patched.status, "%s", patched.body)
	patchedRes, err := scim.DecodeResource(patched.body, ptr(scim.UserResourceType()))
	require.NoError(t, err)
	patchedName, _ := patchedRes.Get("userName")
	assert.Equal(t, "patched@example.com", patchedName)

	// Delete → 204 with no body.
	del := h.do(http.MethodDelete, "/Users/"+id, "", nil)
	assert.Equal(t, http.StatusNoContent, del.status)
	assert.Empty(t, del.body, "DELETE has no body")
	assert.Empty(t, del.header.Get("Content-Type"), "DELETE sets no Content-Type")

	// Get after delete → 404.
	gone := h.do(http.MethodGet, "/Users/"+id, "", nil)
	assert.Equal(t, http.StatusNotFound, gone.status)
}

func TestGroupLifecycleWithMembers(t *testing.T) {
	h := newHarness(t, patch.Okta)
	uID, _ := h.createUser("member@example.com")

	created := h.do(http.MethodPost, "/Groups",
		`{"displayName":"Engineers","members":[{"value":"`+uID+`"}]}`, nil)
	require.Equal(t, http.StatusCreated, created.status, "%s", created.body)
	gRes, err := scim.DecodeResource(created.body, ptr(scim.GroupResourceType()))
	require.NoError(t, err)
	gID, _ := gRes.Get("id")

	// Add a second member via valuePath PATCH.
	patchBody := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],` +
		`"Operations":[{"op":"add","path":"members","value":[{"value":"u2"}]}]}`
	patched := h.do(http.MethodPatch, "/Groups/"+gID.(string), patchBody, nil)
	require.Equal(t, http.StatusOK, patched.status, "%s", patched.body)
	pRes, err := scim.DecodeResource(patched.body, ptr(scim.GroupResourceType()))
	require.NoError(t, err)
	members, _ := pRes.Get("members")
	assert.Len(t, members, 2)
}

func TestPatchNoOpDoesNotBumpVersion(t *testing.T) {
	h := newHarness(t, patch.Okta)
	uID, _ := h.createUser("m@example.com")
	created := h.do(http.MethodPost, "/Groups",
		`{"displayName":"Team","members":[{"value":"`+uID+`"}]}`, nil)
	require.Equal(t, http.StatusCreated, created.status)
	gRes, _ := scim.DecodeResource(created.body, ptr(scim.GroupResourceType()))
	gID, _ := gRes.Get("id")
	firstETag := created.header.Get("ETag")

	// Re-add an existing member: set semantics means no change.
	patchBody := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],` +
		`"Operations":[{"op":"add","path":"members","value":[{"value":"` + uID + `"}]}]}`
	patched := h.do(http.MethodPatch, "/Groups/"+gID.(string), patchBody, nil)
	require.Equal(t, http.StatusOK, patched.status, "%s", patched.body)
	assert.Equal(t, firstETag, patched.header.Get("ETag"), "no-op PATCH keeps the version")
}

func TestListAndSearch(t *testing.T) {
	h := newHarness(t, patch.Default)
	h.createUser("alice@example.com")
	h.createUser("bob@example.com")
	h.createUser("carol@example.com")

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		wantTotal int
		wantItems int
	}{
		{
			name: "list all", method: http.MethodGet, path: "/Users",
			wantTotal: 3, wantItems: 3,
		},
		{
			name: "filtered list", method: http.MethodGet,
			path:      `/Users?filter=` + url("userName eq \"bob@example.com\""),
			wantTotal: 1, wantItems: 1,
		},
		{
			name: "paginated", method: http.MethodGet,
			path:      "/Users?count=2&sortBy=userName",
			wantTotal: 3, wantItems: 2,
		},
		{
			name: "count zero totals only", method: http.MethodGet,
			path:      "/Users?count=0",
			wantTotal: 3, wantItems: 0,
		},
		{
			name: "search by post", method: http.MethodPost, path: "/Users/.search",
			body:      `{"filter":"userName sw \"a\""}`,
			wantTotal: 1, wantItems: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(tt.method, tt.path, tt.body, nil)
			require.Equal(t, http.StatusOK, resp.status, "%s", resp.body)
			var lr scim.ListResponse
			require.NoError(t, lr.UnmarshalJSON(resp.body))
			assert.Equal(t, tt.wantTotal, lr.TotalResults)
			assert.Len(t, lr.Resources, tt.wantItems)
		})
	}
}
