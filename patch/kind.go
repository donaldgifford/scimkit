package patch

// Kind is a PATCH operation type (RFC 7644 §3.5.2), stored in canonical
// lowercase. Decode validates the wire value exactly; the
// Profile.CaseInsensitiveOps knob folds capitalized IdP spellings before
// validation.
type Kind string

// Operation kinds.
const (
	KindAdd     Kind = "add"
	KindRemove  Kind = "remove"
	KindReplace Kind = "replace"
)

// valid reports whether k is one of the three canonical operation kinds.
func (k Kind) valid() bool {
	switch k {
	case KindAdd, KindRemove, KindReplace:
		return true
	default:
		return false
	}
}
