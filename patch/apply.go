package patch

import (
	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// Apply executes decoded operations sequentially against a working copy
// of res, implementing the RFC 7644 §3.5.2 decision tables. The input
// resource is never mutated: Apply clones it up front, each op observes
// the effects of the ops before it, and the first failing op aborts the
// whole PatchOp (atomicity per §3.5.2) leaving the caller's resource
// untouched. Result.Changed reports whether anything actually changed,
// so idempotent PatchOps can skip meta.version bumps.
//
// The engine mutates only through Resource.Get/Set/Unassign — that is
// what keeps the presence-tracking invariants (empty values are never
// stored, extension schemas stay derived) holding for free.
func Apply(res *scim.Resource, ops []Op, p Profile) (Result, error) {
	a := &applier{res: res.Clone(), rt: res.ResourceType(), profile: p}
	for i := range ops {
		if err := a.apply(&ops[i], i); err != nil {
			return Result{}, err
		}
	}
	return Result{Resource: a.res, Changed: a.changed}, nil
}

type applier struct {
	res     *scim.Resource
	rt      *scim.ResourceType
	profile Profile
	changed bool
}

func (a *applier) apply(op *Op, index int) error {
	switch op.Kind {
	case KindAdd:
		return a.applyAdd(op, index)
	case KindRemove:
		return a.applyRemove(op, index)
	case KindReplace:
		return a.applyReplace(op, index)
	default:
		return scim.ErrInvalidSyntax(opLabel(index) + ": unknown op kind " + string(op.Kind))
	}
}

// resolveOpTarget resolves a pathed op against the schema and runs the
// mutability guard. Undefined paths are ErrInvalidPath: unlike filter
// evaluation's no-value tolerance, a PATCH names a location to modify,
// so a location that cannot exist is a client error.
func (a *applier) resolveOpTarget(op *Op, index int) (attr, sub *scim.Attribute, err error) {
	attr, sub, ok := resolveTarget(a.rt, op.Path)
	if !ok {
		return nil, nil, scim.ErrInvalidPath(opLabel(index) + ": no attribute " + op.Path.String())
	}
	if attr.MultiValued && sub != nil && op.Path.ValueFilter == nil {
		return nil, nil, scim.ErrInvalidPath(
			opLabel(index) + ": sub-attribute on multi-valued " + attr.Name + " requires a value filter")
	}
	if err := a.checkMutable(op, attr, sub, index); err != nil {
		return nil, nil, err
	}
	return attr, sub, nil
}

// checkMutable enforces mutability for a pathed op: readOnly targets
// are never writable, immutable targets only while unassigned (or with
// their current value — RFC 7643 §2.2 match-or-error), and a
// whole-attribute remove of a required attribute is invalid. Resource.Set
// deliberately skips these rules, so the PATCH pipeline is where they
// bind. Immutable sub-attributes behind a value filter are enforced per
// matched element at the write sites, where the element is known.
func (a *applier) checkMutable(op *Op, attr, sub *scim.Attribute, index int) error {
	if attr.Mutability == scim.MutabilityReadOnly {
		return scim.ErrMutability(opLabel(index) + ": attribute " + attr.Name + " is readOnly")
	}
	if sub != nil && sub.Mutability == scim.MutabilityReadOnly {
		return scim.ErrMutability(opLabel(index) + ": sub-attribute " + sub.Name + " is readOnly")
	}
	if attr.Mutability == scim.MutabilityImmutable {
		if _, assigned := a.res.Get(attrPathText(op.Path)); assigned {
			return scim.ErrMutability(
				opLabel(index) + ": attribute " + attr.Name + " is immutable and already assigned")
		}
	}
	if err := a.checkImmutableSubPath(op, sub, index); err != nil {
		return err
	}
	if op.Kind == KindRemove && attr.Required && sub == nil && op.Path.ValueFilter == nil {
		return scim.ErrMutability(opLabel(index) + ": cannot remove required attribute " + attr.Name)
	}
	return nil
}

// checkImmutableSubPath guards a dotted path naming an immutable
// sub-attribute without a value filter (manager.value style): once
// assigned, remove is a modification and a write may only carry the
// current value.
func (a *applier) checkImmutableSubPath(op *Op, sub *scim.Attribute, index int) error {
	if sub == nil || sub.Mutability != scim.MutabilityImmutable || op.Path.ValueFilter != nil {
		return nil
	}
	existing, assigned := a.res.Get(subPathText(op.Path))
	if !assigned {
		return nil
	}
	if op.Kind == KindRemove {
		return errImmutableSub(sub, index)
	}
	coerced, err := a.coerce(subPathText(op.Path), op.Value)
	if err != nil {
		return err
	}
	if !valuesEqual(existing, coerced) {
		return errImmutableSub(sub, index)
	}
	return nil
}

// checkElemImmutable rejects writing a different value to an immutable
// sub-attribute already assigned on one element; re-sending the
// identical value stays a legal no-op.
func (a *applier) checkElemImmutable(op *Op, sub *scim.Attribute, elem scim.ComplexValue, raw any, index int) error {
	if sub.Mutability != scim.MutabilityImmutable {
		return nil
	}
	existing, assigned := elem[scim.FoldName(sub.Name)]
	if !assigned || existing == nil {
		return nil
	}
	coerced, err := a.coerceElemSub(op.Path, sub, raw)
	if err != nil {
		return err
	}
	if !valuesEqual(existing, coerced) {
		return errImmutableSub(sub, index)
	}
	return nil
}

func errImmutableSub(sub *scim.Attribute, index int) *scim.Error {
	return scim.ErrMutability(
		opLabel(index) + ": sub-attribute " + sub.Name + " is immutable and already assigned")
}

// coerceElemSub coerces one sub-attribute value to its stored form
// through a scratch element of the attribute at p.
func (a *applier) coerceElemSub(p *filter.Path, sub *scim.Attribute, raw any) (any, error) {
	scratch := scim.NewResource(a.rt)
	key := scim.FoldName(sub.Name)
	elem := scim.ComplexValue{key: raw}
	if err := scratch.Set(attrPathText(p), scim.MultiComplexValue{elem}); err != nil {
		return nil, err
	}
	v, _ := scratch.Get(attrPathText(p))
	mcv, ok := v.(scim.MultiComplexValue)
	if !ok || len(mcv) == 0 {
		return nil, nil //nolint:nilnil // nil value with nil error means the raw value canonicalizes to unassigned (RFC 7643 §2.5)
	}
	return mcv[0][key], nil
}

// setTracked writes through Resource.Set and folds before/after change
// detection into the applier.
func (a *applier) setTracked(path string, v any) error {
	before, _ := a.res.Get(path)
	if err := a.res.Set(path, v); err != nil {
		return err
	}
	after, _ := a.res.Get(path)
	if !valuesEqual(before, after) {
		a.changed = true
	}
	return nil
}

// coerce round-trips a decoded value through a scratch resource so the
// engine can compare and merge in stored forms only. A nil result means
// the value canonicalizes to unassigned (nil, empty array, empty
// object).
func (a *applier) coerce(path string, v any) (any, error) {
	scratch := scim.NewResource(a.rt)
	if err := scratch.Set(path, v); err != nil {
		return nil, err
	}
	got, _ := scratch.Get(path)
	return got, nil
}

// attrPathText renders the Resource path for a filter.Path's top-level
// attribute (urn:attr), dropping sub-attribute and value filter.
func attrPathText(p *filter.Path) string {
	if p.URN != "" {
		return p.URN + ":" + p.Attr
	}
	return p.Attr
}

// subPathText renders the Resource path including the sub-attribute.
func subPathText(p *filter.Path) string {
	s := attrPathText(p)
	if p.SubAttr != "" {
		s += "." + p.SubAttr
	}
	return s
}

// matchingElements returns the indexes of list elements satisfying the
// path's value filter, by probing each element alone through
// filter.Match — per-element correlation with the exact Table 3
// semantics the filter package already implements.
func (a *applier) matchingElements(p *filter.Path, list scim.MultiComplexValue) ([]int, error) {
	probe := scim.NewResource(a.rt)
	expr := filter.ValuePath{
		Path:   filter.Path{URN: p.URN, Attr: p.Attr},
		Filter: p.ValueFilter,
	}
	var idx []int
	for i := range list {
		if err := probe.Set(attrPathText(p), scim.MultiComplexValue{list[i]}); err != nil {
			return nil, err
		}
		ok, err := filter.Match(probe, expr)
		if err != nil {
			return nil, err
		}
		if ok {
			idx = append(idx, i)
		}
	}
	return idx, nil
}

// getList fetches a multi-valued complex attribute's stored elements;
// ok=false when unassigned.
func (a *applier) getList(p *filter.Path) (scim.MultiComplexValue, bool) {
	v, assigned := a.res.Get(attrPathText(p))
	if !assigned {
		return nil, false
	}
	mcv, ok := v.(scim.MultiComplexValue)
	return mcv, ok
}

// primaryTrue reports whether a stored element carries primary=true.
func primaryTrue(cv scim.ComplexValue) bool {
	p, ok := cv[attrPrimary].(bool)
	return ok && p
}

// demoteOthers clears primary on every element outside keep when any
// kept element carries primary=true (RFC 7643 §2.4: at most one primary;
// the server demotes the previous one rather than erroring). Elements
// are copied before modification — stored values are never mutated in
// place.
func demoteOthers(list []scim.ComplexValue, keep map[int]bool) {
	hasNewPrimary := false
	for i := range list {
		if keep[i] && primaryTrue(list[i]) {
			hasNewPrimary = true
			break
		}
	}
	if !hasNewPrimary {
		return
	}
	for i := range list {
		if keep[i] || !primaryTrue(list[i]) {
			continue
		}
		clone := cloneElement(list[i])
		clone[attrPrimary] = false
		list[i] = clone
	}
}

// attrPrimary is the folded name of the RFC 7643 §2.4 primary
// sub-attribute.
const attrPrimary = "primary"
