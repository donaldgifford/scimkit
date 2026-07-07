package server_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// TestConcurrentGroupPatchStorm fires many simultaneous member-add
// PATCHes at one group — Entra's real behavior — and asserts every add
// survives. Run under -race, the striped locks plus the store's own lock
// must serialize the read-modify-write so no update is lost.
func TestConcurrentGroupPatchStorm(t *testing.T) {
	h := newHarness(t, patch.Okta)
	created := h.do(http.MethodPost, "/Groups", `{"displayName":"Storm"}`, nil)
	require.Equal(t, http.StatusCreated, created.status)
	gRes, err := scim.DecodeResource(created.body, ptr(scim.GroupResourceType()))
	require.NoError(t, err)
	gID, _ := gRes.Get("id")

	const workers = 24
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(n int) {
			defer wg.Done()
			body := fmt.Sprintf(
				`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],`+
					`"Operations":[{"op":"add","path":"members","value":[{"value":"user-%d"}]}]}`, n)
			resp := h.do(http.MethodPatch, "/Groups/"+gID.(string), body, nil)
			assert.Equal(t, http.StatusOK, resp.status)
		}(i)
	}
	wg.Wait()

	final := h.do(http.MethodGet, "/Groups/"+gID.(string), "", nil)
	require.Equal(t, http.StatusOK, final.status)
	finalRes, err := scim.DecodeResource(final.body, ptr(scim.GroupResourceType()))
	require.NoError(t, err)
	members, ok := finalRes.Get("members")
	require.True(t, ok, "group has members")
	assert.Len(t, members, workers, "every concurrent member add survived")
}
