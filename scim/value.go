package scim

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// ReferenceValue is the stored form of a SCIM reference attribute
// (RFC 7643 §2.3.7). It is a distinct type because reference and string are
// both JSON strings and would otherwise be indistinguishable in Go.
type ReferenceValue string

// ComplexValue is one instance of a SCIM complex attribute: sub-attribute
// values keyed by case-folded sub-attribute name. Declared casing is
// re-derived from the schema at serialization time.
type ComplexValue map[string]any

// MultiComplexValue is the stored form of a multi-valued complex attribute.
type MultiComplexValue []ComplexValue

// IndexFunc returns the index of the first element satisfying pred, or -1.
// The filter evaluator and PATCH engine share it for per-element valuePath
// correlation.
func (m MultiComplexValue) IndexFunc(pred func(ComplexValue) bool) int {
	for i, cv := range m {
		if pred(cv) {
			return i
		}
	}
	return -1
}

// Stored value forms, by attribute type (the closed set Resource.Get
// returns):
//
//	string     string          multi: []string
//	boolean    bool            multi: []bool
//	decimal    float64         multi: []float64
//	integer    int64           multi: []int64
//	dateTime   time.Time       multi: []time.Time
//	binary     []byte          multi: [][]byte
//	reference  ReferenceValue  multi: []ReferenceValue
//	complex    ComplexValue    multi: MultiComplexValue

// coerceValue converts v into the canonical stored form for the attribute,
// accepting both natural Go values and decoded-JSON values (string, bool,
// json.Number, map[string]any, []any). A nil result with nil error means the
// value canonicalizes to unassigned (nil input or an empty multi-value).
func coerceValue(a *Attribute, v any) (any, error) {
	if v == nil {
		return nil, nil //nolint:nilnil // nil value with nil error means unassigned (RFC 7643 §2.5)
	}
	if a.MultiValued {
		return coerceMulti(a, v)
	}
	return coerceSingle(a, v)
}

func coerceSingle(a *Attribute, v any) (any, error) {
	switch a.Type {
	case TypeString:
		return coerceString(v)
	case TypeBoolean:
		return coerceBool(v)
	case TypeDecimal:
		return coerceDecimal(v)
	case TypeInteger:
		return coerceInteger(v)
	case TypeDateTime:
		return coerceDateTime(v)
	case TypeBinary:
		return coerceBinary(v)
	case TypeReference:
		return coerceReference(v)
	case TypeComplex:
		cv, err := coerceComplex(a, v)
		if err != nil || cv == nil {
			// Return an untyped nil: a nil ComplexValue boxed in any would
			// not compare equal to nil.
			return nil, err
		}
		return cv, nil
	default:
		return nil, fmt.Errorf("attribute %q: unknown type %q", a.Name, a.Type)
	}
}

func coerceString(v any) (any, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	default:
		return nil, typeMismatch("string", v)
	}
}

func coerceBool(v any) (any, error) {
	switch b := v.(type) {
	case bool:
		return b, nil
	default:
		return nil, typeMismatch("boolean", v)
	}
}

func coerceDecimal(v any) (any, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return nil, typeMismatch("decimal", v)
		}
		return f, nil
	default:
		return nil, typeMismatch("decimal", v)
	}
}

func coerceInteger(v any) (any, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return nil, typeMismatch("integer", v)
		}
		return i, nil
	case float64:
		i := int64(n)
		if float64(i) != n {
			return nil, typeMismatch("integer", v)
		}
		return i, nil
	default:
		return nil, typeMismatch("integer", v)
	}
}

func coerceDateTime(v any) (any, error) {
	switch d := v.(type) {
	case time.Time:
		return d, nil
	case string:
		t, err := time.Parse(time.RFC3339, d)
		if err != nil {
			return nil, fmt.Errorf("invalid dateTime %q: %w", d, err)
		}
		return t, nil
	default:
		return nil, typeMismatch("dateTime", v)
	}
}

func coerceBinary(v any) (any, error) {
	switch b := v.(type) {
	case []byte:
		// Clone so later mutation of the caller's slice cannot corrupt
		// stored resource state.
		return slices.Clone(b), nil
	case string:
		raw, err := base64.StdEncoding.DecodeString(b)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 binary value: %w", err)
		}
		return raw, nil
	default:
		return nil, typeMismatch("binary", v)
	}
}

func coerceReference(v any) (any, error) {
	switch r := v.(type) {
	case ReferenceValue:
		return r, nil
	case string:
		return ReferenceValue(r), nil
	default:
		return nil, typeMismatch("reference", v)
	}
}

// coerceComplex folds sub-attribute keys and coerces sub-values against the
// schema. Unknown sub-attributes are dropped (RFC 7644 §3.12 allows
// tolerating unrecognized parameters; strict rejection is a decode-profile
// concern layered above). Complex sub-attributes are rejected: complex
// values nest at most one level (RFC 7643 §2.3.8).
func coerceComplex(a *Attribute, v any) (ComplexValue, error) {
	var src map[string]any
	switch m := v.(type) {
	case ComplexValue:
		src = m
	case map[string]any:
		src = m
	default:
		return nil, typeMismatch("complex", v)
	}
	out := make(ComplexValue, len(src))
	for key, sub := range src {
		subAttr := findAttribute(a.SubAttributes, key)
		if subAttr == nil {
			continue
		}
		if subAttr.Type == TypeComplex {
			return nil, fmt.Errorf("sub-attribute %q: complex within complex exceeds max depth", subAttr.Name)
		}
		if sub == nil {
			continue
		}
		cv, err := coerceValue(subAttr, sub)
		if err != nil {
			return nil, fmt.Errorf("sub-attribute %q: %w", subAttr.Name, err)
		}
		if cv != nil {
			out[foldName(subAttr.Name)] = cv
		}
	}
	if len(out) == 0 {
		return nil, nil //nolint:nilnil // nil value with nil error means unassigned (RFC 7643 §2.5)
	}
	return out, nil
}

// coerceMulti converts v to the typed slice form for a multi-valued
// attribute. Empty inputs canonicalize to nil (unassigned). At most one
// element of a multi-valued complex attribute may carry primary=true
// (RFC 7643 §2.4).
func coerceMulti(a *Attribute, v any) (any, error) {
	items, err := multiItems(v)
	if err != nil || items == nil {
		return nil, err
	}
	if a.Type == TypeComplex {
		return coerceMultiComplex(a, items)
	}
	return coerceMultiScalar(a, items)
}

// multiItems normalizes the accepted multi-value input shapes to []any.
func multiItems(v any) ([]any, error) {
	var items []any
	switch s := v.(type) {
	case []any:
		items = s
	case []string:
		items = make([]any, len(s))
		for i, e := range s {
			items[i] = e
		}
	case []ComplexValue:
		items = make([]any, len(s))
		for i, e := range s {
			items[i] = e
		}
	case MultiComplexValue:
		items = make([]any, len(s))
		for i, e := range s {
			items[i] = e
		}
	case []map[string]any:
		items = make([]any, len(s))
		for i, e := range s {
			items[i] = e
		}
	default:
		return nil, typeMismatch("multi-valued", v)
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items, nil
}

func coerceMultiComplex(a *Attribute, items []any) (any, error) {
	out := make(MultiComplexValue, 0, len(items))
	primaries := 0
	for i, item := range items {
		cv, err := coerceComplex(a, item)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		if cv == nil {
			continue
		}
		if p, ok := cv[foldName("primary")].(bool); ok && p {
			primaries++
		}
		out = append(out, cv)
	}
	if primaries > 1 {
		return nil, fmt.Errorf("attribute %q: more than one element has primary set", a.Name)
	}
	if len(out) == 0 {
		return nil, nil //nolint:nilnil // nil value with nil error means unassigned (RFC 7643 §2.5)
	}
	return out, nil
}

func coerceMultiScalar(a *Attribute, items []any) (any, error) {
	switch a.Type {
	case TypeString:
		return coerceSlice[string](a, items)
	case TypeBoolean:
		return coerceSlice[bool](a, items)
	case TypeDecimal:
		return coerceSlice[float64](a, items)
	case TypeInteger:
		return coerceSlice[int64](a, items)
	case TypeDateTime:
		return coerceSlice[time.Time](a, items)
	case TypeBinary:
		return coerceSlice[[]byte](a, items)
	case TypeReference:
		return coerceSlice[ReferenceValue](a, items)
	default:
		return nil, fmt.Errorf("attribute %q: unknown type %q", a.Name, a.Type)
	}
}

func coerceSlice[T any](a *Attribute, items []any) (any, error) {
	out := make([]T, 0, len(items))
	for i, item := range items {
		if item == nil {
			continue
		}
		cv, err := coerceSingle(a, item)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		tv, ok := cv.(T)
		if !ok {
			return nil, fmt.Errorf("element %d: %w", i, typeMismatch(string(a.Type), cv))
		}
		out = append(out, tv)
	}
	if len(out) == 0 {
		return nil, nil //nolint:nilnil // nil value with nil error means unassigned (RFC 7643 §2.5)
	}
	return out, nil
}

// findAttribute returns the attribute whose name folds equal to name, or
// nil. It is the single lookup used for case-insensitive resolution.
func findAttribute(attrs []Attribute, name string) *Attribute {
	folded := foldName(name)
	for i := range attrs {
		if foldName(attrs[i].Name) == folded {
			return &attrs[i]
		}
	}
	return nil
}

func typeMismatch(want string, got any) error {
	return fmt.Errorf("expected %s value, got %T", want, got)
}
