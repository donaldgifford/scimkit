package filter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/donaldgifford/scimkit/scim"
)

func TestBuilder(t *testing.T) {
	tests := []struct {
		name string
		expr Expr
		want string
	}{
		{name: "eq string", expr: Eq("userName", "bjensen"), want: `userName eq "bjensen"`},
		{
			name: "eq escapes value",
			expr: Eq("title", `Tour "Guide" \ Lead`),
			want: `title eq "Tour \"Guide\" \\ Lead"`,
		},
		{name: "ne", expr: Ne("userType", "Intern"), want: `userType ne "Intern"`},
		{name: "co", expr: Co("emails.value", "@example.com"), want: `emails.value co "@example.com"`},
		{name: "sw", expr: Sw("userName", "J"), want: `userName sw "J"`},
		{name: "ew", expr: Ew("userName", ".org"), want: `userName ew ".org"`},
		{name: "gt int", expr: Gt("count", 3), want: `count gt 3`},
		{name: "ge int64", expr: Ge("count", int64(9000000000)), want: `count ge 9000000000`},
		{name: "int16", expr: Eq("count", int16(-3)), want: `count eq -3`},
		{name: "uint", expr: Eq("count", uint(7)), want: `count eq 7`},
		{
			name: "uint64 above int64 range",
			expr: Eq("count", uint64(18446744073709551615)),
			want: `count eq 18446744073709551615`,
		},
		{name: "lt float", expr: Lt("ratio", 2.5), want: `ratio lt 2.5`},
		{
			name: "whole float keeps decimal point",
			expr: Le("ratio", 2.0),
			want: `ratio le 2.0`,
		},
		{name: "eq bool", expr: Eq("active", true), want: `active eq true`},
		{name: "eq null", expr: Eq("nickName", nil), want: `nickName eq null`},
		{
			name: "time renders RFC 3339",
			expr: Gt("meta.lastModified", time.Date(2011, 5, 13, 4, 42, 34, 0, time.UTC)),
			want: `meta.lastModified gt "2011-05-13T04:42:34Z"`,
		},
		{name: "pr", expr: Pr("title"), want: `title pr`},
		{
			name: "URN-qualified path",
			expr: Eq(scim.SchemaEnterpriseUser+":employeeNumber", "701984"),
			want: scim.SchemaEnterpriseUser + `:employeeNumber eq "701984"`,
		},
		{
			name: "and",
			expr: And(Eq("userName", "b"), Pr("title")),
			want: `userName eq "b" and title pr`,
		},
		{
			name: "variadic and is left-associated",
			expr: And(Pr("a"), Pr("b"), Pr("c")),
			want: `a pr and b pr and c pr`,
		},
		{
			name: "or",
			expr: Or(Pr("a"), Pr("b"), Pr("c")),
			want: `a pr or b pr or c pr`,
		},
		{
			name: "or inside and gets parens",
			expr: And(Or(Pr("a"), Pr("b")), Pr("c")),
			want: `(a pr or b pr) and c pr`,
		},
		{name: "not", expr: Not(Pr("a")), want: `not (a pr)`},
		{
			name: "has value",
			expr: HasValue("emails", And(Eq("type", "work"), Co("value", "@x"))),
			want: `emails[type eq "work" and value co "@x"]`,
		},
		{
			name: "unparseable path kept verbatim",
			expr: Pr("9bad path"),
			want: `9bad path pr`,
		},
		{
			name: "byte slice renders base64 matching binary comparison",
			expr: Eq("x509Certificates.value", []byte("der-bytes")),
			want: `x509Certificates.value eq "ZGVyLWJ5dGVz"`,
		},
		{
			name: "unsupported value type renders visible diagnostic",
			expr: Eq("a", struct{}{}),
			want: `a eq "!invalid(struct {}: {})"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.expr.String())
		})
	}
}
