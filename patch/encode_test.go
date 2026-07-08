package patch_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

func TestEncodeOps(t *testing.T) {
	tests := []struct {
		name    string
		ops     []patch.Op
		wantErr bool
		// wantOps is a JSON fragment the Operations array must equal.
		wantOps string
	}{
		{
			name: "pathless replace omits path",
			ops: []patch.Op{{
				Kind:  patch.KindReplace,
				Value: map[string]any{"active": false},
			}},
			wantOps: `[{"op":"replace","value":{"active":false}}]`,
		},
		{
			name: "add with path and value",
			ops: []patch.Op{{
				Kind:  patch.KindAdd,
				Path:  &filter.Path{Attr: "members"},
				Value: []any{map[string]any{"value": "u1"}},
			}},
			wantOps: `[{"op":"add","path":"members","value":[{"value":"u1"}]}]`,
		},
		{
			name: "remove with value-path selector omits value",
			ops: []patch.Op{{
				Kind: patch.KindRemove,
				Path: &filter.Path{Attr: "members", ValueFilter: filter.Eq("value", "u1")},
			}},
			wantOps: `[{"op":"remove","path":"members[value eq \"u1\"]"}]`,
		},
		{
			name: "replace scalar sub-attribute",
			ops: []patch.Op{{
				Kind:  patch.KindReplace,
				Path:  &filter.Path{Attr: "name", SubAttr: "givenName"},
				Value: "Barbara",
			}},
			wantOps: `[{"op":"replace","path":"name.givenName","value":"Barbara"}]`,
		},
		{
			name:    "empty ops is an error",
			ops:     nil,
			wantErr: true,
		},
		{
			name:    "remove without path is an error",
			ops:     []patch.Op{{Kind: patch.KindRemove}},
			wantErr: true,
		},
		{
			name:    "add without value is an error",
			ops:     []patch.Op{{Kind: patch.KindAdd, Path: &filter.Path{Attr: "x"}}},
			wantErr: true,
		},
		{
			name:    "unknown kind is an error",
			ops:     []patch.Op{{Kind: patch.Kind("upsert"), Path: &filter.Path{Attr: "x"}, Value: 1}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := patch.EncodeOps(tt.ops)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			var msg struct {
				Schemas    []string        `json:"schemas"`
				Operations json.RawMessage `json:"Operations"`
			}
			require.NoError(t, json.Unmarshal(got, &msg))
			assert.Equal(t, []string{scim.MessagePatchOp}, msg.Schemas)
			assert.JSONEq(t, tt.wantOps, string(msg.Operations))
		})
	}
}

// TestEncodeDecodeRoundTrip encodes ops and decodes them back under
// Strict, asserting the wire form the client produces is exactly what a
// strict server accepts.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	rt := scim.GroupResourceType()
	ops := []patch.Op{
		{Kind: patch.KindAdd, Path: &filter.Path{Attr: "members"}, Value: []any{map[string]any{"value": "u1"}}},
		{Kind: patch.KindRemove, Path: &filter.Path{Attr: "members", ValueFilter: filter.Eq("value", "u2")}},
		{Kind: patch.KindReplace, Path: &filter.Path{Attr: "displayName"}, Value: "Renamed"},
	}
	body, err := patch.EncodeOps(ops)
	require.NoError(t, err)

	decoded, err := patch.Decode(body, &rt, patch.Strict)
	require.NoError(t, err, "strict server accepts the encoded body")
	require.Len(t, decoded, len(ops))
	for i := range ops {
		assert.Equal(t, ops[i].Kind, decoded[i].Kind, "op %d kind", i)
		assert.Equal(t, ops[i].Path.String(), decoded[i].Path.String(), "op %d path", i)
	}
}
