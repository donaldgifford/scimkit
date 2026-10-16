package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

func TestParsePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Path
	}{
		{name: "bare attribute", input: `members`, want: Path{Attr: "members"}},
		{name: "sub-attribute", input: `name.givenName`, want: Path{Attr: "name", SubAttr: "givenName"}},
		{
			name:  "URN qualified",
			input: scim.SchemaEnterpriseUser + `:manager.displayName`,
			want:  Path{URN: scim.SchemaEnterpriseUser, Attr: "manager", SubAttr: "displayName"},
		},
		{
			name:  "value path",
			input: `emails[type eq "work"]`,
			want: Path{
				Attr:        "emails",
				ValueFilter: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "work"},
			},
		},
		{
			name:  "value path with trailing sub-attribute",
			input: `emails[type eq "work"].value`,
			want: Path{
				Attr:        "emails",
				SubAttr:     "value",
				ValueFilter: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "work"},
			},
		},
		{
			name:  "entra-style member selector",
			input: `members[value eq "2819c223"]`,
			want: Path{
				Attr:        "members",
				ValueFilter: Compare{Path: Path{Attr: "value"}, Op: OpEqual, Value: "2819c223"},
			},
		},
		{
			name:  "compound value filter",
			input: `addresses[type eq "work" and locality eq "Hollywood"].streetAddress`,
			want: Path{
				Attr:    "addresses",
				SubAttr: "streetAddress",
				ValueFilter: AndExpr{
					L: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "work"},
					R: Compare{Path: Path{Attr: "locality"}, Op: OpEqual, Value: "Hollywood"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePath(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParsePathErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "empty", input: ``, wantErr: "expected attribute path"},
		{name: "operator junk after path", input: `userName eq "x"`, wantErr: "expected end of path"},
		{name: "unclosed value filter", input: `emails[type pr`, wantErr: "expected ']'"},
		{name: "empty value filter", input: `emails[]`, wantErr: "expected attribute expression"},
		{
			name:    "sub-attribute before value filter",
			input:   `emails.value[type eq "work"]`,
			wantErr: "value filter must follow the attribute",
		},
		{
			name:    "double sub-attribute after value filter",
			input:   `emails[type pr].a.b`,
			wantErr: "invalid sub-attribute",
		},
		{name: "garbage after value filter", input: `emails[type pr]value`, wantErr: "expected .subAttr"},
		{name: "number in path position", input: `42`, wantErr: "expected attribute path"},
		{name: "nested value filter", input: `emails[emails[type pr]]`, wantErr: "cannot nest inside a value filter"},
		{
			name:    "nested value filter with sub-attribute",
			input:   `members[groups[value eq "x"] pr].value`,
			wantErr: "cannot nest inside a value filter",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePath(tt.input)
			var scimErr *scim.Error
			require.ErrorAs(t, err, &scimErr)
			assert.Equal(t, scim.ScimTypeInvalidPath, scimErr.ScimType, "PATCH path errors are invalidPath")
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// TestParsePathStringRoundTrip pins Path.String() re-parseability.
func TestParsePathStringRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "bare", input: `members`},
		{name: "sub", input: `name.givenName`},
		{name: "value filter", input: `emails[type eq "work"]`},
		{name: "value filter with sub", input: `emails[type eq "work"].value`},
		{name: "urn", input: scim.SchemaEnterpriseUser + `:manager.displayName`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := ParsePath(tt.input)
			require.NoError(t, err)
			second, err := ParsePath(first.String())
			require.NoError(t, err)
			assert.Equal(t, first, second)
		})
	}
}
