package patch

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

const patchEnvelope = `{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": `

func patchBody(operations string) string {
	return patchEnvelope + operations + `}`
}

func TestDecode(t *testing.T) {
	rt := scim.UserResourceType()
	tests := []struct {
		name string
		body string
		want []Op
	}{
		{
			name: "add with path",
			body: patchBody(`[{"op": "add", "path": "nickName", "value": "Babs"}]`),
			want: []Op{{Kind: KindAdd, Path: &filter.Path{Attr: "nickName"}, Value: "Babs"}},
		},
		{
			name: "pathless add",
			body: patchBody(`[{"op": "add", "value": {"nickName": "Babs"}}]`),
			want: []Op{{Kind: KindAdd, Value: map[string]any{"nickName": "Babs"}}},
		},
		{
			name: "pathless replace",
			body: patchBody(`[{"op": "replace", "value": {"active": false}}]`),
			want: []Op{{Kind: KindReplace, Value: map[string]any{"active": false}}},
		},
		{
			name: "remove with path",
			body: patchBody(`[{"op": "remove", "path": "nickName"}]`),
			want: []Op{{Kind: KindRemove, Path: &filter.Path{Attr: "nickName"}}},
		},
		{
			name: "valuePath parses",
			body: patchBody(`[{"op": "remove", "path": "emails[type eq \"work\"]"}]`),
			want: []Op{{
				Kind: KindRemove,
				Path: &filter.Path{
					Attr: "emails",
					ValueFilter: filter.Compare{
						Path:  filter.Path{Attr: "type"},
						Op:    filter.OpEqual,
						Value: "work",
					},
				},
			}},
		},
		{
			name: "number value stays json.Number",
			body: patchBody(`[{"op": "replace", "path": "count", "value": 9007199254740993}]`),
			want: []Op{{
				Kind:  KindReplace,
				Path:  &filter.Path{Attr: "count"},
				Value: json.Number("9007199254740993"),
			}},
		},
		{
			name: "nested numbers stay json.Number",
			body: patchBody(`[{"op": "add", "value": {"ratio": 2.5}}]`),
			want: []Op{{Kind: KindAdd, Value: map[string]any{"ratio": json.Number("2.5")}}},
		},
		{
			name: "multiple operations in order",
			body: patchBody(`[{"op": "add", "path": "title", "value": "x"}, {"op": "remove", "path": "title"}]`),
			want: []Op{
				{Kind: KindAdd, Path: &filter.Path{Attr: "title"}, Value: "x"},
				{Kind: KindRemove, Path: &filter.Path{Attr: "title"}},
			},
		},
		{
			name: "envelope keys are case-insensitive",
			body: `{"SCHEMAS": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "operations": [{"OP": "add", "PATH": "title", "VALUE": "x"}]}`,
			want: []Op{{Kind: KindAdd, Path: &filter.Path{Attr: "title"}, Value: "x"}},
		},
		{
			name: "schema URN matches case-insensitively",
			body: `{"schemas": ["URN:IETF:PARAMS:SCIM:API:MESSAGES:2.0:PATCHOP"], "Operations": [{"op": "remove", "path": "title"}]}`,
			want: []Op{{Kind: KindRemove, Path: &filter.Path{Attr: "title"}}},
		},
		{
			name: "extra schema URNs ignored",
			body: `{"schemas": ["urn:example:custom", "urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": [{"op": "remove", "path": "title"}]}`,
			want: []Op{{Kind: KindRemove, Path: &filter.Path{Attr: "title"}}},
		},
		{
			name: "unknown envelope and op keys ignored",
			body: `{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"], "Operations": [{"op": "remove", "path": "title", "extra": 1}], "id": "x"}`,
			want: []Op{{Kind: KindRemove, Path: &filter.Path{Attr: "title"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.body), &rt, Profile{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDecodeMaxOperations pins the Operations bound exactly at
// MaxOperations.
func TestDecodeMaxOperations(t *testing.T) {
	rt := scim.UserResourceType()
	tests := []struct {
		name     string
		count    int
		wantType string
	}{
		{name: "at the bound", count: MaxOperations},
		{name: "one over", count: MaxOperations + 1, wantType: scim.ScimTypeInvalidSyntax},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := `{"op": "replace", "path": "title", "value": "x"}`
			ops := make([]string, tt.count)
			for i := range ops {
				ops[i] = op
			}
			body := patchBody(`[` + strings.Join(ops, ",") + `]`)
			got, err := Decode([]byte(body), &rt, Strict)
			if tt.wantType != "" {
				requireScimType(t, err, tt.wantType)
				require.ErrorContains(t, err, strconv.Itoa(MaxOperations))
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.count)
		})
	}
}

// TestDecodeLenientPaths pins that LenientPaths is the only way a
// lenient value-filter shape reaches ParsePath, and that it changes
// nothing else.
func TestDecodeLenientPaths(t *testing.T) {
	rt := scim.UserResourceType()
	tests := []struct {
		name     string
		profile  Profile
		body     string
		wantType string
	}{
		{
			name:    "bare not operand accepted under LenientPaths",
			profile: Profile{LenientPaths: true},
			body:    patchBody(`[{"op": "remove", "path": "emails[not type eq \"work\"]"}]`),
		},
		{
			name:     "bare not operand rejected by Strict",
			profile:  Strict,
			body:     patchBody(`[{"op": "remove", "path": "emails[not type eq \"work\"]"}]`),
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "bare not operand rejected by Default",
			profile:  Default,
			body:     patchBody(`[{"op": "remove", "path": "emails[not type eq \"work\"]"}]`),
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "malformed path still invalidPath under LenientPaths",
			profile:  Profile{LenientPaths: true},
			body:     patchBody(`[{"op": "remove", "path": "emails["}]`),
			wantType: scim.ScimTypeInvalidPath,
		},
		{
			name:     "nested value filter still invalidPath under LenientPaths",
			profile:  Profile{LenientPaths: true},
			body:     patchBody(`[{"op": "remove", "path": "emails[emails[type pr]]"}]`),
			wantType: scim.ScimTypeInvalidPath,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode([]byte(tt.body), &rt, tt.profile)
			if tt.wantType != "" {
				requireScimType(t, err, tt.wantType)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, `emails[not (type eq "work")]`, got[0].Path.String(), "rendering is always strict grammar")
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	rt := scim.UserResourceType()
	tests := []struct {
		name         string
		body         string
		wantScimType string
	}{
		{
			name:         "invalid JSON",
			body:         `{`,
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "trailing data",
			body:         patchBody(`[{"op": "remove", "path": "title"}]`) + `{}`,
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "body not an object",
			body:         `[1, 2]`,
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "schemas missing",
			body:         `{"Operations": [{"op": "remove", "path": "title"}]}`,
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "schemas not an array",
			body:         `{"schemas": "urn:ietf:params:scim:api:messages:2.0:PatchOp", "Operations": [{"op": "remove", "path": "title"}]}`,
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "wrong schema URN",
			body:         `{"schemas": ["urn:ietf:params:scim:api:messages:2.0:BulkRequest"], "Operations": [{"op": "remove", "path": "title"}]}`,
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "Operations missing",
			body:         `{"schemas": ["urn:ietf:params:scim:api:messages:2.0:PatchOp"]}`,
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "Operations not an array",
			body:         patchBody(`{"op": "remove"}`),
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "Operations empty",
			body:         patchBody(`[]`),
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "operation not an object",
			body:         patchBody(`["remove"]`),
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "op missing",
			body:         patchBody(`[{"path": "title"}]`),
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "op not a string",
			body:         patchBody(`[{"op": 1, "path": "title"}]`),
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "unknown op",
			body:         patchBody(`[{"op": "merge", "path": "title", "value": "x"}]`),
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "capitalized op rejected under strict",
			body:         patchBody(`[{"op": "Add", "path": "title", "value": "x"}]`),
			wantScimType: scim.ScimTypeInvalidSyntax,
		},
		{
			name:         "path not a string",
			body:         patchBody(`[{"op": "remove", "path": 1}]`),
			wantScimType: scim.ScimTypeInvalidPath,
		},
		{
			name:         "malformed path",
			body:         patchBody(`[{"op": "remove", "path": "emails["}]`),
			wantScimType: scim.ScimTypeInvalidPath,
		},
		{
			name:         "empty path string",
			body:         patchBody(`[{"op": "remove", "path": ""}]`),
			wantScimType: scim.ScimTypeInvalidPath,
		},
		{
			name:         "add without value",
			body:         patchBody(`[{"op": "add", "path": "title"}]`),
			wantScimType: scim.ScimTypeInvalidValue,
		},
		{
			name:         "add with null value",
			body:         patchBody(`[{"op": "add", "path": "title", "value": null}]`),
			wantScimType: scim.ScimTypeInvalidValue,
		},
		{
			name:         "replace without value",
			body:         patchBody(`[{"op": "replace", "path": "title"}]`),
			wantScimType: scim.ScimTypeInvalidValue,
		},
		{
			name:         "remove with value rejected under strict",
			body:         patchBody(`[{"op": "remove", "path": "members", "value": [{"value": "2819c223"}]}]`),
			wantScimType: scim.ScimTypeInvalidValue,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.body), &rt, Profile{})
			var scimErr *scim.Error
			require.ErrorAs(t, err, &scimErr)
			assert.Equal(t, tt.wantScimType, scimErr.ScimType)
			assert.Equal(t, 400, scimErr.Status)
		})
	}
}
