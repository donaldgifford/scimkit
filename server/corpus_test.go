package server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/internal/idpcorpus"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// corpusProfiles maps the fixture profile names to the runtime profiles.
var corpusProfiles = map[string]patch.Profile{
	"strict":  patch.Strict,
	"default": patch.Default,
	"entra":   patch.Entra,
	"okta":    patch.Okta,
}

// TestGoldenIdPTrafficOverHTTP replays every golden PATCH fixture through
// the full HTTP stack under each documented profile: shapes real IdPs
// send succeed under their vendor (and Default) profile, and the same
// bytes are rejected with the RFC-correct scimType under Strict.
func TestGoldenIdPTrafficOverHTTP(t *testing.T) {
	fixtures, err := idpcorpus.All()
	require.NoError(t, err)
	require.NotEmpty(t, fixtures, "corpus is embedded")

	for _, fx := range fixtures {
		for i := range fx.Cases {
			c := fx.Cases[i]
			t.Run(fmt.Sprintf("%s/%s/%s", fx.Provider, fx.Name, c.Profile), func(t *testing.T) {
				profile, ok := corpusProfiles[c.Profile]
				require.Truef(t, ok, "unknown profile %q", c.Profile)
				replayFixture(t, &fx, &c, profile)
			})
		}
	}
}

// replayFixture seeds a store, PATCHes the fixture request over HTTP, and
// asserts the case's documented outcome.
func replayFixture(t *testing.T, fx *idpcorpus.Fixture, c *idpcorpus.Case, profile patch.Profile) {
	t.Helper()
	rt := corpusResourceType(t, fx.ResourceType)
	store := memstore.New(rt, memstore.WithProfile(profile))
	id := seedCorpusResource(t, store, &rt, c.Seed)

	ts := newCorpusServer(t, &rt, store, profile)
	resp := corpusPatch(t, ts, rt.Endpoint+"/"+id, string(fx.Request))

	switch {
	case c.DecodeError != "":
		assertCorpusError(t, resp, c.DecodeError)
	case c.ApplyError != "":
		assertCorpusError(t, resp, c.ApplyError)
	default:
		require.Equalf(t, http.StatusOK, resp.status, "clean apply should be 200: %s", resp.body)
		_, err := scim.DecodeResource(resp.body, &rt)
		require.NoError(t, err, "response is a valid resource")
	}
}

func corpusResourceType(t *testing.T, name string) scim.ResourceType {
	t.Helper()
	switch name {
	case "User":
		return scim.UserResourceType()
	case "Group":
		return scim.GroupResourceType()
	default:
		t.Fatalf("unknown corpus resource type %q", name)
		return scim.ResourceType{}
	}
}

// seedCorpusResource stores the case's seed (or an empty resource) and
// returns its id so the fixture request has a target.
func seedCorpusResource(t *testing.T, store *memstore.Store, rt *scim.ResourceType, seed []byte) string {
	t.Helper()
	res := scim.NewResource(rt)
	if len(seed) > 0 {
		var err error
		res, err = scim.DecodeResource(seed, rt)
		require.NoError(t, err, "decode seed")
	}
	created, err := store.Create(context.Background(), res)
	require.NoError(t, err, "seed store")
	id, _ := created.Get("id")
	return id.(string)
}

func newCorpusServer(t *testing.T, rt *scim.ResourceType, store server.Store, profile patch.Profile) *httptest.Server {
	t.Helper()
	h, err := server.New(server.Config{
		BaseURL:          "https://example.com/scim/v2",
		ResourceTypes:    []server.Registration{{Type: *rt, Store: store}},
		Profile:          profile,
		MaxFilterResults: 100,
		Logger:           discardLogger(),
	})
	require.NoError(t, err)
	return newTestServer(t, h)
}

func corpusPatch(t *testing.T, ts *httptest.Server, path, body string) response {
	t.Helper()
	h := &harness{t: t, server: ts}
	return h.do(http.MethodPatch, path, body, nil)
}

func assertCorpusError(t *testing.T, resp response, scimType string) {
	t.Helper()
	require.Equalf(t, http.StatusBadRequest, resp.status, "expected 400, got body: %s", resp.body)
	var scimErr scim.Error
	require.NoError(t, scimErr.UnmarshalJSON(resp.body))
	assert.Equal(t, scimType, scimErr.ScimType)
}
