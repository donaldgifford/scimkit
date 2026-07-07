package patch

import (
	"slices"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// synthesizeElement manufactures the element a valuePath op expected to
// find, seeding sub-attributes from the filter's eq clauses and merging
// the op's value on top — the SynthesizeAddValuePath and
// ReplaceNoTargetAsAdd tail when no element matched. Only eq-only
// conjunction filters describe a constructible element; anything else
// stays the strict noTarget error.
func (a *applier) synthesizeElement(op *Op, sub *scim.Attribute, index int) error {
	seed, ok := seedFromFilter(op.Path.ValueFilter)
	if !ok {
		return scim.ErrNoTarget(opLabel(index) + ": no value matches the filter on " + op.Path.Attr +
			", and only eq-only conjunction filters can seed a new element")
	}
	if sub != nil {
		seed[scim.FoldName(sub.Name)] = op.Value
	} else {
		mp, isMap := op.Value.(map[string]any)
		if !isMap {
			return scim.ErrInvalidValue(opLabel(index) + ": value for complex element must be an object")
		}
		for k, v := range mp {
			seed[scim.FoldName(k)] = v
		}
	}
	list, _ := a.getList(op.Path)
	out := slices.Clone(list)
	out = append(out, scim.ComplexValue(seed))
	demoteOthers(out, map[int]bool{len(out) - 1: true})
	return a.setTracked(attrPathText(op.Path), out)
}

// seedFromFilter extracts {subAttr: literal} pairs from an eq-only
// conjunction. ok=false means the filter cannot describe one element:
// any operator other than eq, any qualified or nested path, or two eq
// clauses disagreeing on the same sub-attribute.
func seedFromFilter(e filter.Expr) (map[string]any, bool) {
	out := make(map[string]any)
	if !collectEqClauses(e, out) {
		return nil, false
	}
	return out, true
}

func collectEqClauses(e filter.Expr, out map[string]any) bool {
	switch t := e.(type) {
	case filter.Compare:
		if t.Op != filter.OpEqual || t.Path.URN != "" || t.Path.SubAttr != "" || t.Path.ValueFilter != nil {
			return false
		}
		key := scim.FoldName(t.Path.Attr)
		if existing, dup := out[key]; dup && existing != t.Value {
			return false
		}
		if t.Value == nil {
			// eq null constrains the element to lack the sub-attribute;
			// seeding nothing satisfies it.
			return true
		}
		out[key] = t.Value
		return true
	case filter.AndExpr:
		return collectEqClauses(t.L, out) && collectEqClauses(t.R, out)
	default:
		return false
	}
}
