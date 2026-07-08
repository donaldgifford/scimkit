package scimtest

import "encoding/json"

// Scenario is a named sequence of SCIM requests replayed against a target,
// each with expectations on the response. Steps run in order; a variable
// captured from one step's response substitutes into later steps.
type Scenario struct {
	Name        string
	Description string
	Steps       []Step
}

// Step is one request in a scenario. Path and Body may contain
// text/template actions ("{{.var}}") resolved against captured variables
// before the request is sent; corpus-sourced bodies carry none, so
// substitution is a safe no-op for them.
type Step struct {
	// Name labels the step in reports.
	Name string
	// Method is the HTTP method.
	Method string
	// Path is the request path relative to the target base URL, e.g.
	// "/Users" or "/Users/{{.uid}}".
	Path string
	// Headers are set on the request; Content-Type defaults to
	// application/scim+json when a body is present and none is given.
	Headers map[string]string
	// Body is the raw request body, or nil for a bodyless request.
	Body json.RawMessage
	// Capture extracts values from the response for later steps.
	Capture []Capture
	// Expect is asserted against the response.
	Expect Expectation
}

// Capture binds a value read from a step's response body to a variable
// name, making it available to subsequent steps' templates.
type Capture struct {
	// Var is the variable name, referenced as "{{.Var}}".
	Var string
	// Path is the dotted response path to read (see lookupPath).
	Path string
}

// Expectation asserts a step's response. A zero Status is invalid — every
// step must pin an exact status; ScimType, Headers, and Body assertions are
// checked only when set.
type Expectation struct {
	// Status is the required HTTP status code.
	Status int
	// ScimType, when set, must match the response body's scimType field.
	ScimType string
	// Headers, when set, are matched exactly by key.
	Headers map[string]string
	// Body assertions are checked against the decoded response.
	Body []BodyAssertion
}

// BodyAssertion checks one path in a response body. Its fields mirror
// idpcorpus.PathAssertion so corpus expectations translate directly.
type BodyAssertion struct {
	// Path is the dotted response path (see lookupPath).
	Path string
	// Value is the expected value for a scalar path.
	Value any
	// Unassigned expects the path to be absent.
	Unassigned bool
	// Len expects an array of this length.
	Len *int
}
