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
	// qualify; eligibility is checked at apply time when no element
	// matches, and anything else falls through to the strict noTarget
	// error. Entra ID adds
	// addresses[type eq "work"].locality style paths for elements that
	// do not exist yet.
	SynthesizeAddValuePath bool
}

// The built-in profiles. Strict is exact RFC 7644; Default accepts
// shape deviations that carry unambiguous intent but never remaps RFC
// error semantics; the IdP profiles layer documented per-vendor
// deviations on top of Default.
var (
	// Strict is exact RFC 7644 §3.5.2 semantics: every knob off. Use it
	// for conformance testing and for clients that must never mask a
	// provider bug.
	Strict Profile

	// Default is Postel mode — tolerant input, strict output. It
	// accepts the widely observed shape deviations (title-cased ops,
	// string-encoded primitives with {"value": x} wrapping, flattened
	// pathless-replace keys, remove-with-value member selectors)
	// because each has exactly one possible meaning. It keeps the RFC's
	// error semantics: a replace or add whose valuePath filter matches
	// nothing still fails with noTarget.
	Default = Profile{
		CaseInsensitiveOps:     true,
		CoerceStringPrimitives: true,
		ExpandPathlessReplace:  true,
		RemoveValueAsSelector:  true,
	}

	// Entra is Default plus Microsoft Entra ID's documented semantic
	// deviations: Entra provisioning issues replace and add operations
	// against valuePath targets that do not exist yet (for example
	// addresses[type eq "work"].locality on a user with no addresses)
	// and expects the element to be created, where strict RFC 7644
	// §3.5.2.3 requires 400 noTarget.
	Entra = Profile{
		CaseInsensitiveOps:     true,
		CoerceStringPrimitives: true,
		ExpandPathlessReplace:  true,
		RemoveValueAsSelector:  true,
		ReplaceNoTargetAsAdd:   true,
		SynthesizeAddValuePath: true,
	}

	// Okta matches Default: Okta's one grammar deviation — removing
	// group members via an op-level value array instead of a valuePath
	// filter — is already part of Default's shape tolerance. It exists
	// as a distinct profile so deployments pin vendor intent and future
	// Okta-specific deviations land without call-site changes.
	Okta = Default
)
