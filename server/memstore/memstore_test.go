package memstore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

func newUser(t *testing.T, rt *scim.ResourceType, userName string) *scim.Resource {
	t.Helper()
	res := scim.NewResource(rt)
	require.NoError(t, res.Set("userName", userName))
	return res
}

func seedUsers(t *testing.T, names ...string) (*Store, []string) {
	t.Helper()
	s := New(scim.UserResourceType())
	ids := make([]string, 0, len(names))
	for _, name := range names {
		created, err := s.Create(context.Background(), newUser(t, &s.rt, name))
		require.NoError(t, err)
		id, _ := created.Get("id")
		ids = append(ids, id.(string))
	}
	return s, ids
}

func TestCreate(t *testing.T) {
	s, ids := seedUsers(t, "bjensen@example.com")
	got, err := s.Get(context.Background(), ids[0])
	require.NoError(t, err)
	assert.Equal(t, `W/"1"`, got.Meta().Version, "first version stamped")

	t.Run("duplicate userName is uniqueness", func(t *testing.T) {
		_, err := s.Create(context.Background(), newUser(t, &s.rt, "BJENSEN@example.com"))
		requireScimType(t, err, scim.ScimTypeUniqueness)
	})

	t.Run("returned resource is a clone", func(t *testing.T) {
		created, err := s.Create(context.Background(), newUser(t, &s.rt, "other@example.com"))
		require.NoError(t, err)
		require.NoError(t, created.Set("userName", "mutated@example.com"))
		id, _ := created.Get("id")
		fresh, err := s.Get(context.Background(), id.(string))
		require.NoError(t, err)
		v, _ := fresh.Get("userName")
		assert.Equal(t, "other@example.com", v, "store state unaffected by caller mutation")
	})
}

func TestGetMissing(t *testing.T) {
	s, _ := seedUsers(t)
	_, err := s.Get(context.Background(), "missing")
	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr)
	assert.Equal(t, 404, scimErr.Status)
}

func TestReplace(t *testing.T) {
	t.Run("bumps version and reindexes", func(t *testing.T) {
		s, ids := seedUsers(t, "old@example.com")
		next := newUser(t, &s.rt, "new@example.com")
		replaced, err := s.Replace(context.Background(), ids[0], next)
		require.NoError(t, err)
		assert.Equal(t, `W/"2"`, replaced.Meta().Version)

		page, err := s.List(context.Background(), server.Query{
			Filter:     mustFilter(t, `userName eq "old@example.com"`),
			StartIndex: 1, Count: 10,
		})
		require.NoError(t, err)
		assert.Zero(t, page.TotalResults, "old index term removed")
	})

	t.Run("stale version token is precondition failure", func(t *testing.T) {
		s, ids := seedUsers(t, "a@example.com")
		next := newUser(t, &s.rt, "a2@example.com")
		next.SetMeta(scim.Meta{Version: `W/"999"`})
		_, err := s.Replace(context.Background(), ids[0], next)
		var scimErr *scim.Error
		require.ErrorAs(t, err, &scimErr)
		assert.Equal(t, 412, scimErr.Status)
	})

	t.Run("missing id is not found", func(t *testing.T) {
		s, _ := seedUsers(t)
		_, err := s.Replace(context.Background(), "missing", newUser(t, &s.rt, "x@example.com"))
		var scimErr *scim.Error
		require.ErrorAs(t, err, &scimErr)
		assert.Equal(t, 404, scimErr.Status)
	})

	t.Run("uniqueness enforced against other resources", func(t *testing.T) {
		s, ids := seedUsers(t, "a@example.com", "b@example.com")
		_, err := s.Replace(context.Background(), ids[1], newUser(t, &s.rt, "A@example.com"))
		requireScimType(t, err, scim.ScimTypeUniqueness)
	})
}

func TestDelete(t *testing.T) {
	s, ids := seedUsers(t, "a@example.com")
	require.NoError(t, s.Delete(context.Background(), ids[0]))
	err := s.Delete(context.Background(), ids[0])
	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr)
	assert.Equal(t, 404, scimErr.Status)

	page, err := s.List(context.Background(), server.Query{
		Filter:     mustFilter(t, `userName eq "a@example.com"`),
		StartIndex: 1, Count: 10,
	})
	require.NoError(t, err)
	assert.Zero(t, page.TotalResults, "index entry removed with the resource")
}

func TestList(t *testing.T) {
	s, _ := seedUsers(t, "carol@example.com", "alice@example.com", "bob@example.com")
	tests := []struct {
		name      string
		q         server.Query
		wantTotal int
		wantNames []string
	}{
		{
			name:      "indexed eq filter is case-insensitive",
			q:         server.Query{Filter: mustFilter(t, `userName eq "ALICE@example.com"`), StartIndex: 1, Count: 10},
			wantTotal: 1,
			wantNames: []string{"alice@example.com"},
		},
		{
			name:      "non-indexed filter scans with Match",
			q:         server.Query{Filter: mustFilter(t, `userName sw "b"`), StartIndex: 1, Count: 10},
			wantTotal: 1,
			wantNames: []string{"bob@example.com"},
		},
		{
			name:      "unfiltered sorted ascending",
			q:         server.Query{SortBy: mustPath(t, "userName"), Ascending: true, StartIndex: 1, Count: 10},
			wantTotal: 3,
			wantNames: []string{"alice@example.com", "bob@example.com", "carol@example.com"},
		},
		{
			name:      "sorted descending with pagination",
			q:         server.Query{SortBy: mustPath(t, "userName"), Ascending: false, StartIndex: 2, Count: 1},
			wantTotal: 3,
			wantNames: []string{"bob@example.com"},
		},
		{
			name:      "count zero returns totals only",
			q:         server.Query{StartIndex: 1, Count: 0},
			wantTotal: 3,
			wantNames: []string{},
		},
		{
			name:      "startIndex past the end is empty",
			q:         server.Query{StartIndex: 10, Count: 5},
			wantTotal: 3,
			wantNames: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := s.List(context.Background(), tt.q)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, page.TotalResults)
			names := make([]string, 0, len(page.Resources))
			for _, res := range page.Resources {
				v, _ := res.Get("userName")
				names = append(names, v.(string))
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

func TestListInvalidFilter(t *testing.T) {
	s, _ := seedUsers(t, "a@example.com")
	_, err := s.List(context.Background(), server.Query{
		Filter:     mustFilter(t, `active gt true`),
		StartIndex: 1, Count: 10,
	})
	requireScimType(t, err, scim.ScimTypeInvalidFilter)
}

func TestPatch(t *testing.T) {
	rt := scim.GroupResourceType()
	seed := func(t *testing.T) (*Store, string) {
		t.Helper()
		s := New(rt, WithProfile(patch.Okta))
		res := scim.NewResource(&s.rt)
		require.NoError(t, res.Set("displayName", "Tour Guides"))
		require.NoError(t, res.Set("members", []any{map[string]any{"value": "u1"}}))
		created, err := s.Create(context.Background(), res)
		require.NoError(t, err)
		id, _ := created.Get("id")
		return s, id.(string)
	}
	decodeOps := func(t *testing.T, body string) []patch.Op {
		t.Helper()
		ops, err := patch.Decode(
			[]byte(`{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": `+body+`}`),
			&rt, patch.Okta)
		require.NoError(t, err)
		return ops
	}

	t.Run("member add bumps version", func(t *testing.T) {
		s, id := seed(t)
		got, err := s.Patch(context.Background(), id,
			decodeOps(t, `[{"op": "add", "path": "members", "value": [{"value": "u2"}]}]`))
		require.NoError(t, err)
		assert.Equal(t, `W/"2"`, got.Meta().Version)
		v, _ := got.Get("members")
		assert.Len(t, v.(scim.MultiComplexValue), 2)
	})

	t.Run("idempotent member re-add keeps version", func(t *testing.T) {
		s, id := seed(t)
		got, err := s.Patch(context.Background(), id,
			decodeOps(t, `[{"op": "add", "path": "members", "value": [{"value": "u1"}]}]`))
		require.NoError(t, err)
		assert.Equal(t, `W/"1"`, got.Meta().Version, "no-op patch must not bump the version")
		assert.True(t, got.Meta().LastModified.IsZero(), "no-op patch must not touch lastModified")
	})

	t.Run("member remove by okta selector", func(t *testing.T) {
		s, id := seed(t)
		got, err := s.Patch(context.Background(), id,
			decodeOps(t, `[{"op": "remove", "path": "members", "value": [{"value": "u1"}]}]`))
		require.NoError(t, err)
		_, assigned := got.Get("members")
		assert.False(t, assigned)
	})

	t.Run("missing resource is not found", func(t *testing.T) {
		s, _ := seed(t)
		_, err := s.Patch(context.Background(), "missing",
			decodeOps(t, `[{"op": "remove", "path": "members"}]`))
		var scimErr *scim.Error
		require.ErrorAs(t, err, &scimErr)
		assert.Equal(t, 404, scimErr.Status)
	})

	t.Run("apply errors propagate", func(t *testing.T) {
		s, id := seed(t)
		_, err := s.Patch(context.Background(), id,
			decodeOps(t, `[{"op": "remove", "path": "mystery"}]`))
		requireScimType(t, err, scim.ScimTypeInvalidPath)
	})
}

func mustFilter(t *testing.T, s string) filter.Expr {
	t.Helper()
	e, err := filter.Parse(s)
	require.NoError(t, err)
	return e
}

func mustPath(t *testing.T, s string) *filter.Path {
	t.Helper()
	p, err := filter.ParsePath(s)
	require.NoError(t, err)
	return &p
}

func requireScimType(t *testing.T, err error, scimType string) {
	t.Helper()
	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr)
	assert.Equal(t, scimType, scimErr.ScimType)
}
