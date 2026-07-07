package patch

import (
	"slices"

	"github.com/donaldgifford/scimkit/scim"
)

// applyReplace implements the RFC 7644 §3.5.2.3 replace table: pathless
// values replace attributes at whole-attribute granularity, a missing
// target attribute falls back to add (spec-mandated, not a Profile
// knob), single-valued complex targets partially merge — named
// sub-attributes replace, unnamed ones remain — multi-valued targets
// replace the whole array, and valuePath targets replace every matching
// element. A valuePath matching nothing is 400 noTarget under strict
// semantics; the ReplaceNoTargetAsAdd knob layers on top.
func (a *applier) applyReplace(op *Op, index int) error {
	if op.Path == nil {
		return a.addPathless(op, index)
	}
	attr, sub, err := a.resolveOpTarget(op, index)
	if err != nil {
		return err
	}
	if op.Path.ValueFilter != nil {
		return a.replaceValuePath(op, attr, sub, index)
	}
	if sub != nil {
		return a.setTracked(subPathText(op.Path), op.Value)
	}
	return a.replaceAttribute(attrPathText(op.Path), attr, op.Value)
}

// replaceAttribute applies whole-attribute replace semantics: arrays are
// replaced wholesale (an empty array unassigns), complex values merge
// partially per §3.5.2.3, simple values are set.
func (a *applier) replaceAttribute(path string, attr *scim.Attribute, v any) error {
	switch {
	case attr.MultiValued:
		coerced, err := a.coerce(path, wrapElements(v))
		if err != nil {
			return err
		}
		return a.setTracked(path, coerced)
	case attr.Type == scim.TypeComplex:
		return a.mergeComplex(path, v)
	default:
		return a.setTracked(path, v)
	}
}

// replaceValuePath replaces every element matching the value filter: a
// sub-attribute path sets that sub per element, a whole-element target
// substitutes the value object for each match.
func (a *applier) replaceValuePath(op *Op, attr, sub *scim.Attribute, index int) error {
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
		if a.profile.ReplaceNoTargetAsAdd {
			return a.synthesizeElement(op, sub, index)
		}
		return scim.ErrNoTarget(opLabel(index) + ": no value matches the filter on " + attr.Name)
	}
	if sub != nil {
		return a.writeToElements(op, sub, list, matches)
	}
	mp, ok := op.Value.(map[string]any)
	if !ok {
		return scim.ErrInvalidValue(opLabel(index) + ": value for complex element must be an object")
	}
	out := slices.Clone(list)
	keep := make(map[int]bool, len(matches))
	for _, i := range matches {
		elem := make(scim.ComplexValue, len(mp))
		for k, v := range mp {
			elem[scim.FoldName(k)] = v
		}
		out[i] = elem
		keep[i] = true
	}
	demoteOthers(out, keep)
	return a.setTracked(attrPathText(op.Path), out)
}
