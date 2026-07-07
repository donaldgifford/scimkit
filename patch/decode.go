package patch

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// Decode parses a PatchOp message body (RFC 7644 §3.5.2) into normalized
// operations. The envelope must carry the PatchOp schema URN and a
// non-empty Operations array; each operation is shape-checked (op value,
// path grammar, value presence per kind) and its path parsed with
// filter.ParsePath. Normalization per the Profile happens between those
// steps: knobs that rewrite raw shapes run before path parsing, knobs
// that need the target attribute's schema type run after, resolved
// against rt. Errors are *scim.Error values: ErrInvalidSyntax for
// envelope and op-shape violations, ErrInvalidPath for path grammar
// violations, ErrInvalidValue for missing or kind-incompatible values.
func Decode(body []byte, rt *scim.ResourceType, p Profile) ([]Op, error) {
	raws, err := parseEnvelope(body)
	if err != nil {
		return nil, err
	}
	ops := make([]Op, 0, len(raws))
	for i := range raws {
		op, err := buildOp(&raws[i], i, p)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	_ = rt // schema-directed normalization passes attach here
	return ops, nil
}

// rawOp is one Operations element before kind validation and path
// parsing, with presence tracked separately from the zero value.
type rawOp struct {
	op       string
	path     string
	value    any
	hasPath  bool
	hasValue bool
}

// parseEnvelope validates the message envelope and splits Operations
// into raw ops. Envelope and operation keys match case-insensitively
// (RFC 7643 §2.1); unknown keys are ignored, mirroring the scim codec's
// tolerant-decode posture.
func parseEnvelope(body []byte) ([]rawOp, error) {
	doc, err := decodeJSON(body)
	if err != nil {
		return nil, err
	}
	obj, ok := doc.(map[string]any)
	if !ok {
		return nil, scim.ErrInvalidSyntax("PatchOp body must be a JSON object")
	}
	var schemas, operations any
	var hasSchemas, hasOperations bool
	for k, v := range obj {
		switch scim.FoldName(k) {
		case "schemas":
			schemas, hasSchemas = v, true
		case "operations":
			operations, hasOperations = v, true
		}
	}
	if err := checkSchemas(schemas, hasSchemas); err != nil {
		return nil, err
	}
	if !hasOperations {
		return nil, scim.ErrInvalidSyntax("PatchOp requires an Operations array")
	}
	list, ok := operations.([]any)
	if !ok {
		return nil, scim.ErrInvalidSyntax("Operations must be an array")
	}
	if len(list) == 0 {
		return nil, scim.ErrInvalidSyntax("Operations must not be empty")
	}
	out := make([]rawOp, 0, len(list))
	for i, elem := range list {
		ro, err := parseRawOp(elem, i)
		if err != nil {
			return nil, err
		}
		out = append(out, ro)
	}
	return out, nil
}

// decodeJSON decodes the body with json.Number preserved, rejecting
// trailing data after the message object.
func decodeJSON(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, scim.ErrInvalidSyntax("parse PatchOp body: " + err.Error())
	}
	if dec.More() {
		return nil, scim.ErrInvalidSyntax("trailing data after PatchOp body")
	}
	return doc, nil
}

// checkSchemas requires the PatchOp message URN (fold-compared, since URN
// matching is case-insensitive); extra URNs are ignored.
func checkSchemas(schemas any, present bool) error {
	if !present {
		return scim.ErrInvalidSyntax("PatchOp requires a schemas array")
	}
	list, ok := schemas.([]any)
	if !ok {
		return scim.ErrInvalidSyntax("schemas must be an array")
	}
	want := scim.FoldName(scim.MessagePatchOp)
	for _, elem := range list {
		if s, ok := elem.(string); ok && scim.FoldName(s) == want {
			return nil
		}
	}
	return scim.ErrInvalidSyntax("schemas must include " + scim.MessagePatchOp)
}

// parseRawOp shape-checks one Operations element: it must be an object,
// op must be a string and is required, path must be a string when
// present.
func parseRawOp(elem any, index int) (rawOp, error) {
	obj, ok := elem.(map[string]any)
	if !ok {
		return rawOp{}, scim.ErrInvalidSyntax(opLabel(index) + " must be a JSON object")
	}
	var ro rawOp
	var hasOp bool
	for k, v := range obj {
		switch scim.FoldName(k) {
		case "op":
			s, ok := v.(string)
			if !ok {
				return rawOp{}, scim.ErrInvalidSyntax(opLabel(index) + ": op must be a string")
			}
			ro.op, hasOp = s, true
		case "path":
			s, ok := v.(string)
			if !ok {
				return rawOp{}, scim.ErrInvalidPath(opLabel(index) + ": path must be a string")
			}
			ro.path, ro.hasPath = s, true
		case "value":
			ro.value, ro.hasValue = v, true
		}
	}
	if !hasOp {
		return rawOp{}, scim.ErrInvalidSyntax(opLabel(index) + " requires an op")
	}
	return ro, nil
}

// buildOp validates one raw op's kind and value shape and parses its
// path.
func buildOp(ro *rawOp, index int, p Profile) (Op, error) {
	kind, err := opKind(ro.op, index, p)
	if err != nil {
		return Op{}, err
	}
	op := Op{Kind: kind}
	if ro.hasPath {
		parsed, err := filter.ParsePath(ro.path)
		if err != nil {
			return Op{}, err
		}
		op.Path = &parsed
	}
	switch kind {
	case KindAdd, KindReplace:
		if !ro.hasValue || ro.value == nil {
			return Op{}, scim.ErrInvalidValue(opLabel(index) + ": " + string(kind) + " requires a value")
		}
		op.Value = ro.value
	case KindRemove:
		// Removing selected members via an op-level value array is the
		// RemoveValueAsSelector rewrite; without it a value on remove is
		// rejected rather than silently removing more than the client
		// selected.
		if ro.hasValue {
			return Op{}, scim.ErrInvalidValue(opLabel(index) + ": remove does not take a value")
		}
	}
	return op, nil
}

// opKind validates the wire op value, exact lowercase per RFC 7644
// §3.5.2 unless the Profile folds case.
func opKind(op string, index int, p Profile) (Kind, error) {
	candidate := op
	if p.CaseInsensitiveOps {
		candidate = scim.FoldName(op)
	}
	switch Kind(candidate) {
	case KindAdd, KindRemove, KindReplace:
		return Kind(candidate), nil
	default:
		return "", scim.ErrInvalidSyntax(opLabel(index) + ": unknown op " + strconv.Quote(op))
	}
}

func opLabel(index int) string {
	return "Operations[" + strconv.Itoa(index) + "]"
}
