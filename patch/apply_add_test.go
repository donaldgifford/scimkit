package patch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// applyType is widgetType plus an immutable attribute and a members-like
// multi-complex attribute for the apply decision tables.
func applyType(t *testing.T) scim.ResourceType {
	t.Helper()
	rt := widgetType(t)
	rt.Schema.Attributes = append(rt.Schema.Attributes,
		scim.Attribute{Name: "origin", Type: scim.TypeString, Mutability: scim.MutabilityImmutable},
		scim.Attribute{Name: "kind", Type: scim.TypeString, Required: true},
	)
	return rt
}

// applyWidget builds the shared starting resource for apply tests.
func applyWidget(t *testing.T) *scim.Resource {
	t.Helper()
	rt := applyType(t)
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("title", "Tour Guide"))
	require.NoError(t, res.Set("kind", "widget"))
	require.NoError(t, res.Set("active", true))
	require.NoError(t, res.Set("name", map[string]any{"givenName": "Barbara", "verified": true}))
	require.NoError(t, res.Set("emails", []any{
		map[string]any{"value": "home@example.com", "type": "home", "primary": true},
		map[string]any{"value": "work@example.com", "type": "work"},
	}))
	require.NoError(t, res.Set("tags", []any{"alpha"}))
	return res
}

// applyOne decodes a single-op PatchOp body and applies it under the
// profile, so tests exercise the same path production traffic takes.
func applyOne(t *testing.T, res *scim.Resource, p Profile, body string) (Result, error) {
	t.Helper()
	ops, err := Decode([]byte(patchBody(body)), res.ResourceType(), p)
	require.NoError(t, err)
	return Apply(res, ops, p)
}

func TestApplyAdd(t *testing.T) {
	tests := []struct {
		name        string
		op          string
		wantChanged bool
		check       func(t *testing.T, res *scim.Resource)
	}{
		{
			name:        "single-valued assigns",
			op:          `{"op": "add", "path": "ratio", "value": 2.5}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, ok := res.Get("ratio")
				require.True(t, ok)
				assert.Equal(t, 2.5, v)
			},
		},
		{
			name:        "single-valued replaces existing",
			op:          `{"op": "add", "path": "title", "value": "Manager"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("title")
				assert.Equal(t, "Manager", v)
			},
		},
		{
			name:        "identical value is a no-op",
			op:          `{"op": "add", "path": "title", "value": "Tour Guide"}`,
			wantChanged: false,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("title")
				assert.Equal(t, "Tour Guide", v)
			},
		},
		{
			name:        "sub-attribute of single complex",
			op:          `{"op": "add", "path": "name.givenName", "value": "Babs"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("name.givenName")
				assert.Equal(t, "Babs", v)
				verified, _ := res.Get("name.verified")
				assert.Equal(t, true, verified, "sibling sub-attributes survive")
			},
		},
		{
			name:        "complex value partially merges",
			op:          `{"op": "add", "path": "name", "value": {"givenName": "Babs"}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("name")
				assert.Equal(t, scim.ComplexValue{"givenname": "Babs", "verified": true}, v)
			},
		},
		{
			name:        "multi-valued appends",
			op:          `{"op": "add", "path": "emails", "value": [{"value": "new@example.com", "type": "other"}]}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 3)
				assert.Equal(t, "new@example.com", list[2]["value"])
			},
		},
		{
			name:        "bare element appends to multi-valued",
			op:          `{"op": "add", "path": "tags", "value": "beta"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("tags")
				assert.Equal(t, []string{"alpha", "beta"}, v)
			},
		},
		{
			name:        "duplicate element by value identity is a no-op",
			op:          `{"op": "add", "path": "emails", "value": [{"value": "home@example.com"}]}`,
			wantChanged: false,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				assert.Len(t, v.(scim.MultiComplexValue), 2)
			},
		},
		{
			name:        "duplicate scalar is a no-op",
			op:          `{"op": "add", "path": "tags", "value": ["alpha"]}`,
			wantChanged: false,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("tags")
				assert.Equal(t, []string{"alpha"}, v)
			},
		},
		{
			name:        "empty array add is a no-op that keeps elements",
			op:          `{"op": "add", "path": "emails", "value": []}`,
			wantChanged: false,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, ok := res.Get("emails")
				require.True(t, ok, "existing elements must survive an empty add")
				assert.Len(t, v.(scim.MultiComplexValue), 2)
			},
		},
		{
			name:        "unassigned multi-valued assigns",
			op:          `{"op": "add", "path": "count", "value": 42}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("count")
				assert.Equal(t, int64(42), v)
			},
		},
		{
			name:        "valuePath merges into matching element",
			op:          `{"op": "add", "path": "emails[type eq \"work\"]", "value": {"display": "Work"}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				assert.Equal(t, "work@example.com", list[1]["value"], "existing subs survive the merge")
				assert.Equal(t, "home@example.com", list[0]["value"], "other elements untouched")
			},
		},
		{
			name:        "valuePath sub-attribute sets per element",
			op:          `{"op": "add", "path": "emails[type eq \"work\"].value", "value": "w2@example.com"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				assert.Equal(t, "w2@example.com", list[1]["value"])
			},
		},
		{
			name:        "new primary demotes the previous one",
			op:          `{"op": "add", "path": "emails[type eq \"work\"].primary", "value": true}`,
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
			name:        "pathless add merges attributes",
			op:          `{"op": "add", "value": {"count": 7, "title": "Lead"}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("count")
				assert.Equal(t, int64(7), v)
				title, _ := res.Get("title")
				assert.Equal(t, "Lead", title)
			},
		},
		{
			name:        "pathless add multi-valued appends",
			op:          `{"op": "add", "value": {"tags": ["beta"]}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("tags")
				assert.Equal(t, []string{"alpha", "beta"}, v)
			},
		},
		{
			name:        "pathless add skips readOnly and unknown keys",
			op:          `{"op": "add", "value": {"id": "hijack", "mystery": 1, "schemas": ["x"], "title": "Lead"}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				_, ok := res.Get("id")
				assert.False(t, ok, "readOnly id silently skipped")
				title, _ := res.Get("title")
				assert.Equal(t, "Lead", title)
			},
		},
		{
			name:        "pathless extension container",
			op:          `{"op": "add", "value": {"urn:example:2.0:WidgetExt": {"level": 3}}}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("urn:example:2.0:WidgetExt:level")
				assert.Equal(t, int64(3), v)
				assert.Contains(t, res.Schemas(), "urn:example:2.0:WidgetExt",
					"extension URN joins schemas via derivation")
			},
		},
		{
			name:        "URN-qualified path",
			op:          `{"op": "add", "path": "urn:example:2.0:WidgetExt:level", "value": 5}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("urn:example:2.0:WidgetExt:level")
				assert.Equal(t, int64(5), v)
			},
		},
		{
			name:        "unassigned immutable is writable",
			op:          `{"op": "add", "path": "origin", "value": "factory"}`,
			wantChanged: true,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("origin")
				assert.Equal(t, "factory", v)
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

func TestApplyAddErrors(t *testing.T) {
	tests := []struct {
		name     string
		seed     func(t *testing.T, res *scim.Resource)
		op       string
		wantType string
	}{
		{
			name:     "undefined attribute",
			op:       `{"op": "add", "path": "mystery", "value": "x"}`,
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "readOnly attribute",
			op:       `{"op": "add", "path": "id", "value": "hijack"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name: "immutable already assigned",
			seed: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				require.NoError(t, res.Set("origin", "factory"))
			},
			op:       `{"op": "add", "path": "origin", "value": "other"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "valuePath matching nothing is noTarget under strict",
			op:       `{"op": "add", "path": "emails[type eq \"missing\"]", "value": {"display": "x"}}`,
			wantType: scim.ScimTypeNoTarget,
		},
		{
			name:     "valuePath on non-multi-valued attribute",
			op:       `{"op": "add", "path": "name[givenName eq \"B\"]", "value": {"verified": false}}`,
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "sub-attribute on multi-valued without filter",
			op:       `{"op": "add", "path": "emails.value", "value": "x"}`,
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "type mismatch",
			op:       `{"op": "add", "path": "count", "value": "not a number"}`,
			wantType: scim.ScimTypeInvalidValue,
		},
		{
			name:     "pathless add with scalar value",
			op:       `{"op": "add", "value": "flat"}`,
			wantType: scim.ScimTypeInvalidValue,
		},
		{
			name:     "schemas is not a patch target",
			op:       `{"op": "add", "path": "schemas", "value": ["urn:x"]}`,
			wantType: scim.ScimTypeMutability,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := applyWidget(t)
			if tt.seed != nil {
				tt.seed(t, res)
			}
			_, err := applyOne(t, res, Strict, `[`+tt.op+`]`)
			requireScimType(t, err, tt.wantType)
		})
	}
}

// TestApplyAtomicity pins §3.5.2: a failing op aborts the whole PatchOp
// and the caller's resource is untouched even by the ops before it.
func TestApplyAtomicity(t *testing.T) {
	res := applyWidget(t)
	ops := []Op{
		{Kind: KindAdd, Path: &filter.Path{Attr: "title"}, Value: "Changed"},
		{Kind: KindAdd, Path: &filter.Path{Attr: "id"}, Value: "hijack"},
	}
	result, err := Apply(res, ops, Strict)
	requireScimType(t, err, scim.ScimTypeMutability)
	assert.Equal(t, Result{}, result, "failed Apply returns the zero Result")
	title, _ := res.Get("title")
	assert.Equal(t, "Tour Guide", title, "input resource untouched by the successful first op")
}

// TestApplyInputNeverMutated pins the working-copy contract on success.
func TestApplyInputNeverMutated(t *testing.T) {
	res := applyWidget(t)
	result, err := applyOne(t, res, Strict,
		`[{"op": "add", "path": "title", "value": "Manager"}]`)
	require.NoError(t, err)
	before, _ := res.Get("title")
	assert.Equal(t, "Tour Guide", before, "input resource keeps its value")
	after, _ := result.Resource.Get("title")
	assert.Equal(t, "Manager", after)
}
