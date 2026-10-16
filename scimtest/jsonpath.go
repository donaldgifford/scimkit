package scimtest

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

// lookupPath resolves a dotted attribute path against decoded JSON. It
// greedily matches the longest run of dot-separated tokens against an
// actual map key before falling back to shorter runs, so extension-schema
// keys that themselves contain dots — e.g.
// "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User" — resolve
// correctly. Numeric tokens index into arrays. It returns the value and
// whether the path was present.
func lookupPath(doc any, path string) (any, bool) {
	return resolvePath(doc, strings.Split(path, "."))
}

func resolvePath(cur any, tokens []string) (any, bool) {
	if len(tokens) == 0 {
		return cur, true
	}
	switch node := cur.(type) {
	case map[string]any:
		// Longest-match first, backtracking to shorter prefixes if the
		// remainder does not resolve under the longer key.
		for n := len(tokens); n >= 1; n-- {
			key := strings.Join(tokens[:n], ".")
			v, ok := node[key]
			if !ok {
				continue
			}
			if res, ok := resolvePath(v, tokens[n:]); ok {
				return res, true
			}
		}
		return nil, false
	case []any:
		idx, err := strconv.Atoi(tokens[0])
		if err != nil || idx < 0 || idx >= len(node) {
			return nil, false
		}
		return resolvePath(node[idx], tokens[1:])
	default:
		return nil, false
	}
}

// valueEqual compares an expected assertion value against a decoded JSON
// value, coercing numeric types so an authored int matches JSON's float64.
func valueEqual(want, got any) bool {
	if wf, ok := toFloat(want); ok {
		gf, ok := toFloat(got)
		return ok && wf == gf
	}
	return reflect.DeepEqual(want, got)
}

// toFloat reports the numeric value of v, if it is a number.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
