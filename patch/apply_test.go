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
