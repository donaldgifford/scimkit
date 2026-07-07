package patch

import (
	"slices"

	"github.com/donaldgifford/scimkit/scim"
)

// applyRemove implements the RFC 7644 §3.5.2.2 remove table: no path is
// noTarget, whole attributes unassign, valuePath filters remove the
// matching elements (unassigning the attribute when none remain), and a
// filtered sub-attribute path unassigns that sub on matching elements.
// Removing something already absent is a Changed=false no-op — IdPs
// routinely re-send removes, and the RFC only mandates noTarget for the
// missing-path case.
func (a *applier) applyRemove(op *Op, index int) error {
	if op.Path == nil {
		return scim.ErrNoTarget(opLabel(index) + ": remove requires a path")
	}
	attr, sub, err := a.resolveOpTarget(op, index)
	if err != nil {
		return err
	}
	if op.Path.ValueFilter != nil {
		return a.removeValuePath(op, attr, sub, index)
	}
	if sub != nil {
		return a.setTracked(subPathText(op.Path), nil)
	}
	return a.setTracked(attrPathText(op.Path), nil)
}

// removeValuePath drops matching elements, or with a sub-attribute path
// unassigns that sub on matching elements (dropping elements that end up
// empty). Survivors go back through Set, whose empty-canonicalization
// unassigns the attribute when the last element goes — the
// presence-tracking model makes remove-last-element correct for free.
func (a *applier) removeValuePath(op *Op, attr, sub *scim.Attribute, index int) error {
	if !attr.MultiValued || attr.Type != scim.TypeComplex {
		return scim.ErrInvalidPath(
			opLabel(index) + ": value filter on non-multi-valued attribute " + attr.Name)
	}
	list, assigned := a.getList(op.Path)
	if !assigned {
		return nil
	}
	matches, err := a.matchingElements(op.Path, list)
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}
	if sub != nil {
		return a.removeSubFromElements(op, sub, list, matches, index)
	}
	if attr.Required && len(matches) == len(list) {
		return scim.ErrMutability(
			opLabel(index) + ": cannot remove every value of required attribute " + attr.Name)
	}
	matched := make(map[int]bool, len(matches))
	for _, i := range matches {
		matched[i] = true
	}
	survivors := make([]any, 0, len(list)-len(matches))
	for i := range list {
		if !matched[i] {
			survivors = append(survivors, list[i])
		}
	}
	return a.setTracked(attrPathText(op.Path), survivors)
}

// removeSubFromElements unassigns one sub-attribute on the matched
// elements. An immutable sub already assigned cannot be removed —
// unassigning is a modification of a set immutable value.
func (a *applier) removeSubFromElements(op *Op, sub *scim.Attribute, list scim.MultiComplexValue, matches []int, index int) error {
	out := slices.Clone(list)
	key := scim.FoldName(sub.Name)
	for _, i := range matches {
		if sub.Mutability == scim.MutabilityImmutable {
			if v, ok := out[i][key]; ok && v != nil {
				return errImmutableSub(sub, index)
			}
		}
		elem := cloneElement(out[i])
		delete(elem, key)
		out[i] = elem
	}
	// Set drops elements that emptied and unassigns the attribute when
	// none survive.
	return a.setTracked(attrPathText(op.Path), out)
}
