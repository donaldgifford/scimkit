package patch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

// This file mirrors RFC 7644 §3.5.2's structure: each test replays the
// section's own examples verbatim under Strict and asserts the outcomes
// the spec prescribes.

func conformanceGroup(t *testing.T) *scim.Resource {
	t.Helper()
	rt := scim.GroupResourceType()
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("displayName", "Tour Guides"))
	require.NoError(t, res.Set("members", []any{
		map[string]any{
			"value": "2819c223-7f76-453a-919d-413861904646",
			"$ref":  "https://example.com/v2/Users/2819c223-7f76-453a-919d-413861904646",
		},
	}))
	return res
}

func conformanceUser(t *testing.T) *scim.Resource {
	t.Helper()
	rt := scim.UserResourceType()
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("userName", "bjensen@example.com"))
	require.NoError(t, res.Set("emails", []any{
		map[string]any{"value": "bjensen@example.com", "type": "work"},
		map[string]any{"value": "babs@jensen.org", "type": "home"},
	}))
	require.NoError(t, res.Set("addresses", []any{
		map[string]any{
			"type":          "work",
			"streetAddress": "100 Universal City Plaza",
			"locality":      "Hollywood",
		},
	}))
	return res
}

// TestSection3521Add replays the §3.5.2.1 examples: adding a group
// member and adding attributes without a path.
func TestSection3521Add(t *testing.T) {
	t.Run("add member to group", func(t *testing.T) {
		res := conformanceGroup(t)
		result, err := applyOne(t, res, Strict, `[
			{"op": "add", "path": "members", "value": [{
				"$ref": "https://example.com/v2/Users/babs",
				"value": "babs-id"
			}]}
		]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		v, _ := result.Resource.Get("members")
		assert.Len(t, v.(scim.MultiComplexValue), 2)
	})

	t.Run("add attributes without path", func(t *testing.T) {
		res := conformanceUser(t)
		result, err := applyOne(t, res, Strict, `[
			{"op": "add", "value": {
				"emails": [{"value": "babs@jensen.org", "type": "home"}],
				"nickName": "Babs"
			}}
		]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		nick, _ := result.Resource.Get("nickName")
		assert.Equal(t, "Babs", nick)
		v, _ := result.Resource.Get("emails")
		assert.Len(t, v.(scim.MultiComplexValue), 2, "duplicate email ignored, nickName still changed")
	})
}

// TestSection3522Remove replays the §3.5.2.2 examples: removing a
// single member, all members, a filtered email, and the remove-then-add
// membership swap.
func TestSection3522Remove(t *testing.T) {
	t.Run("remove single member", func(t *testing.T) {
		res := conformanceGroup(t)
		result, err := applyOne(t, res, Strict, `[
			{"op": "remove", "path": "members[value eq \"2819c223-7f76-453a-919d-413861904646\"]"}
		]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		_, assigned := result.Resource.Get("members")
		assert.False(t, assigned, "last member removed leaves members unassigned")
	})

	t.Run("remove all members", func(t *testing.T) {
		res := conformanceGroup(t)
		result, err := applyOne(t, res, Strict, `[{"op": "remove", "path": "members"}]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		_, assigned := result.Resource.Get("members")
		assert.False(t, assigned)
	})

	t.Run("remove email by conjunction filter", func(t *testing.T) {
		res := conformanceUser(t)
		result, err := applyOne(t, res, Strict, `[
			{"op": "remove", "path": "emails[type eq \"work\" and value ew \"example.com\"]"}
		]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		v, _ := result.Resource.Get("emails")
		list := v.(scim.MultiComplexValue)
		require.Len(t, list, 1)
		assert.Equal(t, "babs@jensen.org", list[0]["value"])
	})

	t.Run("remove and add members in one PatchOp", func(t *testing.T) {
		res := conformanceGroup(t)
		result, err := applyOne(t, res, Strict, `[
			{"op": "remove", "path": "members"},
			{"op": "add", "path": "members", "value": [{"value": "new-member"}]}
		]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		v, _ := result.Resource.Get("members")
		list := v.(scim.MultiComplexValue)
		require.Len(t, list, 1)
		assert.Equal(t, "new-member", list[0]["value"])
	})
}

// TestSection3523Replace replays the §3.5.2.3 examples: pathless
// attribute replacement, a valuePath sub-attribute replace, and a
// whole-element replace through a conjunction filter.
func TestSection3523Replace(t *testing.T) {
	t.Run("replace attributes without path", func(t *testing.T) {
		res := conformanceUser(t)
		result, err := applyOne(t, res, Strict, `[
			{"op": "replace", "value": {
				"emails": [
					{"value": "bjensen@example.com", "type": "work", "primary": true},
					{"value": "babs@jensen.org", "type": "home"}
				],
				"nickName": "Babs"
			}}
		]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		v, _ := result.Resource.Get("emails")
		list := v.(scim.MultiComplexValue)
		require.Len(t, list, 2)
		assert.Equal(t, true, list[0]["primary"])
	})

	t.Run("replace streetAddress through valuePath", func(t *testing.T) {
		res := conformanceUser(t)
		result, err := applyOne(t, res, Strict, `[
			{"op": "replace", "path": "addresses[type eq \"work\"].streetAddress", "value": "911 Universal City Plaza"}
		]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		v, _ := result.Resource.Get("addresses")
		list := v.(scim.MultiComplexValue)
		assert.Equal(t, "911 Universal City Plaza", list[0]["streetaddress"])
		assert.Equal(t, "Hollywood", list[0]["locality"], "sibling subs survive a sub replace")
	})

	t.Run("replace matched element wholly", func(t *testing.T) {
		res := conformanceUser(t)
		result, err := applyOne(t, res, Strict, `[
			{"op": "replace", "path": "emails[type eq \"work\" and value ew \"example.com\"]",
			 "value": {"value": "bjensen@strict.example.com", "type": "work"}}
		]`)
		require.NoError(t, err)
		assert.True(t, result.Changed)
		v, _ := result.Resource.Get("emails")
		list := v.(scim.MultiComplexValue)
		require.Len(t, list, 2)
		assert.Equal(t, "bjensen@strict.example.com", list[0]["value"])
	})
}

// TestSection352ErrorTaxonomy pins the Table 9 scimType for each PATCH
// failure mode the section defines.
func TestSection352ErrorTaxonomy(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantType string
	}{
		{
			name:     "malformed path is invalidPath",
			body:     `[{"op": "remove", "path": "emails[type"}]`,
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "remove without path is noTarget",
			body:     `[{"op": "remove"}]`,
			wantType: scim.ScimTypeNoTarget,
		},
		{
			name:     "replace with unmatched filter is noTarget",
			body:     `[{"op": "replace", "path": "emails[type eq \"missing\"]", "value": {"value": "x"}}]`,
			wantType: scim.ScimTypeNoTarget,
		},
		{
			name:     "missing value is invalidValue",
			body:     `[{"op": "add", "path": "nickName"}]`,
			wantType: scim.ScimTypeInvalidValue,
		},
		{
			name:     "unknown op is invalidSyntax",
			body:     `[{"op": "move", "path": "nickName", "value": "x"}]`,
			wantType: scim.ScimTypeInvalidSyntax,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := conformanceUser(t)
			_, err := applyOne(t, res, Strict, tt.body)
			requireScimType(t, err, tt.wantType)
		})
	}
}
