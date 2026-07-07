package patch

// Profile bundles the compatibility knobs deciding how far Decode and
// Apply bend strict RFC 7644 PATCH semantics to accept documented IdP
// behavior. The zero value is fully strict: every knob off, exact RFC
// grammar and semantics. Tolerance is input-side only — normalized
// operations and applied resources are always spec-shaped.
type Profile struct {
	// CaseInsensitiveOps folds capitalized op values ("Add", "Replace")
	// to canonical lowercase. Entra ID sends title-cased op values in
	// provisioning traffic even though RFC 7644 §3.5.2 defines them
	// lowercase.
	CaseInsensitiveOps bool

	// CoerceStringPrimitives converts string-encoded booleans and
	// numbers ("False", "701984") to the target attribute's schema type,
	// and unwraps the {"value": x} object shape Entra ID wraps simple
	// add/replace values in. Entra sends active=False as a quoted string
	// and wraps single values as one-element complex objects.
	CoerceStringPrimitives bool

	// ExpandPathlessReplace expands a pathless replace whose value
	// object uses flattened dotted keys ("name.givenName") or
	// full-URN-qualified keys into per-attribute pathed operations.
	// Entra ID flattens sub-attributes into dotted keys on pathless
	// replace instead of sending nested objects.
	ExpandPathlessReplace bool

	// RemoveValueAsSelector treats a remove operation that carries an
	// op-level value array as a member selector, rewriting it to
	// valuePath-qualified removes. Okta removes group members by sending
	// remove with value:[{"value": id}] and no value filter in the path.
	RemoveValueAsSelector bool

	// ReplaceNoTargetAsAdd makes a replace whose valuePath filter
	// matches no element behave as an add instead of failing. Strict RFC
	// 7644 §3.5.2.3 requires HTTP 400 with scimType noTarget.
	ReplaceNoTargetAsAdd bool

	// SynthesizeAddValuePath manufactures a new element from an add
	// targeting a valuePath that matches nothing, seeding the element
	// from the filter's eq clauses. Only eq-only conjunction filters
	// qualify; anything else is rejected at decode. Entra ID adds
	// addresses[type eq "work"].locality style paths for elements that
	// do not exist yet.
	SynthesizeAddValuePath bool
}
