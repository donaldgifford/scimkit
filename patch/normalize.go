package patch

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// subAttrValue is the canonical "value" sub-attribute name used by the
// selector rewrite and object unwrapping.
const subAttrValue = "value"

// expandPathlessReplace rewrites a pathless replace whose value object
// carries flattened dotted keys ("name.givenName") or URN-qualified keys
// into per-attribute pathed replace ops — Entra ID flattens
// sub-attributes this way instead of nesting objects. Plain keys stay in
// the pathless op, which is dropped when nothing remains. Expanded ops
// are ordered by folded key so output is deterministic.
func expandPathlessReplace(raws []rawOp, p Profile) []rawOp {
	out := make([]rawOp, 0, len(raws))
	for i := range raws {
		ro := raws[i]
		if !isPathlessReplace(&ro, i, p) {
			out = append(out, ro)
			continue
		}
		obj, ok := ro.value.(map[string]any)
		if !ok {
			out = append(out, ro)
			continue
		}
		plain, expanded := splitFlattenedKeys(obj, ro.op)
		if len(plain) > 0 {
			out = append(out, rawOp{op: ro.op, value: plain, hasValue: true})
		}
		out = append(out, expanded...)
	}
	return out
}

func isPathlessReplace(ro *rawOp, index int, p Profile) bool {
	kind, err := opKind(ro.op, index, p)
	return err == nil && kind == KindReplace && !ro.hasPath
}

// splitFlattenedKeys partitions a pathless replace's value object: keys
// containing '.' or ':' (impossible in a plain ATTRNAME) become pathed
// replace ops; the rest stay behind as a plain-key object.
func splitFlattenedKeys(obj map[string]any, op string) (map[string]any, []rawOp) {
	plain := make(map[string]any)
	keys := make([]string, 0, len(obj))
	for k := range obj {
		if strings.ContainsAny(k, ".:") {
			keys = append(keys, k)
			continue
		}
		plain[k] = obj[k]
	}
	sort.Slice(keys, func(i, j int) bool { return scim.FoldName(keys[i]) < scim.FoldName(keys[j]) })
	expanded := make([]rawOp, 0, len(keys))
	for _, k := range keys {
		expanded = append(expanded, rawOp{
			op:       op,
			path:     k,
			hasPath:  true,
			value:    obj[k],
			hasValue: true,
		})
	}
	return plain, expanded
}

// rewriteRemoveSelectors turns a remove carrying an op-level value into
// per-element valuePath removes — Okta removes group members as
// {"op": "remove", "path": "members", "value": [{"value": id}]}. Each
// element becomes members[value eq id]-style ops; element objects with
// several scalar members become eq conjunctions. Anything that cannot
// be a selector (no path, an already-filtered path, non-scalar members)
// is ErrInvalidValue.
func rewriteRemoveSelectors(ops []Op) ([]Op, error) {
	out := make([]Op, 0, len(ops))
	for i := range ops {
		op := ops[i]
		if op.Kind != KindRemove || op.Value == nil {
			out = append(out, op)
			continue
		}
		if op.Path == nil || op.Path.ValueFilter != nil || op.Path.SubAttr != "" {
			return nil, scim.ErrInvalidValue(
				opLabel(i) + ": remove value selector requires a bare attribute path")
		}
		elems, ok := op.Value.([]any)
		if !ok {
			elems = []any{op.Value}
		}
		for _, elem := range elems {
			vf, err := selectorFilter(elem, i)
			if err != nil {
				return nil, err
			}
			out = append(out, Op{Kind: KindRemove, Path: &filter.Path{
				URN:         op.Path.URN,
				Attr:        op.Path.Attr,
				ValueFilter: vf,
			}})
		}
	}
	return out, nil
}

// selectorFilter builds the eq filter one selector element implies: a
// scalar selects by the "value" sub-attribute, an object contributes an
// eq clause per scalar member (null members ignored, keys folded for
// deterministic conjunction order).
func selectorFilter(elem any, index int) (filter.Expr, error) {
	switch t := elem.(type) {
	case string, bool, json.Number:
		return filter.Compare{Path: filter.Path{Attr: subAttrValue}, Op: filter.OpEqual, Value: t}, nil
	case map[string]any:
		return selectorObjectFilter(t, index)
	default:
		return nil, scim.ErrInvalidValue(opLabel(index) + ": remove selector element must be a scalar or object")
	}
}

func selectorObjectFilter(obj map[string]any, index int) (filter.Expr, error) {
	keys := make([]string, 0, len(obj))
	for k, v := range obj {
		if v == nil {
			continue
		}
		switch v.(type) {
		case string, bool, json.Number:
			keys = append(keys, k)
		default:
			return nil, scim.ErrInvalidValue(
				opLabel(index) + ": remove selector member " + strconv.Quote(k) + " must be a scalar")
		}
	}
	if len(keys) == 0 {
		return nil, scim.ErrInvalidValue(opLabel(index) + ": remove selector element selects nothing")
	}
	sort.Slice(keys, func(i, j int) bool { return scim.FoldName(keys[i]) < scim.FoldName(keys[j]) })
	var expr filter.Expr
	for _, k := range keys {
		clause := filter.Compare{Path: filter.Path{Attr: k}, Op: filter.OpEqual, Value: obj[k]}
		if expr == nil {
			expr = clause
		} else {
			expr = filter.AndExpr{L: expr, R: clause}
		}
	}
	return expr, nil
}

// unwrapValueObjects strips the {"value": x} wrapping Entra ID puts
// around simple add/replace values — including the one-element-array
// form [{"$ref": null, "value": "False"}] — whenever the op's path
// resolves to a non-complex target. Complex targets keep their shape:
// there {"value": ...} is a real sub-attribute.
func unwrapValueObjects(ops []Op, rt *scim.ResourceType) {
	for i := range ops {
		op := &ops[i]
		if op.Path == nil || (op.Kind != KindAdd && op.Kind != KindReplace) {
			continue
		}
		attr, sub, ok := resolveTarget(rt, op.Path)
		if !ok {
			continue
		}
		target := attr
		if sub != nil {
			target = sub
		}
		if target.Type == scim.TypeComplex {
			continue
		}
		if target.MultiValued && sub == nil {
			if list, ok := op.Value.([]any); ok {
				for j := range list {
					list[j] = unwrapSimple(list[j])
				}
				continue
			}
		}
		op.Value = unwrapSimple(op.Value)
	}
}

// unwrapSimple unwraps one wrapped simple value: a one-element array
// collapses to its element, and an object whose only non-null member is
// "value" collapses to that member's scalar.
func unwrapSimple(v any) any {
	if list, ok := v.([]any); ok && len(list) == 1 {
		v = list[0]
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return v
	}
	var inner any
	found := false
	for k, member := range obj {
		if member == nil {
			continue
		}
		if scim.FoldName(k) != subAttrValue || found {
			return v
		}
		inner, found = member, true
	}
	if !found {
		return v
	}
	switch inner.(type) {
	case string, bool, json.Number:
		return inner
	default:
		return v
	}
}

// coerceStringPrimitives converts string-encoded booleans and numbers to
// the target attribute's schema type — Entra ID sends active as "False"
// and numeric extension attributes as quoted strings. Unparseable
// strings are left alone for Apply to reject; unwrapValueObjects must
// run first so wrapped scalars are bare by the time they are coerced.
func coerceStringPrimitives(ops []Op, rt *scim.ResourceType) {
	for i := range ops {
		op := &ops[i]
		if op.Kind != KindAdd && op.Kind != KindReplace {
			continue
		}
		if op.Path == nil {
			if obj, ok := op.Value.(map[string]any); ok {
				coercePathless(rt, obj)
			}
			continue
		}
		attr, sub, ok := resolveTarget(rt, op.Path)
		if !ok {
			continue
		}
		if sub != nil {
			op.Value = coerceScalarOrList(sub, op.Value)
			continue
		}
		op.Value = coerceForAttr(attr, op.Value)
	}
}

// coercePathless walks a resource-shaped value object: plain keys
// resolve as core or common attributes, URN keys as extension
// containers.
func coercePathless(rt *scim.ResourceType, obj map[string]any) {
	for k, v := range obj {
		if ext, ok := extensionAttrs(rt, k); ok {
			if inner, ok := v.(map[string]any); ok {
				coerceObject(ext, inner)
			}
			continue
		}
		attr := findAttr(rt.Schema.Attributes, k)
		if attr == nil {
			attr = findAttr(scim.CommonAttributes(), k)
		}
		if attr == nil {
			continue
		}
		obj[k] = coerceForAttr(attr, v)
	}
}

func extensionAttrs(rt *scim.ResourceType, urn string) ([]scim.Attribute, bool) {
	folded := scim.FoldName(urn)
	for i := range rt.Extensions {
		if scim.FoldName(rt.Extensions[i].Schema.ID) == folded {
			return rt.Extensions[i].Schema.Attributes, true
		}
	}
	return nil, false
}

// coerceForAttr coerces a value against one attribute: complex values
// recurse into sub-attributes, multi-valued attributes coerce per
// element.
func coerceForAttr(a *scim.Attribute, v any) any {
	if a.Type == scim.TypeComplex {
		coerceComplexValue(a, v)
		return v
	}
	return coerceScalarOrList(a, v)
}

func coerceComplexValue(a *scim.Attribute, v any) {
	switch t := v.(type) {
	case []any:
		for _, elem := range t {
			if obj, ok := elem.(map[string]any); ok {
				coerceObject(a.SubAttributes, obj)
			}
		}
	case map[string]any:
		coerceObject(a.SubAttributes, t)
	}
}

func coerceObject(attrs []scim.Attribute, obj map[string]any) {
	for k, v := range obj {
		attr := findAttr(attrs, k)
		if attr == nil {
			continue
		}
		obj[k] = coerceForAttr(attr, v)
	}
}

func coerceScalarOrList(a *scim.Attribute, v any) any {
	if list, ok := v.([]any); ok && a.MultiValued {
		for i := range list {
			list[i] = coerceScalar(a, list[i])
		}
		return list
	}
	return coerceScalar(a, v)
}

// coerceScalar converts one string-encoded primitive; anything already
// typed, or not parseable as the target type, passes through untouched.
func coerceScalar(a *scim.Attribute, v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	switch a.Type {
	case scim.TypeBoolean:
		switch scim.FoldName(s) {
		case "true":
			return true
		case "false":
			return false
		}
	case scim.TypeInteger:
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			return json.Number(s)
		}
	case scim.TypeDecimal:
		if _, err := strconv.ParseFloat(s, 64); err == nil {
			return json.Number(s)
		}
	}
	return v
}
