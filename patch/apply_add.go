package patch

import (
	"bytes"
	"slices"
	"sort"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

// applyAdd implements the RFC 7644 §3.5.2.1 add table: pathless values
// merge into the resource, single-valued targets are replaced,
// single-valued complex targets partially merge, multi-valued targets
// append (ignoring duplicates, so re-adds are Changed=false no-ops), and
// valuePath targets merge into every matching element.
func (a *applier) applyAdd(op *Op, index int) error {
	if op.Path == nil {
		return a.addPathless(op, index)
	}
	attr, sub, err := a.resolveOpTarget(op, index)
	if err != nil {
		return err
	}
	if op.Path.ValueFilter != nil {
		return a.addValuePath(op, attr, sub, index)
	}
	if sub != nil {
		return a.setTracked(subPathText(op.Path), op.Value)
	}
	return a.addToAttribute(attrPathText(op.Path), attr, op.Value)
}

// addToAttribute applies add semantics for one attribute, shared by
// pathed adds and pathless per-key merges.
func (a *applier) addToAttribute(path string, attr *scim.Attribute, v any) error {
	switch {
	case attr.MultiValued:
		return a.addMulti(path, v)
	case attr.Type == scim.TypeComplex:
		return a.mergeComplex(path, v)
	default:
		return a.setTracked(path, v)
	}
}

// addMulti appends coerced elements, skipping ones already present. An
// empty incoming value must never reach Resource.Set: Set canonicalizes
// empty to unassigned, which would delete the existing elements instead
// of no-opping.
func (a *applier) addMulti(path string, v any) error {
	incoming, err := a.coerce(path, wrapElements(v))
	if err != nil {
		return err
	}
	if incoming == nil {
		return nil
	}
	existing, assigned := a.res.Get(path)
	if !assigned {
		return a.setTracked(path, incoming)
	}
	merged, added := appendNew(existing, incoming)
	if !added {
		return nil
	}
	return a.setTracked(path, merged)
}

// wrapElements lifts a bare element into a one-element array; a
// non-array add to a multi-valued attribute has only that meaning.
func wrapElements(v any) any {
	if _, ok := v.([]any); ok {
		return v
	}
	return []any{v}
}

// mergeComplex partially merges a value into a single-valued complex
// attribute: named sub-attributes are set, unnamed ones remain. Add and
// replace share this per §3.5.2.1 and §3.5.2.3.
func (a *applier) mergeComplex(path string, v any) error {
	coerced, err := a.coerce(path, v)
	if err != nil {
		return err
	}
	if coerced == nil {
		return nil
	}
	incoming, ok := coerced.(scim.ComplexValue)
	if !ok {
		return a.setTracked(path, coerced)
	}
	existing, assigned := a.res.Get(path)
	if !assigned {
		return a.setTracked(path, incoming)
	}
	current, ok := existing.(scim.ComplexValue)
	if !ok {
		return a.setTracked(path, incoming)
	}
	merged := cloneElement(current)
	for k, sv := range incoming {
		merged[k] = sv
	}
	return a.setTracked(path, merged)
}

// addValuePath merges the op value into every element matching the
// value filter. No matching element is strict noTarget; the
// SynthesizeAddValuePath knob's manufacture branch layers on top.
func (a *applier) addValuePath(op *Op, attr, sub *scim.Attribute, index int) error {
	if !attr.MultiValued || attr.Type != scim.TypeComplex {
		return scim.ErrInvalidPath(
			opLabel(index) + ": value filter on non-multi-valued attribute " + attr.Name)
	}
	list, _ := a.getList(op.Path)
	matches, err := a.matchingElements(op.Path, list)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		if a.profile.SynthesizeAddValuePath {
			return a.synthesizeElement(op, sub, index)
		}
		return scim.ErrNoTarget(opLabel(index) + ": no value matches the filter on " + attr.Name)
	}
	return a.writeToElements(op, attr, sub, list, matches, index)
}

// writeToElements applies an op's value to the selected elements of a
// stored list — the shared tail of valuePath add and replace: sub-attr
// targets set that sub per element, whole-element targets merge the
// value object in, and a newly set primary demotes the others. An
// immutable sub-attribute already assigned on an element accepts only
// its current value; readOnly keys inside a merge object are skipped
// like the pathless merge skips readOnly attributes.
func (a *applier) writeToElements(
	op *Op, attr, sub *scim.Attribute, list scim.MultiComplexValue, matches []int, index int,
) error {
	var mp map[string]any
	if sub == nil {
		var ok bool
		mp, ok = op.Value.(map[string]any)
		if !ok {
			return scim.ErrInvalidValue(opLabel(index) + ": value for complex element must be an object")
		}
	}
	out := slices.Clone(list)
	keep := make(map[int]bool, len(matches))
	for _, i := range matches {
		elem := cloneElement(out[i])
		if sub != nil {
			if err := a.checkElemImmutable(op, sub, elem, op.Value, index); err != nil {
				return err
			}
			elem[scim.FoldName(sub.Name)] = op.Value
		} else if err := a.mergeElemKeys(op, attr, elem, mp, index); err != nil {
			return err
		}
		out[i] = elem
		keep[i] = true
	}
	demoteOthers(out, keep)
	return a.setTracked(attrPathText(op.Path), out)
}

// mergeElemKeys merges a value object into one existing element with
// per-key mutability checks.
func (a *applier) mergeElemKeys(op *Op, attr *scim.Attribute, elem scim.ComplexValue, mp map[string]any, index int) error {
	for k, v := range mp {
		subAttr := findAttr(attr.SubAttributes, k)
		if subAttr != nil {
			if subAttr.Mutability == scim.MutabilityReadOnly {
				continue
			}
			if err := a.checkElemImmutable(op, subAttr, elem, v, index); err != nil {
				return err
			}
		}
		elem[scim.FoldName(k)] = v
	}
	return nil
}

// addPathless merges a resource-shaped value object per §3.5.2.1: each
// key gets that attribute's add semantics. Unknown keys and readOnly
// attributes are skipped silently — the same tolerance the codec's
// decode applies to client-supplied readOnly content — while the
// derived schemas and meta attributes are never patch targets. Keys are
// visited in folded order so multi-key failures are deterministic.
func (a *applier) addPathless(op *Op, index int) error {
	obj, ok := op.Value.(map[string]any)
	if !ok {
		return scim.ErrInvalidValue(opLabel(index) + ": pathless " + string(op.Kind) + " requires an object value")
	}
	for _, k := range sortedKeys(obj) {
		if err := a.applyPathlessKey(op, k, obj[k], index); err != nil {
			return err
		}
	}
	return nil
}

// applyPathlessKey dispatches one pathless-merge key: extension URNs
// recurse into the container, plain keys resolve against the core and
// common schemas.
func (a *applier) applyPathlessKey(op *Op, key string, v any, index int) error {
	folded := scim.FoldName(key)
	if folded == "schemas" || folded == "meta" {
		return nil
	}
	if ext, isExt := extensionAttrs(a.rt, key); isExt {
		return a.applyPathlessExtension(op, key, ext, v, index)
	}
	attr := findAttr(a.rt.Schema.Attributes, key)
	if attr == nil {
		attr = findAttr(scim.CommonAttributes(), key)
	}
	if attr == nil {
		return nil
	}
	return a.applyPathlessAttr(op, key, attr, v, index)
}

func (a *applier) applyPathlessExtension(op *Op, urn string, ext []scim.Attribute, v any, index int) error {
	obj, ok := v.(map[string]any)
	if !ok {
		return scim.ErrInvalidValue(opLabel(index) + ": extension " + urn + " value must be an object")
	}
	for _, k := range sortedKeys(obj) {
		attr := findAttr(ext, k)
		if attr == nil {
			continue
		}
		if err := a.applyPathlessAttr(op, urn+":"+k, attr, obj[k], index); err != nil {
			return err
		}
	}
	return nil
}

// applyPathlessAttr guards mutability for one pathless-merge key and
// applies the op's per-attribute semantics: add merges (§3.5.2.1),
// replace overwrites whole attributes (§3.5.2.3).
func (a *applier) applyPathlessAttr(op *Op, path string, attr *scim.Attribute, v any, index int) error {
	if attr.Mutability == scim.MutabilityReadOnly {
		return nil
	}
	if attr.Mutability == scim.MutabilityImmutable {
		if _, assigned := a.res.Get(path); assigned {
			return scim.ErrMutability(
				opLabel(index) + ": attribute " + attr.Name + " is immutable and already assigned")
		}
	}
	if op.Kind == KindReplace {
		return a.replaceAttribute(path, attr, v)
	}
	return a.addToAttribute(path, attr, v)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return scim.FoldName(keys[i]) < scim.FoldName(keys[j]) })
	return keys
}

// cloneElement shallow-copies one stored element before modification;
// stored values are never mutated in place.
func cloneElement(cv scim.ComplexValue) scim.ComplexValue {
	out := make(scim.ComplexValue, len(cv))
	for k, v := range cv {
		out[k] = v
	}
	return out
}

// appendNew appends incoming stored elements not already present,
// reporting whether anything was added. Multi-valued complex elements
// deduplicate by their "value" sub-attribute when the incoming element
// has one — an IdP re-adding a group member sends only the member id,
// while the stored element may carry display and $ref — falling back to
// whole-element equality otherwise.
func appendNew(existing, incoming any) (any, bool) {
	switch ex := existing.(type) {
	case scim.MultiComplexValue:
		inc, ok := incoming.(scim.MultiComplexValue)
		if !ok {
			return existing, false
		}
		return appendComplexElements(ex, inc)
	case []string:
		return appendComparable(ex, incoming)
	case []bool:
		return appendComparable(ex, incoming)
	case []int64:
		return appendComparable(ex, incoming)
	case []float64:
		return appendComparable(ex, incoming)
	case []scim.ReferenceValue:
		return appendComparable(ex, incoming)
	case []time.Time:
		return appendFunc(ex, incoming, time.Time.Equal)
	case [][]byte:
		return appendFunc(ex, incoming, bytes.Equal)
	default:
		return existing, false
	}
}

func appendComparable[T comparable](existing []T, incoming any) (any, bool) {
	inc, ok := incoming.([]T)
	if !ok {
		return existing, false
	}
	return appendFunc(existing, inc, func(a, b T) bool { return a == b })
}

func appendFunc[T any](existing []T, incoming any, eq func(T, T) bool) (any, bool) {
	inc, ok := incoming.([]T)
	if !ok {
		return existing, false
	}
	out := slices.Clone(existing)
	added := false
	for _, e := range inc {
		if slices.ContainsFunc(out, func(x T) bool { return eq(x, e) }) {
			continue
		}
		out = append(out, e)
		added = true
	}
	if !added {
		return existing, false
	}
	return out, true
}

// appendComplexElements appends non-duplicate elements and demotes
// previous primaries when a new element carries primary=true.
func appendComplexElements(existing, incoming scim.MultiComplexValue) (any, bool) {
	out := slices.Clone(existing)
	newIdx := make(map[int]bool)
	added := false
	for _, inc := range incoming {
		if hasDuplicateElement(out, inc) {
			continue
		}
		out = append(out, inc)
		newIdx[len(out)-1] = true
		added = true
	}
	if !added {
		return existing, false
	}
	demoteOthers(out, newIdx)
	return out, true
}

func hasDuplicateElement(list scim.MultiComplexValue, elem scim.ComplexValue) bool {
	if id, ok := elem[subAttrValue]; ok {
		return list.IndexFunc(func(e scim.ComplexValue) bool {
			return valuesEqual(e[subAttrValue], id)
		}) >= 0
	}
	return list.IndexFunc(func(e scim.ComplexValue) bool {
		return complexEqual(e, elem)
	}) >= 0
}
