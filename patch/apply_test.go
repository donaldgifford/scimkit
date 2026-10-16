package patch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

// TestApplySequentialOps pins §3.5.2 sequential semantics: each op
// observes the effects of the ops before it within the same PatchOp.
func TestApplySequentialOps(t *testing.T) {
	res := applyWidget(t)
	result, err := applyOne(t, res, Strict, `[
		{"op": "add", "path": "emails", "value": [{"value": "o@example.com", "type": "other"}]},
		{"op": "replace", "path": "emails[type eq \"other\"].display", "value": "Other"},
		{"op": "remove", "path": "emails[type eq \"home\"]"}
	]`)
	require.NoError(t, err)
	assert.True(t, result.Changed)
	v, _ := result.Resource.Get("emails")
	list := v.(scim.MultiComplexValue)
	require.Len(t, list, 2)
	assert.Equal(t, "work@example.com", list[0]["value"])
	assert.Equal(t, "Other", list[1]["display"], "op 2 saw the element op 1 appended")
}

// TestApplyChangedAggregation pins Changed across several ops: all
// no-ops stay false, one real change flips it.
func TestApplyChangedAggregation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "all no-ops",
			body: `[
				{"op": "add", "path": "title", "value": "Tour Guide"},
				{"op": "remove", "path": "count"},
				{"op": "add", "path": "tags", "value": ["alpha"]}
			]`,
			want: false,
		},
		{
			name: "one change among no-ops",
			body: `[
				{"op": "add", "path": "title", "value": "Tour Guide"},
				{"op": "add", "path": "tags", "value": ["beta"]}
			]`,
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := applyWidget(t)
			result, err := applyOne(t, res, Strict, tt.body)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result.Changed)
		})
	}
}

// TestApplySchemasMaintenance pins implicit schemas upkeep for
// URN-qualified paths in both directions: writing the first extension
// attribute adds the URN, removing the last one drops it. The Resource
// derives schemas from extension containers, so Apply gets this by
// mutating only through Set.
func TestApplySchemasMaintenance(t *testing.T) {
	const ext = "urn:example:2.0:WidgetExt"
	res := applyWidget(t)
	assert.NotContains(t, res.Schemas(), ext)

	added, err := applyOne(t, res, Strict,
		`[{"op": "add", "path": "`+ext+`:level", "value": 3}]`)
	require.NoError(t, err)
	assert.Contains(t, added.Resource.Schemas(), ext, "first extension write adds the URN")

	removed, err := applyOne(t, added.Resource, Strict,
		`[{"op": "remove", "path": "`+ext+`:level"}]`)
	require.NoError(t, err)
	assert.NotContains(t, removed.Resource.Schemas(), ext, "emptying the extension drops the URN")
}

// TestApplyGroupMemberNoOp is the phase success criterion backing the
// server's version-bump logic: re-adding an existing group member under
// the Okta profile must report Changed=false, even when the IdP sends
// only the member id and the stored element carries more.
func TestApplyGroupMemberNoOp(t *testing.T) {
	rt := scim.GroupResourceType()
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("displayName", "Tour Guides"))
	require.NoError(t, res.Set("members", []any{
		map[string]any{
			"value": "2819c223",
			"$ref":  "https://example.com/v2/Users/2819c223",
			"type":  "User",
		},
	}))

	result, err := applyOne(t, res, Okta,
		`[{"op": "add", "path": "members", "value": [{"value": "2819c223"}]}]`)
	require.NoError(t, err)
	assert.False(t, result.Changed, "re-adding an existing member must not report a change")
	v, _ := result.Resource.Get("members")
	list := v.(scim.MultiComplexValue)
	require.Len(t, list, 1)
	assert.Equal(t, "User", list[0]["type"], "stored member enrichment survives")
}

// TestApplyOktaMemberRemoval runs the Okta remove-by-value shape end to
// end: decode rewrites it to valuePath removes, apply drops the member.
func TestApplyOktaMemberRemoval(t *testing.T) {
	rt := scim.GroupResourceType()
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("members", []any{
		map[string]any{"value": "a"},
		map[string]any{"value": "b"},
	}))

	result, err := applyOne(t, res, Okta,
		`[{"op": "remove", "path": "members", "value": [{"value": "a"}]}]`)
	require.NoError(t, err)
	assert.True(t, result.Changed)
	v, _ := result.Resource.Get("members")
	list := v.(scim.MultiComplexValue)
	require.Len(t, list, 1)
	assert.Equal(t, "b", list[0]["value"])
}

// TestApplyImmutableElementSubs pins immutable enforcement inside
// matched valuePath elements against the real Group schema, whose
// members.value/$ref/type are the only immutable attributes RFC 7643
// ships: a member's identity must not be silently retargeted through
// any write shape, while re-sending the identical value stays legal.
func TestApplyImmutableElementSubs(t *testing.T) {
	seedGroup := func(t *testing.T) *scim.Resource {
		t.Helper()
		rt := scim.GroupResourceType()
		res := scim.NewResource(&rt)
		require.NoError(t, res.Set("displayName", "Tour Guides"))
		require.NoError(t, res.Set("members", []any{
			map[string]any{"value": "u1", "$ref": "https://example.com/v2/Users/u1"},
		}))
		return res
	}
	tests := []struct {
		name     string
		op       string
		wantType string
	}{
		{
			name:     "sub-attribute replace retargeting identity",
			op:       `{"op": "replace", "path": "members[value eq \"u1\"].value", "value": "u2"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "sub-attribute add retargeting identity",
			op:       `{"op": "add", "path": "members[value eq \"u1\"].value", "value": "u2"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "element merge retargeting identity",
			op:       `{"op": "add", "path": "members[value eq \"u1\"]", "value": {"value": "u2"}}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "whole-element replace retargeting identity",
			op:       `{"op": "replace", "path": "members[value eq \"u1\"]", "value": {"value": "u2"}}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "whole-element replace dropping assigned $ref",
			op:       `{"op": "replace", "path": "members[value eq \"u1\"]", "value": {"value": "u1"}}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "removing the identity sub-attribute",
			op:       `{"op": "remove", "path": "members[value eq \"u1\"].value"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name: "identical value re-sent is a legal no-op",
			op:   `{"op": "add", "path": "members[value eq \"u1\"]", "value": {"value": "u1"}}`,
		},
		{
			name: "setting an unassigned immutable sub is legal",
			op:   `{"op": "add", "path": "members[value eq \"u1\"].type", "value": "User"}`,
		},
		{
			name: "removing the whole element is legal",
			op:   `{"op": "remove", "path": "members[value eq \"u1\"]"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := seedGroup(t)
			_, err := applyOne(t, res, Strict, `[`+tt.op+`]`)
			if tt.wantType == "" {
				require.NoError(t, err)
				return
			}
			requireScimType(t, err, tt.wantType)
		})
	}
}

// TestApplyRequiredMultiValuedRemove pins the required guard for
// valuePath removes: emptying a required multi-valued attribute through
// a filter is a mutability error, removing part of it is legal.
func TestApplyRequiredMultiValuedRemove(t *testing.T) {
	rt := widgetType(t)
	rt.Schema.Attributes = append(rt.Schema.Attributes, scim.Attribute{
		Name: "owners", Type: scim.TypeComplex, MultiValued: true, Required: true,
		SubAttributes: []scim.Attribute{{Name: "value", Type: scim.TypeString}},
	})
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("owners", []any{
		map[string]any{"value": "a"},
		map[string]any{"value": "b"},
	}))

	result, err := applyOne(t, res, Strict,
		`[{"op": "remove", "path": "owners[value eq \"a\"]"}]`)
	require.NoError(t, err, "partial removal of a required multi-valued attribute is legal")
	assert.True(t, result.Changed)

	_, err = applyOne(t, res, Strict,
		`[{"op": "remove", "path": "owners[value pr]"}]`)
	requireScimType(t, err, scim.ScimTypeMutability)
}

// TestApplyImmutableMatrix consolidates the immutable rules across all
// three kinds: writable while unassigned, locked once assigned.
func TestApplyImmutableMatrix(t *testing.T) {
	tests := []struct {
		name     string
		assigned bool
		op       string
		wantType string
	}{
		{
			name: "add while unassigned succeeds",
			op:   `{"op": "add", "path": "origin", "value": "factory"}`,
		},
		{
			name: "replace while unassigned succeeds",
			op:   `{"op": "replace", "path": "origin", "value": "factory"}`,
		},
		{
			name:     "add while assigned fails",
			assigned: true,
			op:       `{"op": "add", "path": "origin", "value": "other"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "replace while assigned fails",
			assigned: true,
			op:       `{"op": "replace", "path": "origin", "value": "other"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "remove while assigned fails",
			assigned: true,
			op:       `{"op": "remove", "path": "origin"}`,
			wantType: scim.ScimTypeMutability,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := applyWidget(t)
			if tt.assigned {
				require.NoError(t, res.Set("origin", "factory"))
			}
			_, err := applyOne(t, res, Strict, `[`+tt.op+`]`)
			if tt.wantType == "" {
				require.NoError(t, err)
				return
			}
			requireScimType(t, err, tt.wantType)
		})
	}
}

// immutableType extends applyType with immutable multi-valued and
// complex attributes so match-or-error can be pinned per shape.
func immutableType(t *testing.T) scim.ResourceType {
	t.Helper()
	rt := applyType(t)
	rt.Schema.Attributes = append(rt.Schema.Attributes,
		scim.Attribute{Name: "labels", Type: scim.TypeString, MultiValued: true, Mutability: scim.MutabilityImmutable},
		scim.Attribute{Name: "source", Type: scim.TypeComplex, Mutability: scim.MutabilityImmutable, SubAttributes: []scim.Attribute{
			{Name: "system", Type: scim.TypeString},
			{Name: "id", Type: scim.TypeString},
		}},
	)
	return rt
}

func seedSet(path string, v any) func(t *testing.T, res *scim.Resource) {
	return func(t *testing.T, res *scim.Resource) {
		t.Helper()
		require.NoError(t, res.Set(path, v))
	}
}

// TestApplyImmutableMatchOrError pins RFC 7643 §2.2 for assigned
// immutable attributes: re-sending the current value is a Changed=false
// no-op, a different value is a mutability error, and an unassigned
// attribute accepts anything — for pathed and pathless add and replace
// across scalar, multi-valued, and complex shapes.
func TestApplyImmutableMatchOrError(t *testing.T) {
	source := map[string]any{"system": "hr", "id": "7"}
	tests := []struct {
		name     string
		seed     func(t *testing.T, res *scim.Resource)
		op       string
		wantType string
	}{
		{
			name: "pathed add same scalar",
			seed: seedSet("origin", "factory"),
			op:   `{"op": "add", "path": "origin", "value": "factory"}`,
		},
		{
			name: "pathed replace same scalar",
			seed: seedSet("origin", "factory"),
			op:   `{"op": "replace", "path": "origin", "value": "factory"}`,
		},
		{
			name:     "pathed add different scalar",
			seed:     seedSet("origin", "factory"),
			op:       `{"op": "add", "path": "origin", "value": "other"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name: "pathless add same scalar",
			seed: seedSet("origin", "factory"),
			op:   `{"op": "add", "value": {"origin": "factory"}}`,
		},
		{
			name: "pathless replace same scalar",
			seed: seedSet("origin", "factory"),
			op:   `{"op": "replace", "value": {"origin": "factory"}}`,
		},
		{
			name:     "pathless add different scalar",
			seed:     seedSet("origin", "factory"),
			op:       `{"op": "add", "value": {"origin": "other"}}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "pathless replace different scalar",
			seed:     seedSet("origin", "factory"),
			op:       `{"op": "replace", "value": {"origin": "other"}}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name: "pathless add unassigned scalar",
			op:   `{"op": "add", "value": {"origin": "factory"}}`,
		},
		{
			name: "add already-present element to immutable multi",
			seed: seedSet("labels", []any{"a", "b"}),
			op:   `{"op": "add", "path": "labels", "value": ["a"]}`,
		},
		{
			name:     "add new element to immutable multi",
			seed:     seedSet("labels", []any{"a", "b"}),
			op:       `{"op": "add", "path": "labels", "value": ["c"]}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name: "replace immutable multi with identical list",
			seed: seedSet("labels", []any{"a", "b"}),
			op:   `{"op": "replace", "path": "labels", "value": ["a", "b"]}`,
		},
		{
			name:     "replace immutable multi with subset",
			seed:     seedSet("labels", []any{"a", "b"}),
			op:       `{"op": "replace", "path": "labels", "value": ["a"]}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "replace immutable multi with empty list",
			seed:     seedSet("labels", []any{"a"}),
			op:       `{"op": "replace", "path": "labels", "value": []}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name: "add empty list to immutable multi is a no-op",
			seed: seedSet("labels", []any{"a"}),
			op:   `{"op": "add", "path": "labels", "value": []}`,
		},
		{
			name: "merge matching subset into immutable complex",
			seed: seedSet("source", source),
			op:   `{"op": "add", "path": "source", "value": {"system": "hr"}}`,
		},
		{
			name:     "merge differing sub into immutable complex",
			seed:     seedSet("source", source),
			op:       `{"op": "replace", "path": "source", "value": {"id": "8"}}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name:     "sub-attribute path under assigned immutable complex",
			seed:     seedSet("source", source),
			op:       `{"op": "add", "path": "source.id", "value": "7"}`,
			wantType: scim.ScimTypeMutability,
		},
		{
			name: "sub-attribute path under unassigned immutable complex",
			op:   `{"op": "add", "path": "source.id", "value": "7"}`,
		},
		{
			name:     "type mismatch on immutable is invalidValue",
			seed:     seedSet("origin", "factory"),
			op:       `{"op": "replace", "path": "origin", "value": 7}`,
			wantType: scim.ScimTypeInvalidValue,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := immutableType(t)
			res := scim.NewResource(&rt)
			require.NoError(t, res.Set("kind", "widget"))
			if tt.seed != nil {
				tt.seed(t, res)
			}
			before := res.Clone()
			result, err := applyOne(t, res, Strict, `[`+tt.op+`]`)
			if tt.wantType != "" {
				requireScimType(t, err, tt.wantType)
				return
			}
			require.NoError(t, err)
			if tt.seed == nil {
				assert.True(t, result.Changed)
				return
			}
			assert.False(t, result.Changed, "re-sending the current value is a no-op")
			assert.Equal(t, before, result.Resource)
		})
	}
}

// TestApplyInputNotMutated pins Apply's atomicity contract against the
// shared-leaf working copy: however the op touches the resource, the
// caller's input is unchanged afterwards.
func TestApplyInputNotMutated(t *testing.T) {
	tests := []struct {
		name string
		op   string
	}{
		{name: "replace scalar", op: `{"op": "replace", "path": "title", "value": "Changed"}`},
		{name: "set sub-attribute", op: `{"op": "replace", "path": "name.givenName", "value": "Changed"}`},
		{name: "remove sub-attribute", op: `{"op": "remove", "path": "name.verified"}`},
		{name: "remove element", op: `{"op": "remove", "path": "emails[type eq \"work\"]"}`},
		{name: "remove element sub", op: `{"op": "remove", "path": "emails[type eq \"work\"].value"}`},
		{name: "write element sub", op: `{"op": "replace", "path": "emails[type eq \"work\"].display", "value": "W"}`},
		{name: "merge into element", op: `{"op": "add", "path": "emails[type eq \"work\"]", "value": {"display": "W"}}`},
		{name: "replace element", op: `{"op": "replace", "path": "emails[type eq \"work\"]", "value": {"value": "x@y.z"}}`},
		{name: "new primary demotes", op: `{"op": "add", "path": "emails", "value": [{"value": "p@q.r", "primary": true}]}`},
		{name: "append scalar", op: `{"op": "add", "path": "tags", "value": ["beta"]}`},
		{name: "extension write", op: `{"op": "add", "path": "urn:example:2.0:WidgetExt:level", "value": 3}`},
		{name: "pathless merge", op: `{"op": "add", "value": {"name": {"givenName": "Z"}, "tags": ["z"]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := applyWidget(t)
			require.NoError(t, res.Set("urn:example:2.0:WidgetExt:level", 1))
			snapshot := res.Clone()
			result, err := applyOne(t, res, Strict, `[`+tt.op+`]`)
			require.NoError(t, err)
			assert.True(t, result.Changed)
			assert.Equal(t, snapshot, res, "input resource must not change")
			assert.NotEqual(t, snapshot, result.Resource)
		})
	}
}
