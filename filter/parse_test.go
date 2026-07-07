package filter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

func TestParse(t *testing.T) {
	userName := Path{Attr: "userName"}
	tests := []struct {
		name  string
		input string
		want  Expr
	}{
		{
			name:  "simple equality",
			input: `userName eq "bjensen"`,
			want:  Compare{Path: userName, Op: OpEqual, Value: "bjensen"},
		},
		{
			name:  "operators fold case",
			input: `userName EQ "bjensen"`,
			want:  Compare{Path: userName, Op: OpEqual, Value: "bjensen"},
		},
		{
			name:  "keywords fold case",
			input: `userName pr AND userName pr OR userName pr`,
			want: Or{
				L: And{L: Present{Path: userName}, R: Present{Path: userName}},
				R: Present{Path: userName},
			},
		},
		{
			name:  "path casing preserved",
			input: `USERNAME eq "x"`,
			want:  Compare{Path: Path{Attr: "USERNAME"}, Op: OpEqual, Value: "x"},
		},
		{
			name:  "and binds tighter than or",
			input: `a pr or b pr and c pr`,
			want: Or{
				L: Present{Path: Path{Attr: "a"}},
				R: And{L: Present{Path: Path{Attr: "b"}}, R: Present{Path: Path{Attr: "c"}}},
			},
		},
		{
			name:  "grouping overrides precedence",
			input: `(a pr or b pr) and c pr`,
			want: And{
				L: Or{L: Present{Path: Path{Attr: "a"}}, R: Present{Path: Path{Attr: "b"}}},
				R: Present{Path: Path{Attr: "c"}},
			},
		},
		{
			name:  "redundant parens are transparent",
			input: `(a eq "x")`,
			want:  Compare{Path: Path{Attr: "a"}, Op: OpEqual, Value: "x"},
		},
		{
			name:  "not with parens",
			input: `not (a eq "x")`,
			want:  Not{E: Compare{Path: Path{Attr: "a"}, Op: OpEqual, Value: "x"}},
		},
		{
			name:  "boolean literal",
			input: `active eq true`,
			want:  Compare{Path: Path{Attr: "active"}, Op: OpEqual, Value: true},
		},
		{
			name:  "null literal",
			input: `title eq null`,
			want:  Compare{Path: Path{Attr: "title"}, Op: OpEqual, Value: nil},
		},
		{
			name:  "integer literal",
			input: `count gt 3`,
			want:  Compare{Path: Path{Attr: "count"}, Op: OpGreaterThan, Value: json.Number("3")},
		},
		{
			name:  "decimal literal",
			input: `ratio le 2.5`,
			want:  Compare{Path: Path{Attr: "ratio"}, Op: OpLessThanOrEqual, Value: json.Number("2.5")},
		},
		{
			name:  "value path",
			input: `emails[type eq "work"]`,
			want: ValuePath{
				Path:   Path{Attr: "emails"},
				Filter: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "work"},
			},
		},
		{
			name:  "URN-qualified attribute",
			input: scim.SchemaEnterpriseUser + `:employeeNumber eq "701984"`,
			want: Compare{
				Path:  Path{URN: scim.SchemaEnterpriseUser, Attr: "employeeNumber"},
				Op:    OpEqual,
				Value: "701984",
			},
		},
		{
			name:  "dollar ref inside value path",
			input: `members[$ref eq "https://example.com/v2/Users/1"]`,
			want: ValuePath{
				Path:   Path{Attr: "members"},
				Filter: Compare{Path: Path{Attr: "$ref"}, Op: OpEqual, Value: "https://example.com/v2/Users/1"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "empty input", input: ``, wantErr: "expected attribute expression"},
		{name: "missing operator", input: `userName`, wantErr: "expected operator"},
		{name: "unknown operator", input: `a xx "b"`, wantErr: `unknown operator "xx"`},
		{name: "missing value", input: `a eq`, wantErr: "expected comparison value"},
		{name: "bare word value", input: `a eq bjensen`, wantErr: "expected JSON literal"},
		{name: "uppercase JSON literal", input: `a eq TRUE`, wantErr: "expected JSON literal"},
		{name: "unclosed paren", input: `(a pr`, wantErr: "expected ')'"},
		{name: "unclosed bracket", input: `emails[type pr`, wantErr: "expected ']'"},
		{name: "bare not is off-grammar in strict mode", input: `not a pr`, wantErr: "parenthesized"},
		{name: "trailing garbage", input: `a pr b`, wantErr: "expected end of filter"},
		{name: "dangling and", input: `a pr and`, wantErr: "expected attribute expression"},
		{name: "value in path position", input: `"a" eq "b"`, wantErr: "expected attribute expression"},
		{name: "unterminated string", input: `a eq "x`, wantErr: "unterminated string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			var scimErr *scim.Error
			require.ErrorAs(t, err, &scimErr)
			assert.Equal(t, scim.ScimTypeInvalidFilter, scimErr.ScimType)
			require.ErrorContains(t, err, tt.wantErr)
			require.ErrorContains(t, err, "position", "errors carry position detail")
		})
	}
}
