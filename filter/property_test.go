package filter

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// genPaths are valid attrPath strings the generator draws from.
var genPaths = []string{
	"userName",
	"name.givenName",
	"active",
	"emails",
	"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber",
	"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:manager.displayName",
	"$ref",
	"meta.lastModified",
}

// genValues covers the closed literal set with hostile string content.
var genValues = []any{
	"simple",
	`with "quotes"`,
	`back\slash`,
	"colon:in:string",
	"[brackets] (parens)",
	"unicode ünïcødé ✓",
	"",
	json.Number("0"),
	json.Number("-17"),
	json.Number("3.14"),
	json.Number("1e10"),
	true,
	false,
	nil,
}

var genOps = []Op{
	OpEqual, OpNotEqual, OpContains, OpStartsWith, OpEndsWith,
	OpGreaterThan, OpGreaterThanOrEqual, OpLessThan, OpLessThanOrEqual,
}

func genLeaf(r *rand.Rand, bare bool) Expr {
	path := Path{Attr: "type"}
	if !bare {
		p := genPaths[r.Intn(len(genPaths))]
		urn, attr, sub, err := splitAttrPath(p, 0)
		if err != nil {
			panic(err)
		}
		path = Path{URN: urn, Attr: attr, SubAttr: sub}
	}
	if r.Intn(4) == 0 {
		return Present{Path: path}
	}
	return Compare{
		Path:  path,
		Op:    genOps[r.Intn(len(genOps))],
		Value: genValues[r.Intn(len(genValues))],
	}
}

// genExpr builds a random valid expression. bare constrains paths to bare
// sub-attribute names and forbids nested valuePath, matching the valFilter
// grammar context.
func genExpr(r *rand.Rand, depth int, bare bool) Expr {
	if depth <= 0 {
		return genLeaf(r, bare)
	}
	switch r.Intn(6) {
	case 0:
		return AndExpr{L: genExpr(r, depth-1, bare), R: genExpr(r, depth-1, bare)}
	case 1:
		return OrExpr{L: genExpr(r, depth-1, bare), R: genExpr(r, depth-1, bare)}
	case 2:
		return NotExpr{E: genExpr(r, depth-1, bare)}
	case 3:
		if bare {
			return genLeaf(r, bare)
		}
		return ValuePath{Path: Path{Attr: "emails"}, Filter: genExpr(r, depth-1, true)}
	default:
		return genLeaf(r, bare)
	}
}

// TestPropertyStringParseIdentity pins Parse(e.String()) == e over a
// seeded corpus of generated expressions — the builder-direction property.
func TestPropertyStringParseIdentity(t *testing.T) {
	tests := []struct {
		name string
		seed int64
	}{
		{name: "seed 1", seed: 1},
		{name: "seed 2", seed: 2},
		{name: "seed 3", seed: 3},
		{name: "seed 4", seed: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewSource(tt.seed))
			for i := 0; i < 250; i++ {
				e := genExpr(r, 4, false)
				rendered := e.String()
				parsed, err := Parse(rendered)
				require.NoError(t, err, "generated filter must reparse: %s", rendered)
				assert.Equal(t, e, parsed, "round trip changed the AST: %s", rendered)
			}
		})
	}
}

// TestPropertyParseStringStable pins that String() output is a fixpoint:
// parsing and re-rendering an arbitrary valid filter converges immediately.
func TestPropertyParseStringStable(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "rfc simple", input: `userName eq "bjensen"`},
		{name: "rfc co", input: `name.familyName co "O'Malley"`},
		{name: "rfc sw", input: `userName sw "J"`},
		{name: "rfc urn sw", input: `urn:ietf:params:scim:schemas:core:2.0:User:userName sw "J"`},
		{name: "rfc pr", input: `title pr`},
		{name: "rfc gt datetime", input: `meta.lastModified gt "2011-05-13T04:42:34Z"`},
		{name: "rfc logic", input: `title pr and userType eq "Employee"`},
		{name: "rfc or", input: `title pr or userType eq "Intern"`},
		{
			name:  "rfc grouping",
			input: `userType eq "Employee" and (emails co "example.com" or emails.value co "example.org")`,
		},
		{
			name:  "rfc not",
			input: `userType ne "Employee" and not (emails co "example.com" or emails.value co "example.org")`,
		},
		{
			name:  "rfc valuePath",
			input: `userType eq "Employee" and emails[type eq "work" and value co "@example.com"]`,
		},
		{
			name:  "rfc double valuePath",
			input: `emails[type eq "work" and value co "@example.com"] or ims[type eq "xmpp" and value co "@foo.com"]`,
		},
		{name: "redundant parens collapse", input: `((a pr))`},
		{name: "case-folded ops canonicalize", input: `a EQ "x" AND b PR`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := Parse(tt.input)
			require.NoError(t, err)
			rendered := first.String()
			second, err := Parse(rendered)
			require.NoError(t, err, "canonical form must reparse: %s", rendered)
			assert.Equal(t, first, second)
			assert.Equal(t, rendered, second.String(), "String is a fixpoint after one parse")
		})
	}
}

// TestPropertyBuilderParseIdentity drives the same identity through the
// public builder functions rather than raw AST construction.
func TestPropertyBuilderParseIdentity(t *testing.T) {
	tests := []struct {
		name string
		expr Expr
	}{
		{name: "flat and", expr: And(Eq("userName", "b"), Pr("title"), Ne("userType", "Intern"))},
		{name: "nested logic", expr: Or(Not(Pr("nickName")), And(Eq("active", true), Gt("count", 3)))},
		{
			name: "value path",
			expr: HasValue("emails", And(Eq("type", "work"), Co("value", "@example.com"))),
		},
		{name: "hostile string", expr: Eq("title", `a "b" \ c [d] :e`)},
		{name: "null", expr: Eq("nickName", nil)},
		{name: "decimal", expr: Lt("ratio", 2.0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := tt.expr.String()
			parsed, err := Parse(rendered)
			require.NoError(t, err, "builder output must parse: %s", rendered)
			assert.Equal(t, tt.expr, parsed)
		})
	}
}

// Guard against the generator itself drifting invalid: every path in
// genPaths must split.
func TestGenPathsAreValid(t *testing.T) {
	for i, p := range genPaths {
		t.Run(fmt.Sprintf("path %d", i), func(t *testing.T) {
			_, _, _, err := splitAttrPath(p, 0)
			require.NoError(t, err)
		})
	}
}
