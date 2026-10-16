package patch

import (
	"bytes"
	"slices"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

// valuesEqual compares two stored values (the closed set Resource.Get
// returns — see scim/value.go). It backs Changed detection: an op whose
// before and after values compare equal did not modify the resource.
func valuesEqual(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case string, bool, int64, float64, scim.ReferenceValue:
		return a == b
	case time.Time:
		bv, ok := b.(time.Time)
		return ok && av.Equal(bv)
	case []byte:
		bv, ok := b.([]byte)
		return ok && bytes.Equal(av, bv)
	case scim.ComplexValue:
		bv, ok := b.(scim.ComplexValue)
		return ok && complexEqual(av, bv)
	case scim.MultiComplexValue:
		bv, ok := b.(scim.MultiComplexValue)
		return ok && slices.EqualFunc(av, bv, complexEqual)
	default:
		return typedSlicesEqual(a, b)
	}
}

func complexEqual(a, b scim.ComplexValue) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !valuesEqual(av, bv) {
			return false
		}
	}
	return true
}

// typedSlicesEqual covers the multi-valued scalar stored forms.
func typedSlicesEqual(a, b any) bool {
	switch av := a.(type) {
	case []string:
		bv, ok := b.([]string)
		return ok && slices.Equal(av, bv)
	case []bool:
		bv, ok := b.([]bool)
		return ok && slices.Equal(av, bv)
	case []int64:
		bv, ok := b.([]int64)
		return ok && slices.Equal(av, bv)
	case []float64:
		bv, ok := b.([]float64)
		return ok && slices.Equal(av, bv)
	case []time.Time:
		bv, ok := b.([]time.Time)
		return ok && slices.EqualFunc(av, bv, time.Time.Equal)
	case [][]byte:
		bv, ok := b.([][]byte)
		return ok && slices.EqualFunc(av, bv, bytes.Equal)
	case []scim.ReferenceValue:
		bv, ok := b.([]scim.ReferenceValue)
		return ok && slices.Equal(av, bv)
	default:
		return false
	}
}
