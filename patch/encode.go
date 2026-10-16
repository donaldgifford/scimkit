package patch

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/scimkit/scim"
)

// EncodeOps renders ops as an RFC 7644 §3.5.2 PatchOp message body — the
// write counterpart to Decode. Output is always strict-shaped regardless
// of the Profile the ops were built or decoded under: op values are the
// canonical lowercase keywords, path is omitted only for a pathless
// add/replace, and value is omitted only for a path-only remove. This
// matches the library's tolerant-input/strict-output posture — EncodeOps
// is the output side.
//
// Each Op.Value must already be a JSON-plain value (string, bool,
// float64, json.Number, map[string]any, []any, or nil): it is passed
// straight to encoding/json, not through the scim resource codec, so
// time.Time and scim.ComplexValue forms are not converted here.
func EncodeOps(ops []Op) ([]byte, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("patch: EncodeOps requires at least one operation")
	}
	msg := patchOpMessage{
		Schemas:    []string{scim.MessagePatchOp},
		Operations: make([]operationJSON, 0, len(ops)),
	}
	for i := range ops {
		op, err := encodeOp(&ops[i])
		if err != nil {
			return nil, fmt.Errorf("patch: Operations[%d]: %w", i, err)
		}
		msg.Operations = append(msg.Operations, op)
	}
	return json.Marshal(msg)
}

// patchOpMessage is the PatchOp envelope wire shape; the Operations key
// is capitalized per RFC 7644 §3.5.2.
type patchOpMessage struct {
	Schemas    []string        `json:"schemas"`
	Operations []operationJSON `json:"Operations"`
}

type operationJSON struct {
	Op    string `json:"op"`
	Path  string `json:"path,omitempty"`
	Value any    `json:"value,omitempty"`
}

// encodeOp validates and renders one operation.
func encodeOp(op *Op) (operationJSON, error) {
	if !op.Kind.valid() {
		return operationJSON{}, fmt.Errorf("unknown op kind %q", op.Kind)
	}
	out := operationJSON{Op: string(op.Kind)}
	if op.Path != nil {
		out.Path = op.Path.String()
	}
	switch op.Kind {
	case KindRemove:
		if op.Path == nil {
			return operationJSON{}, fmt.Errorf("remove requires a path")
		}
	case KindAdd, KindReplace:
		if op.Value == nil {
			return operationJSON{}, fmt.Errorf("%s requires a value", op.Kind)
		}
		out.Value = op.Value
	}
	return out, nil
}
