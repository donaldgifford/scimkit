package patch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/internal/idpcorpus"
	"github.com/donaldgifford/scimkit/scim"
)

var corpusProfiles = map[string]Profile{
	"strict":  Strict,
	"default": Default,
	"entra":   Entra,
	"okta":    Okta,
}

// TestGoldenIdPCorpus replays every documented IdP request shape: each
// fixture must decode and apply under its vendor profile, and every
// non-compliant shape must be rejected under Strict with the
// RFC-correct scimType.
func TestGoldenIdPCorpus(t *testing.T) {
	fixtures, err := idpcorpus.All()
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	for _, f := range fixtures {
		for _, c := range f.Cases {
			t.Run(f.Provider+"/"+f.Name+"/"+c.Profile, func(t *testing.T) {
				runCorpusCase(t, &f, &c)
			})
		}
	}
}

func runCorpusCase(t *testing.T, f *idpcorpus.Fixture, c *idpcorpus.Case) {
	t.Helper()
	profile, ok := corpusProfiles[c.Profile]
	require.True(t, ok, "unknown profile %q", c.Profile)
	rt := corpusResourceType(t, f.ResourceType)

	ops, err := Decode(f.Request, &rt, profile)
	if c.DecodeError != "" {
		requireScimType(t, err, c.DecodeError)
		return
	}
	require.NoError(t, err, "decode under %s", c.Profile)

	res := corpusSeed(t, &rt, c.Seed)
	result, err := Apply(res, ops, profile)
	if c.ApplyError != "" {
		requireScimType(t, err, c.ApplyError)
		return
	}
	require.NoError(t, err, "apply under %s", c.Profile)
	if c.WantChanged != nil {
		assert.Equal(t, *c.WantChanged, result.Changed)
	}
	for _, w := range c.Want {
		assertCorpusPath(t, result.Resource, w)
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
		t.Fatalf("unknown resource type %q", name)
		return scim.ResourceType{}
	}
}

func corpusSeed(t *testing.T, rt *scim.ResourceType, seed json.RawMessage) *scim.Resource {
	t.Helper()
	if len(seed) == 0 {
		return scim.NewResource(rt)
	}
	res, err := scim.DecodeResource(seed, rt)
	require.NoError(t, err)
	return res
}

// assertCorpusPath compares a stored value against the fixture's JSON
// expectation. Fixture scalars decode as string, bool, or float64;
// stored integers compare through float64, which is exact for corpus-
// sized values.
func assertCorpusPath(t *testing.T, res *scim.Resource, w idpcorpus.PathAssertion) {
	t.Helper()
	got, assigned := res.Get(w.Path)
	if w.Unassigned {
		assert.False(t, assigned, "path %s should be unassigned", w.Path)
		return
	}
	require.True(t, assigned, "path %s should be assigned", w.Path)
	if w.Len != nil {
		assert.Len(t, got, *w.Len, "path %s", w.Path)
		return
	}
	if n, ok := got.(int64); ok {
		assert.Equal(t, w.Value, float64(n), "path %s", w.Path)
		return
	}
	assert.Equal(t, w.Value, got, "path %s", w.Path)
}
