package patch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

func TestApplyRemove(t *testing.T) {
	tests := []struct {
		name        string
		op          string
		wantChanged bool
		check       func(t *testing.T, res *scim.Resource)
	}{
		{
			name:        "single-valued unassigns",
			op:          `{"op": "remove", "path": "title"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("title")
				assert.False(t, ok)
			},
		},
		{
			name:        "multi-valued unassigns wholly",
			op:          `{"op": "remove", "path": "emails"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("emails")
				assert.False(t, ok)
			},
		},
		{
			name:        "already unassigned is a no-op",
			op:          `{"op": "remove", "path": "count"}`,
			wantChanged: false,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("count")
				assert.False(t, ok)
			},
		},
		{
			name:        "sub-attribute of single complex",
			op:          `{"op": "remove", "path": "name.givenName"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("name.givenName")
				assert.False(t, ok)
				v, ok := res.Get("name.verified")
				require.True(t, ok, "sibling sub-attribute survives")
				assert.Equal(t, true, v)
			},
		},
		{
			name:        "valuePath removes matching elements",
			op:          `{"op": "remove", "path": "emails[type eq \"home\"]"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, ok := res.Get("emails")
				require.True(t, ok)
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 1)
				assert.Equal(t, "work@example.com", list[0]["value"])
			},
		},
		{
			name:        "removing the last element unassigns the attribute",
			op:          `{"op": "remove", "path": "singles[note eq \"only\"]"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("singles")
				assert.False(t, ok, "empty survivor list must canonicalize to unassigned")
			},
		},
		{
			name:        "valuePath matching nothing is a no-op",
			op:          `{"op": "remove", "path": "emails[type eq \"missing\"]"}`,
			wantChanged: false,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				assert.Len(t, v.(scim.MultiComplexValue), 2)
			},
		},
		{
			name:        "valuePath with sub-attribute unassigns per element",
			op:          `{"op": "remove", "path": "emails[type eq \"home\"].primary"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 2)
				_, hasPrimary := list[0]["primary"]
				assert.False(t, hasPrimary)
				assert.Equal(t, "home@example.com", list[0]["value"], "element itself survives")
			},
		},
		{
			name:        "element emptied by sub removal is dropped",
			op:          `{"op": "remove", "path": "singles[note eq \"only\"].note"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("singles")
				assert.False(t, ok, "one-sub element empties, list empties, attribute unassigns")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := removeWidget(t)
			result, err := applyOne(t, res, Strict, `[`+tt.op+`]`)
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, result.Changed)
			tt.check(t, result.Resource)
		})
	}
}

// removeWidget extends applyWidget with a multi-complex attribute whose
// elements have a single sub-attribute, for the emptied-element case.
func removeWidget(t *testing.T) *scim.Resource {
	t.Helper()
	rt := applyType(t)
	rt.Schema.Attributes = append(rt.Schema.Attributes, scim.Attribute{
		Name: "singles", Type: scim.TypeComplex, MultiValued: true,
		SubAttributes: []scim.Attribute{{Name: "note", Type: scim.TypeString}},
	})
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("title", "Tour Guide"))
	require.NoError(t, res.Set("kind", "widget"))
	require.NoError(t, res.Set("name", map[string]any{"givenName": "Barbara", "verified": true}))
	require.NoError(t, res.Set("emails", []any{
		map[string]any{"value": "home@example.com", "type": "home", "primary": true},
		map[string]any{"value": "work@example.com", "type": "work"},
	}))
	require.NoError(t, res.Set("tags", []any{"alpha"}))
	require.NoError(t, res.Set("singles", []any{map[string]any{"note": "only"}}))
	return res
}

func TestApplyRemoveErrors(t *testing.T) {
	tests := []struct {
		name     string
		op       string
		wantType string
	}{
		{
			name:     "no path is noTarget",
			op:       `{"op": "remove"}`,
			wantType: scim.ScimTypeNoTarget,
		},
		{
			name:     "required attribute",
			op:       `{"op": "remove", "path": "kind"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "readOnly attribute",
			op:       `{"op": "remove", "path": "id"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "undefined attribute",
			op:       `{"op": "remove", "path": "mystery"}`,
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "sub-attribute on multi-valued without filter",
			op:       `{"op": "remove", "path": "emails.value"}`,
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "valuePath on multi-valued simple attribute",
			op:       `{"op": "remove", "path": "tags[value eq \"alpha\"]"}`,
			wantType: scim.ScimTypeInvalidPath,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := removeWidget(t)
			_, err := applyOne(t, res, Strict, `[`+tt.op+`]`)
			requireScimType(t, err, tt.wantType)
		})
	}
}
