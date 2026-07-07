package filter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

// Match evaluates a filter against a resource (RFC 7644 §3.4.2.2 Table 3
// semantics): string comparison honors the schema's caseExact, dateTime
// compares chronologically, multi-valued attributes match existentially,
// and valuePath predicates correlate per element. Undefined attributes and
// unknown schema URNs evaluate as no-value — the clause is false, never an
// error. The only errors are structural: an ordering operator (gt/ge/lt/le)
// on a resolved boolean or binary attribute is scim.ErrInvalidFilter.
func Match(res *scim.Resource, e Expr) (bool, error) {
	ev := &evaluator{res: res, rt: res.ResourceType(), common: scim.CommonAttributes()}
	if err := ev.validate(e, nil); err != nil {
		return false, err
	}
	return ev.eval(e), nil
}

type evaluator struct {
	res    *scim.Resource
	rt     *scim.ResourceType
	common []scim.Attribute
}

// resolved carries the schema metadata a path resolved to. target is sub
// when the path names a sub-attribute, else attr.
type resolved struct {
	attr *scim.Attribute
	sub  *scim.Attribute
}

func (r resolved) target() *scim.Attribute {
	if r.sub != nil {
		return r.sub
	}
	return r.attr
}

// existentialSub reports the multi-valued complex + sub-attribute case
// (emails.value), which needs per-element probing instead of Resource.Get.
func (r resolved) existentialSub() bool {
	return r.sub != nil && r.attr.MultiValued && r.attr.Type == scim.TypeComplex
}

// resolve maps a path to schema metadata. ok=false means the path is
// undefined for this resource type — a no-value, never an error.
func (ev *evaluator) resolve(p Path) (resolved, bool) {
	attrs, ok := ev.namespace(p.URN)
	if !ok {
		return resolved{}, false
	}
	attr := findAttr(attrs, p.Attr)
	if attr == nil && p.URN == "" {
		attr = findAttr(ev.common, p.Attr)
	}
	if attr == nil {
		return resolved{}, false
	}
	r := resolved{attr: attr}
	if p.SubAttr == "" {
		return r, true
	}
	if attr.Type != scim.TypeComplex {
		return resolved{}, false
	}
	r.sub = findAttr(attr.SubAttributes, p.SubAttr)
	if r.sub == nil {
		return resolved{}, false
	}
	return r, true
}

// namespace returns the attribute list the URN selects: core schema for ""
// or the core URN, a declared extension's attributes otherwise.
func (ev *evaluator) namespace(urn string) ([]scim.Attribute, bool) {
	if urn == "" || scim.FoldName(urn) == scim.FoldName(ev.rt.Schema.ID) {
		return ev.rt.Schema.Attributes, true
	}
	folded := scim.FoldName(urn)
	for i := range ev.rt.Extensions {
		if scim.FoldName(ev.rt.Extensions[i].Schema.ID) == folded {
			return ev.rt.Extensions[i].Schema.Attributes, true
		}
	}
	return nil, false
}

func findAttr(attrs []scim.Attribute, name string) *scim.Attribute {
	folded := scim.FoldName(name)
	for i := range attrs {
		if scim.FoldName(attrs[i].Name) == folded {
			return &attrs[i]
		}
	}
	return nil
}

// pathText renders the Resource.Get path for a resolved filter path.
func pathText(p Path, includeSub bool) string {
	s := p.Attr
	if p.URN != "" {
		s = p.URN + ":" + s
	}
	if includeSub && p.SubAttr != "" {
		s += "." + p.SubAttr
	}
	return s
}

// validate walks the expression once before evaluation, rejecting ordering
// operators on boolean or binary attributes (RFC 7644 Table 3 leaves them
// undefined; the impl doc pins them as invalidFilter). parent is non-nil
// inside a valuePath, where bare names resolve as its sub-attributes.
func (ev *evaluator) validate(e Expr, parent *scim.Attribute) error {
	switch t := e.(type) {
	case AndExpr:
		if err := ev.validate(t.L, parent); err != nil {
			return err
		}
		return ev.validate(t.R, parent)
	case OrExpr:
		if err := ev.validate(t.L, parent); err != nil {
			return err
		}
		return ev.validate(t.R, parent)
	case NotExpr:
		return ev.validate(t.E, parent)
	case Compare:
		return ev.validateCompare(&t, parent)
	case ValuePath:
		if r, ok := ev.resolve(t.Path); ok {
			return ev.validate(t.Filter, r.attr)
		}
		return nil
	case Present:
		return nil
	default:
		return nil
	}
}

func (ev *evaluator) validateCompare(c *Compare, parent *scim.Attribute) error {
	if !c.Op.ordering() {
		return nil
	}
	target, ok := ev.resolveTarget(c.Path, parent)
	if !ok {
		return nil
	}
	if target.Type == scim.TypeBoolean || target.Type == scim.TypeBinary {
		return scim.ErrInvalidFilter(
			"ordering operator " + string(c.Op) + " is not defined for " +
				string(target.Type) + " attribute " + target.Name,
		)
	}
	return nil
}

// resolveTarget resolves a comparison's target attribute in either the
// resource context or, when parent is set, a valuePath element context.
func (ev *evaluator) resolveTarget(p Path, parent *scim.Attribute) (*scim.Attribute, bool) {
	if parent == nil {
		r, ok := ev.resolve(p)
		if !ok {
			return nil, false
		}
		return r.target(), true
	}
	if p.URN != "" || p.SubAttr != "" {
		return nil, false
	}
	sub := findAttr(parent.SubAttributes, p.Attr)
	return sub, sub != nil
}

// eval evaluates a validated expression; undefined paths are no-values, so
// no error can arise here.
func (ev *evaluator) eval(e Expr) bool {
	switch t := e.(type) {
	case AndExpr:
		return ev.eval(t.L) && ev.eval(t.R)
	case OrExpr:
		return ev.eval(t.L) || ev.eval(t.R)
	case NotExpr:
		return !ev.eval(t.E)
	case Compare:
		return ev.evalCompare(&t)
	case Present:
		return ev.evalPresent(t)
	case ValuePath:
		return ev.evalValuePath(&t)
	default:
		return false
	}
}

// evalCompare applies existential semantics: the clause is true when any
// candidate value satisfies the comparison (RFC 7644 §3.4.2.1).
func (ev *evaluator) evalCompare(c *Compare) bool {
	r, ok := ev.resolve(c.Path)
	if !ok {
		return false
	}
	for _, got := range ev.candidates(r, c.Path) {
		if compareValue(r.target(), got, c.Op, c.Value) {
			return true
		}
	}
	return false
}

// candidates collects the values a comparison tests. Unassigned resolves to
// a single nil candidate so eq null matches absence (RFC 7643 §2.5
// null/absent equivalence).
func (ev *evaluator) candidates(r resolved, p Path) []any {
	if r.existentialSub() {
		return ev.subCandidates(r, p)
	}
	v, ok := ev.res.Get(pathText(p, true))
	if !ok {
		return []any{nil}
	}
	if r.attr.MultiValued && r.sub == nil {
		return multiCandidates(v)
	}
	return []any{v}
}

// subCandidates projects one sub-attribute across a multi-valued complex
// attribute's elements: the emails.value existential shorthand.
func (ev *evaluator) subCandidates(r resolved, p Path) []any {
	v, ok := ev.res.Get(pathText(p, false))
	if !ok {
		return []any{nil}
	}
	mcv, ok := v.(scim.MultiComplexValue)
	if !ok {
		return []any{nil}
	}
	folded := scim.FoldName(r.sub.Name)
	out := make([]any, len(mcv))
	for i, elem := range mcv {
		out[i] = elem[folded] // missing key stays nil so eq null can match
	}
	return out
}

// multiCandidates flattens the closed set of multi-valued stored forms.
func multiCandidates(v any) []any {
	switch t := v.(type) {
	case []string:
		return toAny(t)
	case []bool:
		return toAny(t)
	case []int64:
		return toAny(t)
	case []float64:
		return toAny(t)
	case []time.Time:
		return toAny(t)
	case [][]byte:
		return toAny(t)
	case []scim.ReferenceValue:
		return toAny(t)
	case scim.MultiComplexValue:
		return toAny(t)
	default:
		return []any{v}
	}
}

func toAny[T any](in []T) []any {
	out := make([]any, len(in))
	for i, e := range in {
		out[i] = e
	}
	return out
}

func (ev *evaluator) evalPresent(p Present) bool {
	r, ok := ev.resolve(p.Path)
	if !ok {
		return false
	}
	if r.existentialSub() {
		for _, got := range ev.subCandidates(r, p.Path) {
			if got != nil {
				return true
			}
		}
		return false
	}
	_, assigned := ev.res.Get(pathText(p.Path, true))
	return assigned
}

// evalValuePath correlates the bracketed filter per element: each element
// must satisfy the whole inner filter by itself. Flattening sub-attribute
// values across elements first would accept cross-element false positives.
func (ev *evaluator) evalValuePath(vp *ValuePath) bool {
	r, ok := ev.resolve(vp.Path)
	if !ok {
		return false
	}
	v, assigned := ev.res.Get(pathText(vp.Path, true))
	if !assigned {
		return false
	}
	switch t := v.(type) {
	case scim.MultiComplexValue:
		for _, elem := range t {
			if ev.evalElement(vp.Filter, elem, r.target()) {
				return true
			}
		}
		return false
	case scim.ComplexValue:
		// A single-valued complex attribute behaves as a one-element list.
		return ev.evalElement(vp.Filter, t, r.target())
	default:
		return false
	}
}

// evalElement evaluates a valuePath's inner filter against one element:
// bare attribute names resolve as sub-attributes of the parent.
func (ev *evaluator) evalElement(e Expr, elem scim.ComplexValue, parent *scim.Attribute) bool {
	switch t := e.(type) {
	case AndExpr:
		return ev.evalElement(t.L, elem, parent) && ev.evalElement(t.R, elem, parent)
	case OrExpr:
		return ev.evalElement(t.L, elem, parent) || ev.evalElement(t.R, elem, parent)
	case NotExpr:
		return !ev.evalElement(t.E, elem, parent)
	case Compare:
		sub, ok := ev.resolveTarget(t.Path, parent)
		if !ok {
			return false
		}
		return compareValue(sub, elem[scim.FoldName(sub.Name)], t.Op, t.Value)
	case Present:
		sub, ok := ev.resolveTarget(t.Path, parent)
		if !ok {
			return false
		}
		return elem[scim.FoldName(sub.Name)] != nil
	default:
		// Nested valuePath is not grammatical inside a valFilter.
		return false
	}
}

// compareValue applies one comparison against one stored value. got == nil
// means unassigned; want is the strict JSON literal from the AST. Type
// mismatches are non-matches, never errors, matching the tolerant
// evaluation policy.
func compareValue(a *scim.Attribute, got any, op Op, want any) bool {
	if want == nil {
		switch op {
		case OpEqual:
			return got == nil
		case OpNotEqual:
			return got != nil
		case OpContains, OpStartsWith, OpEndsWith,
			OpGreaterThan, OpGreaterThanOrEqual, OpLessThan, OpLessThanOrEqual, OpPresent:
			return false
		default:
			return false
		}
	}
	if got == nil {
		return false
	}
	switch a.Type {
	case scim.TypeString:
		return compareStringForm(got, op, want, a.CaseExact)
	case scim.TypeReference:
		return compareStringForm(got, op, want, a.CaseExact)
	case scim.TypeBoolean:
		return compareBool(got, op, want)
	case scim.TypeInteger, scim.TypeDecimal:
		return compareNumber(got, op, want)
	case scim.TypeDateTime:
		return compareDateTime(got, op, want)
	case scim.TypeBinary:
		return compareBinary(got, op, want)
	case scim.TypeComplex:
		return false
	default:
		return false
	}
}

// compareStringForm covers string and reference stored forms.
func compareStringForm(got any, op Op, want any, caseExact bool) bool {
	w, ok := want.(string)
	if !ok {
		return false
	}
	var g string
	switch t := got.(type) {
	case string:
		g = t
	case scim.ReferenceValue:
		g = string(t)
	default:
		return false
	}
	if !caseExact {
		g, w = scim.FoldName(g), scim.FoldName(w)
	}
	return compareOrdered(op, strings.Compare(g, w), func() bool {
		switch op {
		case OpContains:
			return strings.Contains(g, w)
		case OpStartsWith:
			return strings.HasPrefix(g, w)
		case OpEndsWith:
			return strings.HasSuffix(g, w)
		default:
			return false
		}
	})
}

// compareOrdered maps eq/ne and the ordering operators onto a three-way
// comparison; other operators fall through to the substring callback.
func compareOrdered(op Op, cmp int, substring func() bool) bool {
	switch op {
	case OpEqual:
		return cmp == 0
	case OpNotEqual:
		return cmp != 0
	case OpGreaterThan:
		return cmp > 0
	case OpGreaterThanOrEqual:
		return cmp >= 0
	case OpLessThan:
		return cmp < 0
	case OpLessThanOrEqual:
		return cmp <= 0
	case OpContains, OpStartsWith, OpEndsWith:
		return substring()
	case OpPresent:
		return false
	default:
		return false
	}
}

func compareBool(got any, op Op, want any) bool {
	g, gok := got.(bool)
	w, wok := want.(bool)
	if !gok || !wok {
		return false
	}
	switch op {
	case OpEqual:
		return g == w
	case OpNotEqual:
		return g != w
	case OpContains, OpStartsWith, OpEndsWith,
		OpGreaterThan, OpGreaterThanOrEqual, OpLessThan, OpLessThanOrEqual, OpPresent:
		return false
	default:
		return false
	}
}

// compareNumber compares integer and decimal stored forms against a number
// literal in float64 space; int64 stored values fit exactly within SCIM's
// practical ranges.
func compareNumber(got any, op Op, want any) bool {
	n, ok := want.(json.Number)
	if !ok {
		return false
	}
	w, err := n.Float64()
	if err != nil {
		return false
	}
	var g float64
	switch t := got.(type) {
	case int64:
		g = float64(t)
	case float64:
		g = t
	default:
		return false
	}
	switch {
	case g < w:
		return compareOrdered(op, -1, func() bool { return false })
	case g > w:
		return compareOrdered(op, 1, func() bool { return false })
	default:
		return compareOrdered(op, 0, func() bool { return false })
	}
}

// compareDateTime compares chronologically, never textually: equal instants
// in different offsets are equal.
func compareDateTime(got any, op Op, want any) bool {
	g, ok := got.(time.Time)
	if !ok {
		return false
	}
	ws, ok := want.(string)
	if !ok {
		return false
	}
	w, err := time.Parse(time.RFC3339, ws)
	if err != nil {
		return false
	}
	switch {
	case g.Before(w):
		return compareOrdered(op, -1, func() bool { return false })
	case g.After(w):
		return compareOrdered(op, 1, func() bool { return false })
	default:
		return compareOrdered(op, 0, func() bool { return false })
	}
}

// compareBinary supports eq/ne on decoded bytes; ordering was rejected at
// validation and substring operators are undefined for binary.
func compareBinary(got any, op Op, want any) bool {
	g, ok := got.([]byte)
	if !ok {
		return false
	}
	ws, ok := want.(string)
	if !ok {
		return false
	}
	w, err := base64.StdEncoding.DecodeString(ws)
	if err != nil {
		return false
	}
	switch op {
	case OpEqual:
		return bytes.Equal(g, w)
	case OpNotEqual:
		return !bytes.Equal(g, w)
	case OpContains, OpStartsWith, OpEndsWith,
		OpGreaterThan, OpGreaterThanOrEqual, OpLessThan, OpLessThanOrEqual, OpPresent:
		return false
	default:
		return false
	}
}
