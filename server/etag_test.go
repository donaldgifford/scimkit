package server_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
)

func TestConditionalRequests(t *testing.T) {
	h := newHarness(t, patch.Default)
	id, created := h.createUser("etag@example.com")
	etag := created.header.Get("ETag")
	require.NotEmpty(t, etag)

	t.Run("If-None-Match returns 304", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/Users/"+id, "", map[string]string{"If-None-Match": etag})
		assert.Equal(t, http.StatusNotModified, resp.status)
		assert.Empty(t, resp.body)
		assert.Equal(t, etag, resp.header.Get("ETag"))
	})

	t.Run("If-None-Match with stale tag returns 200", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/Users/"+id, "", map[string]string{"If-None-Match": `W/"stale"`})
		assert.Equal(t, http.StatusOK, resp.status)
	})

	t.Run("If-Match mismatch on PUT is 412", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/Users/"+id, `{"userName":"new@example.com"}`,
			map[string]string{"If-Match": `W/"wrong"`})
		assert.Equal(t, http.StatusPreconditionFailed, resp.status)
	})

	t.Run("If-Match hit on PUT succeeds", func(t *testing.T) {
		cur := h.do(http.MethodGet, "/Users/"+id, "", nil)
		resp := h.do(http.MethodPut, "/Users/"+id, `{"userName":"match@example.com"}`,
			map[string]string{"If-Match": cur.header.Get("ETag")})
		assert.Equal(t, http.StatusOK, resp.status, "%s", resp.body)
	})
}

func TestProjectionOnResponses(t *testing.T) {
	h := newHarness(t, patch.Default)
	id, _ := h.createUser("proj@example.com")

	t.Run("attributes limits emitted fields", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/Users/"+id+"?attributes=userName", "", nil)
		require.Equal(t, http.StatusOK, resp.status)
		// id and schemas are the minimum set and always survive; userName
		// is requested; nothing else default-returned should appear beyond
		// them. meta is returned=default so it is dropped.
		assert.Contains(t, string(resp.body), "userName")
		assert.NotContains(t, string(resp.body), `"meta"`)
	})

	t.Run("attributes and excludedAttributes are mutually exclusive", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/Users/"+id+"?attributes=userName&excludedAttributes=meta", "", nil)
		assert.Equal(t, http.StatusBadRequest, resp.status)
		assert.Equal(t, "invalidValue", h.decodeError(resp.body).ScimType)
	})
}
