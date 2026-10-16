package filter

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

// Builder functions construct filter expressions for the client direction
// with correct quoting and escaping — e.String() is always safe to place in
// a query. Paths are attrPath strings ("userName", "name.givenName",
// URN-qualified extension attributes); a path that does not parse is kept
// verbatim so the mistake surfaces in the rendered filter rather than
// vanishing.

// Eq builds path eq value.
func Eq(path string, v any) Expr { return compare(path, OpEqual, v) }

// Ne builds path ne value.
func Ne(path string, v any) Expr { return compare(path, OpNotEqual, v) }

// Co builds path co value.
func Co(path string, v any) Expr { return compare(path, OpContains, v) }

// Sw builds path sw value.
func Sw(path string, v any) Expr { return compare(path, OpStartsWith, v) }

// Ew builds path ew value.
func Ew(path string, v any) Expr { return compare(path, OpEndsWith, v) }

// Gt builds path gt value.
func Gt(path string, v any) Expr { return compare(path, OpGreaterThan, v) }

// Ge builds path ge value.
func Ge(path string, v any) Expr { return compare(path, OpGreaterThanOrEqual, v) }

// Lt builds path lt value.
func Lt(path string, v any) Expr { return compare(path, OpLessThan, v) }

// Le builds path le value.
func Le(path string, v any) Expr { return compare(path, OpLessThanOrEqual, v) }

// Pr builds the unary path pr presence test.
func Pr(path string) Expr { return Present{Path: builderPath(path)} }

// And conjoins two or more expressions, left-associated like the parser.
func And(l, r Expr, more ...Expr) Expr {
	var out Expr = AndExpr{L: l, R: r}
	for _, m := range more {
		out = AndExpr{L: out, R: m}
	}
	return out
}

// Or disjoins two or more expressions, left-associated like the parser.
func Or(l, r Expr, more ...Expr) Expr {
	var out Expr = OrExpr{L: l, R: r}
	for _, m := range more {
		out = OrExpr{L: out, R: m}
	}
	return out
}

// Not negates an expression; String() renders the strict parenthesized
// form.
func Not(e Expr) Expr { return NotExpr{E: e} }

// HasValue builds the valuePath grouping path[filter], matched per element
// of a multi-valued attribute.
func HasValue(path string, f Expr) Expr {
	return ValuePath{Path: builderPath(path), Filter: f}
}

func compare(path string, op Op, v any) Expr {
	return Compare{Path: builderPath(path), Op: op, Value: normalizeValue(v)}
}

// builderPath splits an attrPath string; unparseable input is kept verbatim
// in Attr so the rendered filter shows the mistake.
func builderPath(path string) Path {
	urn, attr, sub, err := splitAttrPath(path, 0)
	if err != nil {
		return Path{Attr: path}
	}
	return Path{URN: urn, Attr: attr, SubAttr: sub}
}

// normalizeValue maps natural Go values onto the AST's closed literal set
// (string, json.Number, bool, nil). time.Time renders as RFC 3339 for
// dateTime comparison; []byte renders as base64 to match binary attribute
// comparison; floats always carry a decimal point or exponent so a
// builder-emitted decimal is never textually an integer literal. An
// unsupported type renders as an "!invalid" diagnostic string so the
// mistake is visible in the filter instead of silently mis-stringified.
func normalizeValue(v any) any {
	if n, ok := integerNumber(v); ok {
		return n
	}
	switch t := v.(type) {
	case nil, string, bool, json.Number:
		return t
	case float32:
		return decimalNumber(float64(t))
	case float64:
		return decimalNumber(t)
	case time.Time:
		return t.Format(time.RFC3339)
	case []byte:
		return base64.StdEncoding.EncodeToString(t)
	case scim.ReferenceValue:
		return string(t)
	default:
		return fmt.Sprintf("!invalid(%T: %v)", t, t)
	}
}

// integerNumber maps every Go integer width onto an integer json.Number.
func integerNumber(v any) (json.Number, bool) {
	switch t := v.(type) {
	case int:
		return json.Number(strconv.FormatInt(int64(t), 10)), true
	case int8:
		return json.Number(strconv.FormatInt(int64(t), 10)), true
	case int16:
		return json.Number(strconv.FormatInt(int64(t), 10)), true
	case int32:
		return json.Number(strconv.FormatInt(int64(t), 10)), true
	case int64:
		return json.Number(strconv.FormatInt(t, 10)), true
	case uint:
		return json.Number(strconv.FormatUint(uint64(t), 10)), true
	case uint8:
		return json.Number(strconv.FormatUint(uint64(t), 10)), true
	case uint16:
		return json.Number(strconv.FormatUint(uint64(t), 10)), true
	case uint32:
		return json.Number(strconv.FormatUint(uint64(t), 10)), true
	case uint64:
		return json.Number(strconv.FormatUint(t, 10)), true
	default:
		return "", false
	}
}

func decimalNumber(f float64) json.Number {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return json.Number(s)
}
