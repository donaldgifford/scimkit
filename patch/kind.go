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
