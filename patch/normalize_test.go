package patch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// widgetType builds a resource type exercising every coercible primitive
// type plus complex single, complex multi, and simple multi shapes, with
// one extension schema for URN-qualified coverage.
func widgetType(t *testing.T) scim.ResourceType {
	t.Helper()
	schema := scim.Schema{
		ID:   "urn:example:2.0:Widget",
		Name: "Widget",
		Attributes: []scim.Attribute{
			{Name: "active", Type: scim.TypeBoolean},
			{Name: "count", Type: scim.TypeInteger},
			{Name: "ratio", Type: scim.TypeDecimal},
			{Name: "title", Type: scim.TypeString},
			{Name: "tags", Type: scim.TypeString, MultiValued: true},
			{Name: "name", Type: scim.TypeComplex, SubAttributes: []scim.Attribute{
				{Name: "givenName", Type: scim.TypeString},
				{Name: "verified", Type: scim.TypeBoolean},
			}},
			{Name: "emails", Type: scim.TypeComplex, MultiValued: true, SubAttributes: []scim.Attribute{
				{Name: "value", Type: scim.TypeString},
				{Name: "type", Type: scim.TypeString},
				{Name: "primary", Type: scim.TypeBoolean},
				{Name: "display", Type: scim.TypeString},
			}},
		},
	}.WithDefaults()
	ext := scim.Schema{
		ID:   "urn:example:2.0:WidgetExt",
		Name: "WidgetExt",
		Attributes: []scim.Attribute{
			{Name: "level", Type: scim.TypeInteger},
		},
	}.WithDefaults()
	return scim.ResourceType{
		Name:       "Widget",
		Endpoint:   "/Widgets",
		Schema:     schema,
		Extensions: []scim.SchemaExtension{{Schema: ext}},
	}
}

func TestCaseInsensitiveOps(t *testing.T) {
	rt := scim.UserResourceType()
	p := Profile{CaseInsensitiveOps: true}
	tests := []struct {
		name     string
		body     string
		want     []Op
		wantType string
	}{
		{
			name: "capitalized Add folds",
			body: patchBody(`[{"op": "Add", "path": "title", "value": "x"}]`),
			want: []Op{{Kind: KindAdd, Path: &filter.Path{Attr: "title"}, Value: "x"}},
		},
		{
			name: "capitalized Replace folds",
			body: patchBody(`[{"op": "Replace", "path": "title", "value": "x"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "title"}, Value: "x"}},
		},
		{
			name: "uppercase REMOVE folds",
			body: patchBody(`[{"op": "REMOVE", "path": "title"}]`),
			want: []Op{{Kind: KindRemove, Path: &filter.Path{Attr: "title"}}},
		},
		{
			name:     "unknown op still rejected",
			body:     patchBody(`[{"op": "Merge", "path": "title", "value": "x"}]`),
			wantType: scim.ScimTypeInvalidSyntax,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.body), &rt, p)
			if tt.wantType != "" {
				requireScimType(t, err, tt.wantType)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExpandPathlessReplace(t *testing.T) {
	rt := scim.UserResourceType()
	p := Profile{ExpandPathlessReplace: true}
	tests := []struct {
		name     string
		body     string
		want     []Op
		wantType string
	}{
		{
			name: "dotted and URN keys expand, plain keys stay pathless",
			body: patchBody(`[{"op": "replace", "value": {
				"displayName": "Babs",
				"name.givenName": "Barbara",
				"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:department": "Tour"
			}}]`),
			want: []Op{
				{Kind: KindReplace, Value: map[string]any{"displayName": "Babs"}},
				{Kind: KindReplace, Path: &filter.Path{Attr: "name", SubAttr: "givenName"}, Value: "Barbara"},
				{Kind: KindReplace, Path: &filter.Path{
					URN:  "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User",
					Attr: "department",
				}, Value: "Tour"},
			},
		},
		{
			name: "all keys flattened drops the pathless op",
			body: patchBody(`[{"op": "replace", "value": {"name.familyName": "Jensen"}}]`),
			want: []Op{
				{Kind: KindReplace, Path: &filter.Path{Attr: "name", SubAttr: "familyName"}, Value: "Jensen"},
			},
		},
		{
			name: "pathed replace untouched",
			body: patchBody(`[{"op": "replace", "path": "name", "value": {"givenName": "B"}}]`),
			want: []Op{
				{Kind: KindReplace, Path: &filter.Path{Attr: "name"}, Value: map[string]any{"givenName": "B"}},
			},
		},
		{
			name: "plain-key pathless replace untouched",
			body: patchBody(`[{"op": "replace", "value": {"displayName": "Babs"}}]`),
			want: []Op{{Kind: KindReplace, Value: map[string]any{"displayName": "Babs"}}},
		},
		{
			name:     "expanded key with bad path grammar is invalidPath",
			body:     patchBody(`[{"op": "replace", "value": {"name..givenName": "x"}}]`),
			wantType: scim.ScimTypeInvalidPath,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.body), &rt, p)
			if tt.wantType != "" {
				requireScimType(t, err, tt.wantType)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExpandPathlessReplaceOff(t *testing.T) {
	rt := scim.UserResourceType()
	body := patchBody(`[{"op": "replace", "value": {"name.givenName": "Barbara"}}]`)
	got, err := Decode([]byte(body), &rt, Profile{})
	require.NoError(t, err)
	assert.Equal(t, []Op{
		{Kind: KindReplace, Value: map[string]any{"name.givenName": "Barbara"}},
	}, got)
}

func TestRemoveValueAsSelector(t *testing.T) {
	rt := scim.GroupResourceType()
	p := Profile{RemoveValueAsSelector: true}
	memberEq := func(id string) *filter.Path {
		return &filter.Path{Attr: "members", ValueFilter: filter.Compare{
			Path:  filter.Path{Attr: subAttrValue},
			Op:    filter.OpEqual,
			Value: id,
		}}
	}
	tests := []struct {
		name     string
		body     string
		want     []Op
		wantType string
	}{
		{
			name: "okta member removal",
			body: patchBody(`[{"op": "remove", "path": "members", "value": [{"value": "2819c223"}]}]`),
			want: []Op{{Kind: KindRemove, Path: memberEq("2819c223")}},
		},
		{
			name: "several selector elements become several ops",
			body: patchBody(`[{"op": "remove", "path": "members", "value": [{"value": "a"}, {"value": "b"}]}]`),
			want: []Op{
				{Kind: KindRemove, Path: memberEq("a")},
				{Kind: KindRemove, Path: memberEq("b")},
			},
		},
		{
			name: "bare scalar element selects by value",
			body: patchBody(`[{"op": "remove", "path": "members", "value": ["2819c223"]}]`),
			want: []Op{{Kind: KindRemove, Path: memberEq("2819c223")}},
		},
		{
			name: "single object value selects by value",
			body: patchBody(`[{"op": "remove", "path": "members", "value": {"value": "2819c223"}}]`),
			want: []Op{{Kind: KindRemove, Path: memberEq("2819c223")}},
		},
		{
			name: "multi-member element builds a sorted conjunction",
			body: patchBody(`[{"op": "remove", "path": "members", "value": [{"value": "a", "type": "User"}]}]`),
			want: []Op{{Kind: KindRemove, Path: &filter.Path{Attr: "members", ValueFilter: filter.AndExpr{
				L: filter.Compare{Path: filter.Path{Attr: "type"}, Op: filter.OpEqual, Value: "User"},
				R: filter.Compare{Path: filter.Path{Attr: subAttrValue}, Op: filter.OpEqual, Value: "a"},
			}}}},
		},
		{
			name: "null members ignored",
			body: patchBody(`[{"op": "remove", "path": "members", "value": [{"$ref": null, "value": "a"}]}]`),
			want: []Op{{Kind: KindRemove, Path: memberEq("a")}},
		},
		{
			name:     "selector on filtered path rejected",
			body:     patchBody(`[{"op": "remove", "path": "members[value eq \"a\"]", "value": [{"value": "b"}]}]`),
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "pathless remove with value rejected",
			body:     patchBody(`[{"op": "remove", "value": [{"value": "a"}]}]`),
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "element with nested object member rejected",
			body:     patchBody(`[{"op": "remove", "path": "members", "value": [{"value": {"nested": true}}]}]`),
			wantType: scim.ScimTypeInvalidValue,
		},
		{
			name:     "element selecting nothing rejected",
			body:     patchBody(`[{"op": "remove", "path": "members", "value": [{"$ref": null}]}]`),
			wantType: scim.ScimTypeInvalidValue,
		},
		{
			name: "plain remove without value untouched",
			body: patchBody(`[{"op": "remove", "path": "members"}]`),
			want: []Op{{Kind: KindRemove, Path: &filter.Path{Attr: "members"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.body), &rt, p)
			if tt.wantType != "" {
				requireScimType(t, err, tt.wantType)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCoerceStringPrimitives(t *testing.T) {
	rt := widgetType(t)
	p := Profile{CoerceStringPrimitives: true}
	tests := []struct {
		name string
		body string
		want []Op
	}{
		{
			name: "entra wrapped string boolean",
			body: patchBody(`[{"op": "replace", "path": "active", "value": [{"$ref": null, "value": "False"}]}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "active"}, Value: false}},
		},
		{
			name: "object-wrapped boolean",
			body: patchBody(`[{"op": "replace", "path": "active", "value": {"value": "True"}}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "active"}, Value: true}},
		},
		{
			name: "bare string boolean",
			body: patchBody(`[{"op": "replace", "path": "active", "value": "False"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "active"}, Value: false}},
		},
		{
			name: "string integer becomes json.Number",
			body: patchBody(`[{"op": "replace", "path": "count", "value": "701984"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "count"}, Value: json.Number("701984")}},
		},
		{
			name: "string decimal becomes json.Number",
			body: patchBody(`[{"op": "replace", "path": "ratio", "value": "2.5"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "ratio"}, Value: json.Number("2.5")}},
		},
		{
			name: "string attribute never coerced",
			body: patchBody(`[{"op": "replace", "path": "title", "value": "True"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "title"}, Value: "True"}},
		},
		{
			name: "unparseable string left for Apply to reject",
			body: patchBody(`[{"op": "replace", "path": "count", "value": "abc"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "count"}, Value: "abc"}},
		},
		{
			name: "sub-attribute inside complex elements coerced",
			body: patchBody(`[{"op": "add", "path": "emails", "value": [{"value": "a@b", "primary": "True"}]}]`),
			want: []Op{{Kind: KindAdd, Path: &filter.Path{Attr: "emails"}, Value: []any{
				map[string]any{"value": "a@b", "primary": true},
			}}},
		},
		{
			name: "valuePath sub-attribute target coerced",
			body: patchBody(`[{"op": "replace", "path": "emails[value eq \"a@b\"].primary", "value": "False"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{
				Attr:    "emails",
				SubAttr: "primary",
				ValueFilter: filter.Compare{
					Path:  filter.Path{Attr: subAttrValue},
					Op:    filter.OpEqual,
					Value: "a@b",
				},
			}, Value: false}},
		},
		{
			name: "complex target keeps value sub-attribute shape",
			body: patchBody(`[{"op": "add", "path": "emails", "value": [{"value": "a@b"}]}]`),
			want: []Op{{Kind: KindAdd, Path: &filter.Path{Attr: "emails"}, Value: []any{
				map[string]any{"value": "a@b"},
			}}},
		},
		{
			name: "multi-valued simple elements unwrapped",
			body: patchBody(`[{"op": "add", "path": "tags", "value": [{"value": "x"}, {"value": "y"}]}]`),
			want: []Op{{Kind: KindAdd, Path: &filter.Path{Attr: "tags"}, Value: []any{"x", "y"}}},
		},
		{
			name: "pathless value coerced per attribute",
			body: patchBody(`[{"op": "replace", "value": {"active": "true", "count": "42", "unknown": "x"}}]`),
			want: []Op{{Kind: KindReplace, Value: map[string]any{
				"active":  true,
				"count":   json.Number("42"),
				"unknown": "x",
			}}},
		},
		{
			name: "pathless extension container coerced",
			body: patchBody(`[{"op": "replace", "value": {"urn:example:2.0:WidgetExt": {"level": "3"}}}]`),
			want: []Op{{Kind: KindReplace, Value: map[string]any{
				"urn:example:2.0:WidgetExt": map[string]any{"level": json.Number("3")},
			}}},
		},
		{
			name: "URN-qualified path coerced",
			body: patchBody(`[{"op": "replace", "path": "urn:example:2.0:WidgetExt:level", "value": "7"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{
				URN:  "urn:example:2.0:WidgetExt",
				Attr: "level",
			}, Value: json.Number("7")}},
		},
		{
			name: "nested single complex coerced",
			body: patchBody(`[{"op": "replace", "path": "name", "value": {"givenName": "B", "verified": "true"}}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "name"}, Value: map[string]any{
				"givenName": "B",
				"verified":  true,
			}}},
		},
		{
			name: "undefined attribute untouched",
			body: patchBody(`[{"op": "replace", "path": "mystery", "value": "True"}]`),
			want: []Op{{Kind: KindReplace, Path: &filter.Path{Attr: "mystery"}, Value: "True"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.body), &rt, p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCoerceStringPrimitivesOff(t *testing.T) {
	rt := widgetType(t)
	body := patchBody(`[{"op": "replace", "path": "active", "value": [{"value": "False"}]}]`)
	got, err := Decode([]byte(body), &rt, Profile{})
	require.NoError(t, err)
	assert.Equal(t, []Op{{
		Kind:  KindReplace,
		Path:  &filter.Path{Attr: "active"},
		Value: []any{map[string]any{"value": "False"}},
	}}, got)
}

func requireScimType(t *testing.T, err error, scimType string) {
	t.Helper()
	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr)
	assert.Equal(t, scimType, scimErr.ScimType)
}
