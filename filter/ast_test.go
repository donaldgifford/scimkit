package filter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExprString(t *testing.T) {
	userName := Path{Attr: "userName"}
	active := Path{Attr: "active"}
	title := Path{Attr: "title"}
	tests := []struct {
		name string
		expr Expr
		want string
	}{
		{
			name: "compare string",
			expr: Compare{Path: userName, Op: OpEqual, Value: "bjensen"},
			want: `userName eq "bjensen"`,
		},
		{
			name: "compare escapes quotes",
			expr: Compare{Path: title, Op: OpContains, Value: `a "b" \ c`},
			want: `title co "a \"b\" \\ c"`,
		},
		{
			name: "compare bool",
			expr: Compare{Path: active, Op: OpEqual, Value: true},
			want: `active eq true`,
		},
		{
			name: "compare null",
			expr: Compare{Path: title, Op: OpEqual, Value: nil},
			want: `title eq null`,
		},
		{
			name: "compare integer keeps raw form",
			expr: Compare{Path: title, Op: OpGreaterThan, Value: json.Number("3")},
			want: `title gt 3`,
		},
		{
			name: "compare decimal keeps raw form",
			expr: Compare{Path: title, Op: OpLessThanOrEqual, Value: json.Number("2.50")},
			want: `title le 2.50`,
		},
		{
			name: "off-set value renders unlexable diagnostic, not null",
			expr: Compare{Path: title, Op: OpEqual, Value: 3.5},
			want: `title eq !invalid(float64)`,
		},
		{
			name: "present",
			expr: Present{Path: title},
			want: `title pr`,
		},
		{
			name: "URN-qualified path",
			expr: Present{Path: Path{
				URN:  "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User",
				Attr: "employeeNumber",
			}},
			want: `urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber pr`,
		},
		{
			name: "sub-attribute path",
			expr: Compare{Path: Path{Attr: "name", SubAttr: "givenName"}, Op: OpEqual, Value: "B"},
			want: `name.givenName eq "B"`,
		},
		{
			name: "and",
			expr: AndExpr{
				L: Compare{Path: userName, Op: OpEqual, Value: "b"},
				R: Compare{Path: active, Op: OpEqual, Value: true},
			},
			want: `userName eq "b" and active eq true`,
		},
		{
			name: "or of ands needs no parens",
			expr: OrExpr{
				L: AndExpr{L: Present{Path: title}, R: Present{Path: active}},
				R: Present{Path: userName},
			},
			want: `title pr and active pr or userName pr`,
		},
		{
			name: "or operand of and is parenthesized",
			expr: AndExpr{
				L: OrExpr{L: Present{Path: title}, R: Present{Path: active}},
				R: Present{Path: userName},
			},
			want: `(title pr or active pr) and userName pr`,
		},
		{
			name: "not always parenthesizes",
			expr: NotExpr{E: Compare{Path: userName, Op: OpEqual, Value: "b"}},
			want: `not (userName eq "b")`,
		},
		{
			name: "value path",
			expr: ValuePath{
				Path: Path{Attr: "emails"},
				Filter: AndExpr{
					L: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "work"},
					R: Compare{Path: Path{Attr: "value"}, Op: OpContains, Value: "@example.com"},
				},
			},
			want: `emails[type eq "work" and value co "@example.com"]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.expr.String())
		})
	}
}

func TestPathString(t *testing.T) {
	tests := []struct {
		name string
		path Path
		want string
	}{
		{name: "bare attribute", path: Path{Attr: "members"}, want: "members"},
		{name: "sub-attribute", path: Path{Attr: "name", SubAttr: "givenName"}, want: "name.givenName"},
		{
			name: "value filter",
			path: Path{
				Attr:        "emails",
				ValueFilter: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "work"},
			},
			want: `emails[type eq "work"]`,
		},
		{
			name: "value filter with sub-attribute",
			path: Path{
				Attr:        "emails",
				SubAttr:     "value",
				ValueFilter: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "work"},
			},
			want: `emails[type eq "work"].value`,
		},
		{
			name: "URN qualified",
			path: Path{URN: "urn:x:2.0:Thing", Attr: "field", SubAttr: "sub"},
			want: "urn:x:2.0:Thing:field.sub",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.path.String())
		})
	}
}

func TestOpOrdering(t *testing.T) {
	tests := []struct {
		name string
		op   Op
		want bool
	}{
		{name: "gt", op: OpGreaterThan, want: true},
		{name: "ge", op: OpGreaterThanOrEqual, want: true},
		{name: "lt", op: OpLessThan, want: true},
		{name: "le", op: OpLessThanOrEqual, want: true},
		{name: "eq", op: OpEqual, want: false},
		{name: "co", op: OpContains, want: false},
		{name: "pr", op: OpPresent, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.op.ordering())
		})
	}
}
