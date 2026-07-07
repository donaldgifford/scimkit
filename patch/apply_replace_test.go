package patch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

func TestApplyReplace(t *testing.T) {
	tests := []struct {
		name        string
		op          string
		wantChanged bool
		check       func(t *testing.T, res *scim.Resource)
	}{
		{
			name:        "single-valued replaces",
			op:          `{"op": "replace", "path": "title", "value": "Manager"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("title")
				assert.Equal(t, "Manager", v)
			},
		},
		{
			name:        "identical replace is a no-op",
			op:          `{"op": "replace", "path": "title", "value": "Tour Guide"}`,
			wantChanged: false,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("title")
				assert.Equal(t, "Tour Guide", v)
			},
		},
		{
			name:        "missing attribute becomes add",
			op:          `{"op": "replace", "path": "count", "value": 42}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, ok := res.Get("count")
				require.True(t, ok)
				assert.Equal(t, int64(42), v)
			},
		},
		{
			name:        "complex value partially merges",
			op:          `{"op": "replace", "path": "name", "value": {"givenName": "Babs"}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("name")
				assert.Equal(t, scim.ComplexValue{"givenname": "Babs", "verified": true}, v,
					"unspecified sub-attributes remain per §3.5.2.3")
			},
		},
		{
			name:        "sub-attribute path replaces that sub",
			op:          `{"op": "replace", "path": "name.givenName", "value": "Babs"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("name.givenName")
				assert.Equal(t, "Babs", v)
			},
		},
		{
			name:        "multi-valued replaces the whole array",
			op:          `{"op": "replace", "path": "emails", "value": [{"value": "solo@example.com", "type": "other"}]}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 1)
				assert.Equal(t, "solo@example.com", list[0]["value"])
			},
		},
		{
			name:        "empty array unassigns",
			op:          `{"op": "replace", "path": "emails", "value": []}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("emails")
				assert.False(t, ok)
			},
		},
		{
			name:        "valuePath replaces matching elements wholly",
			op:          `{"op": "replace", "path": "emails[type eq \"work\"]", "value": {"value": "w2@example.com", "type": "work"}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 2)
				assert.Equal(t, "w2@example.com", list[1]["value"])
				assert.Equal(t, "home@example.com", list[0]["value"], "non-matching element untouched")
			},
		},
		{
			name:        "valuePath sub-attribute replace",
			op:          `{"op": "replace", "path": "emails[type eq \"home\"].value", "value": "h2@example.com"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				assert.Equal(t, "h2@example.com", list[0]["value"])
				assert.Equal(t, true, list[0]["primary"], "other subs of the element survive")
			},
		},
		{
			name:        "replacement primary demotes the previous one",
			op:          `{"op": "replace", "path": "emails[type eq \"work\"]", "value": {"value": "w@example.com", "type": "work", "primary": true}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				assert.Equal(t, false, list[0]["primary"], "home email demoted")
				assert.Equal(t, true, list[1]["primary"])
			},
		},
		{
			name:        "pathless replace overwrites whole attributes",
			op:          `{"op": "replace", "value": {"title": "Lead", "tags": ["beta"]}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				title, _ := res.Get("title")
				assert.Equal(t, "Lead", title)
				tags, _ := res.Get("tags")
				assert.Equal(t, []string{"beta"}, tags, "multi replaced wholesale, not appended")
			},
		},
		{
			name:        "pathless replace skips readOnly and unknown keys",
			op:          `{"op": "replace", "value": {"id": "hijack", "mystery": 1, "meta": {}, "active": false}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("id")
				assert.False(t, ok)
				active, _ := res.Get("active")
				assert.Equal(t, false, active)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := applyWidget(t)
			result, err := applyOne(t, res, Strict, `[`+tt.op+`]`)
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, result.Changed)
			tt.check(t, result.Resource)
		})
	}
}

func TestApplyReplaceErrors(t *testing.T) {
	tests := []struct {
		name     string
		op       string
		wantType string
	}{
		{
			name:     "valuePath matching nothing is noTarget under strict",
			op:       `{"op": "replace", "path": "emails[type eq \"missing\"]", "value": {"value": "x@example.com"}}`,
			wantType: scim.ScimTypeNoTarget,
		},
		{
			name:     "valuePath on unassigned attribute is noTarget under strict",
			op:       `{"op": "replace", "path": "singles[note eq \"x\"]", "value": {"note": "y"}}`,
			wantType: scim.ScimTypeNoTarget,
		},
		{
			name:     "readOnly attribute",
			op:       `{"op": "replace", "path": "id", "value": "hijack"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "whole-element replace with scalar value",
			op:       `{"op": "replace", "path": "emails[type eq \"work\"]", "value": "flat"}`,
			wantType: scim.ScimTypeInvalidValue,
		},
		{
			name:     "type mismatch",
			op:       `{"op": "replace", "path": "active", "value": "not a bool"}`,
			wantType: scim.ScimTypeInvalidValue,
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

// TestApplyReplaceImmutable pins the immutable matrix for replace:
// unassigned falls back to add and succeeds, assigned is a mutability
// error.
func TestApplyReplaceImmutable(t *testing.T) {
	res := applyWidget(t)
	result, err := applyOne(t, res, Strict,
		`[{"op": "replace", "path": "origin", "value": "factory"}]`)
	require.NoError(t, err)
	assert.True(t, result.Changed)

	_, err = applyOne(t, result.Resource, Strict,
		`[{"op": "replace", "path": "origin", "value": "other"}]`)
	requireScimType(t, err, scim.ScimTypeMutability)
}
