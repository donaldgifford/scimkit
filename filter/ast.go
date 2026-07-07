package filter

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Op is a filter comparison operator (RFC 7644 §3.4.2.2 Table 3), stored in
// canonical lowercase — the parser folds case before constructing the AST.
type Op string

// Comparison operators. OpPresent is the unary pr operator, represented by
// the Present node rather than a Compare.
const (
	OpEqual              Op = "eq"
	OpNotEqual           Op = "ne"
	OpContains           Op = "co"
	OpStartsWith         Op = "sw"
	OpEndsWith           Op = "ew"
	OpGreaterThan        Op = "gt"
	OpGreaterThanOrEqual Op = "ge"
	OpLessThan           Op = "lt"
	OpLessThanOrEqual    Op = "le"
	OpPresent            Op = "pr"
)

// ordering reports whether the operator imposes an order (gt/ge/lt/le),
// which is invalid on boolean and binary attributes.
func (o Op) ordering() bool {
	switch o {
	case OpGreaterThan, OpGreaterThanOrEqual, OpLessThan, OpLessThanOrEqual:
		return true
	case OpEqual, OpNotEqual, OpContains, OpStartsWith, OpEndsWith, OpPresent:
		return false
	default:
		return false
	}
}

// Path addresses an attribute in filter and PATCH-path grammars. URN, Attr,
// and SubAttr preserve the caller's original casing — SCIM matching is
// case-insensitive, so folding is evaluation's job, and String() round-trips
// the original spelling. ValueFilter is non-nil only for PATCH paths of the
// form attr[filter] or attr[filter].sub (RFC 7644 Figure 7).
type Path struct {
	// URN is the schema URN qualifier, "" for unqualified paths. It is
	// lexically split (last colon), not validated against any schema.
	URN string
	// Attr is the top-level attribute name.
	Attr string
	// SubAttr is the sub-attribute name, "" when absent.
	SubAttr string
	// ValueFilter selects elements of a multi-valued attribute.
	ValueFilter Expr
}

// String renders the path in re-parseable form.
func (p Path) String() string {
	var b strings.Builder
	if p.URN != "" {
		b.WriteString(p.URN)
		b.WriteByte(':')
	}
	b.WriteString(p.Attr)
	if p.ValueFilter != nil {
		b.WriteByte('[')
		b.WriteString(p.ValueFilter.String())
		b.WriteByte(']')
	}
	if p.SubAttr != "" {
		b.WriteByte('.')
		b.WriteString(p.SubAttr)
	}
	return b.String()
}

// Expr is a parsed filter expression. Implementations are Compare, Present,
// AndExpr, OrExpr, NotExpr, and ValuePath. String renders a re-parseable filter:
// Parse(e.String()) yields an equal AST.
type Expr interface {
	isExpr()
	String() string
}

// Compare is attrPath op compValue. Value holds the strict JSON literal as
// one of string, json.Number, bool, or nil (the null literal); json.Number
// preserves the integer/decimal distinction until evaluation resolves the
// attribute's schema type.
type Compare struct {
	Path  Path
	Op    Op
	Value any
}

// Present is the unary attrPath pr test.
type Present struct {
	Path Path
}

// AndExpr is the logical conjunction; it binds tighter than OrExpr.
type AndExpr struct {
	L, R Expr
}

// OrExpr is the logical disjunction, the loosest-binding operator.
type OrExpr struct {
	L, R Expr
}

// NotExpr is logical negation of a parenthesized filter; it binds tightest.
type NotExpr struct {
	E Expr
}

// ValuePath is a complex-attribute filter grouping, attr[filter], matched
// per element of the multi-valued attribute (RFC 7644 §3.4.2.2 valuePath).
type ValuePath struct {
	Path   Path
	Filter Expr
}

func (Compare) isExpr()   {}
func (Present) isExpr()   {}
func (AndExpr) isExpr()   {}
func (OrExpr) isExpr()    {}
func (NotExpr) isExpr()   {}
func (ValuePath) isExpr() {}

func (c Compare) String() string {
	return c.Path.String() + " " + string(c.Op) + " " + literalString(c.Value)
}

func (p Present) String() string {
	return p.Path.String() + " pr"
}

// String parenthesizes operands whose bare rendering would reparse to a
// different tree: or under and (and binds tighter), and any same-operator
// right child — the parser left-associates, so a right-nested
// AndExpr{a, AndExpr{b, c}} rendered flat would come back left-nested.
func (a AndExpr) String() string {
	return andLeft(a.L) + " and " + andRight(a.R)
}

func andLeft(e Expr) string {
	if _, ok := e.(OrExpr); ok {
		return "(" + e.String() + ")"
	}
	return e.String()
}

func andRight(e Expr) string {
	switch e.(type) {
	case OrExpr, AndExpr:
		return "(" + e.String() + ")"
	default:
		return e.String()
	}
}

func (o OrExpr) String() string {
	return o.L.String() + " or " + orRight(o.R)
}

func orRight(e Expr) string {
	if _, ok := e.(OrExpr); ok {
		return "(" + e.String() + ")"
	}
	return e.String()
}

// String always renders the strict grammar form not (filter), regardless of
// how a lenient parse accepted the input.
func (n NotExpr) String() string {
	return "not (" + n.E.String() + ")"
}

func (v ValuePath) String() string {
	return v.Path.String() + "[" + v.Filter.String() + "]"
}

// literalString renders a compValue literal. Strings go through
// encoding/json so escaping matches the grammar's JSON string production
// exactly.
// litNull is the JSON null literal, shared by rendering and parsing.
const litNull = "null"

func literalString(v any) string {
	switch t := v.(type) {
	case nil:
		return litNull
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	case string:
		b, err := json.Marshal(t)
		if err != nil {
			// Unreachable: marshalling a Go string cannot fail.
			return strconv.Quote(t)
		}
		return string(b)
	default:
		// Unreachable for parser- and builder-constructed ASTs; render
		// something diagnosable for hand-built ones.
		return litNull
	}
}
