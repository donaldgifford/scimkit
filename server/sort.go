package server

import (
	"bytes"
	"sort"
	"strings"
	"time"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// SortResources orders list in place by a sortBy path per RFC 7644
// §3.4.2.3: comparison is type-aware from the attribute's schema
// definition, strings honor caseExact, resources without a sort key
// always sort last regardless of direction, and a multi-valued
// attribute contributes its primary element's value — or its first
// element's — optionally narrowed by a value filter in the sort path.
// It is exported for Store implementations that sort natively; the
// server uses it itself on the scan-and-Match fallback path.
func SortResources(list []*scim.Resource, rt *scim.ResourceType, by *filter.Path, ascending bool) {
	if by == nil {
		return
	}
	attr, sub, ok := resolveAttr(rt, by)
	if !ok {
		return
	}
	target := attr
	if sub != nil {
		target = sub
	}
	keys := make(map[*scim.Resource]any, len(list))
	for _, r := range list {
		keys[r] = sortKey(r, rt, by, attr, sub)
	}
	sort.SliceStable(list, func(i, j int) bool {
		return sortLess(keys[list[i]], keys[list[j]], target, ascending)
	})
}

// sortKey extracts one resource's sort key as a stored value, or nil
// when the resource has no value at the sort path.
func sortKey(r *scim.Resource, rt *scim.ResourceType, by *filter.Path, attr, sub *scim.Attribute) any {
	if attr.MultiValued && attr.Type == scim.TypeComplex {
		return multiSortKey(r, rt, by, sub)
	}
	v, ok := r.Get(fullPathText(by))
	if !ok {
		return nil
	}
	return v
}

// multiSortKey picks the contributing element of a multi-valued complex
// attribute: the first value-filter match when the path carries one,
// else the primary element, else the first — then projects the named
// sub-attribute (or "value" when none is named).
func multiSortKey(r *scim.Resource, rt *scim.ResourceType, by *filter.Path, sub *scim.Attribute) any {
	v, ok := r.Get(attrPathText(by))
	if !ok {
		return nil
	}
	list, ok := v.(scim.MultiComplexValue)
	if !ok || len(list) == 0 {
		return nil
	}
	elem := pickElement(rt, by, list)
	if elem == nil {
		return nil
	}
	key := "value"
	if sub != nil {
		key = scim.FoldName(sub.Name)
	}
	return elem[key]
}

func pickElement(rt *scim.ResourceType, by *filter.Path, list scim.MultiComplexValue) scim.ComplexValue {
	if by.ValueFilter != nil {
		if i := matchIndex(rt, by, list); i >= 0 {
			return list[i]
		}
		return nil
	}
	if i := list.IndexFunc(func(cv scim.ComplexValue) bool {
		p, ok := cv["primary"].(bool)
		return ok && p
	}); i >= 0 {
		return list[i]
	}
	return list[0]
}

// matchIndex probes each element alone through filter.Match, the same
// per-element correlation the patch engine uses.
func matchIndex(rt *scim.ResourceType, by *filter.Path, list scim.MultiComplexValue) int {
	probe := scim.NewResource(rt)
	expr := filter.ValuePath{
		Path:   filter.Path{URN: by.URN, Attr: by.Attr},
		Filter: by.ValueFilter,
	}
	for i := range list {
		if err := probe.Set(attrPathText(by), scim.MultiComplexValue{list[i]}); err != nil {
			return -1
		}
		ok, err := filter.Match(probe, expr)
		if err != nil {
			return -1
		}
		if ok {
			return i
		}
	}
	return -1
}

// sortLess orders two sort keys: nil (missing) sorts last regardless of
// direction, otherwise the type-aware comparison honors ascending.
func sortLess(a, b any, target *scim.Attribute, ascending bool) bool {
	if a == nil || b == nil {
		return a != nil && b == nil
	}
	c := compareKeys(a, b, target)
	if !ascending {
		c = -c
	}
	return c < 0
}

// compareKeys is the type-aware three-way comparison over stored forms.
func compareKeys(a, b any, target *scim.Attribute) int {
	switch av := a.(type) {
	case string:
		return compareStrings(av, b, target.CaseExact)
	case scim.ReferenceValue:
		bv, ok := b.(scim.ReferenceValue)
		if !ok {
			return 0
		}
		return strings.Compare(string(av), string(bv))
	case bool:
		bv, ok := b.(bool)
		if !ok {
			return 0
		}
		return boolCompare(av, bv)
	case int64:
		return compareNumbers(float64(av), b)
	case float64:
		return compareNumbers(av, b)
	case time.Time:
		bv, ok := b.(time.Time)
		if !ok {
			return 0
		}
		return av.Compare(bv)
	case []byte:
		bv, ok := b.([]byte)
		if !ok {
			return 0
		}
		return bytes.Compare(av, bv)
	default:
		return 0
	}
}

func compareStrings(a string, b any, caseExact bool) int {
	bv, ok := b.(string)
	if !ok {
		return 0
	}
	if caseExact {
		return strings.Compare(a, bv)
	}
	return strings.Compare(scim.FoldName(a), scim.FoldName(bv))
}

func boolCompare(a, b bool) int {
	switch {
	case a == b:
		return 0
	case !a:
		return -1
	default:
		return 1
	}
}

func compareNumbers(a float64, b any) int {
	var bv float64
	switch t := b.(type) {
	case int64:
		bv = float64(t)
	case float64:
		bv = t
	default:
		return 0
	}
	switch {
	case a < bv:
		return -1
	case a > bv:
		return 1
	default:
		return 0
	}
}
