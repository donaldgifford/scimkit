package scim

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// scimType keyword values for HTTP 400-class errors (RFC 7644 §3.12
// Table 9).
const (
	ScimTypeInvalidFilter = "invalidFilter"
	ScimTypeTooMany       = "tooMany"
	ScimTypeUniqueness    = "uniqueness"
	ScimTypeMutability    = "mutability"
	ScimTypeInvalidSyntax = "invalidSyntax"
	ScimTypeInvalidPath   = "invalidPath"
	ScimTypeNoTarget      = "noTarget"
	ScimTypeInvalidValue  = "invalidValue"
	ScimTypeInvalidVers   = "invalidVers"
	ScimTypeSensitive     = "sensitive"
)

// Error is the SCIM protocol error (RFC 7644 §3.12), used on both halves of
// the library: servers marshal it to the urn:…:api:messages:2.0:Error wire
// body and clients unmarshal error responses back into it, so
// errors.As(err, &scimErr) works identically everywhere.
type Error struct {
	// Status is the HTTP status code.
	Status int
	// ScimType is a Table 9 keyword, or empty when none applies.
	ScimType string
	// Detail is a human-readable description of the failure.
	Detail string
}

// Is reports whether target is a *Error with the same Status and, when
// target carries a ScimType, the same ScimType. Detail never
// participates, so errors.Is(err, scim.ErrNotFound("")) matches any
// 404 and errors.Is(err, scim.ErrNoTarget("")) matches any noTarget,
// however deeply wrapped.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok || e.Status != t.Status {
		return false
	}
	return t.ScimType == "" || e.ScimType == t.ScimType
}

// IsStatus reports whether err is, or wraps, a *Error with the given HTTP
// status, whatever its ScimType.
func IsStatus(err error, status int) bool {
	return errors.Is(err, &Error{Status: status})
}

// Error implements the error interface.
func (e *Error) Error() string {
	msg := fmt.Sprintf("scim: status %d", e.Status)
	if e.ScimType != "" {
		msg += " " + e.ScimType
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// errorJSON is the RFC 7644 §3.12 wire shape. The spec's examples carry
// status as a JSON string; some implementations emit a number, so decoding
// accepts both while encoding always emits a string.
type errorJSON struct {
	Schemas  []string        `json:"schemas"`
	Status   json.RawMessage `json:"status"`
	ScimType string          `json:"scimType,omitempty"`
	Detail   string          `json:"detail,omitempty"`
}

// MarshalJSON renders the RFC 7644 §3.12 error body with status as a JSON
// string, per the spec's examples.
func (e *Error) MarshalJSON() ([]byte, error) {
	status, err := json.Marshal(strconv.Itoa(e.Status))
	if err != nil {
		return nil, err
	}
	return json.Marshal(errorJSON{
		Schemas:  []string{MessageError},
		Status:   status,
		ScimType: e.ScimType,
		Detail:   e.Detail,
	})
}

// UnmarshalJSON decodes an RFC 7644 §3.12 error body, accepting status as
// either a JSON string or a JSON number.
func (e *Error) UnmarshalJSON(data []byte) error {
	var j errorJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("parse scim error: %w", err)
	}
	status, err := parseErrorStatus(j.Status)
	if err != nil {
		return err
	}
	*e = Error{Status: status, ScimType: j.ScimType, Detail: j.Detail}
	return nil
}

// parseErrorStatus decodes the status field, which the spec's examples give
// as a JSON string but lax implementations emit as a number.
func parseErrorStatus(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return 0, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("parse scim error status %q: %w", s, err)
		}
		return n, nil
	}
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("parse scim error status %s: %w", raw, err)
	}
	return n, nil
}

func newError(status int, scimType, detail string) *Error {
	return &Error{Status: status, ScimType: scimType, Detail: detail}
}

// ErrInvalidFilter reports a filter whose syntax or attribute/operator
// combination is invalid (Table 9, HTTP 400).
func ErrInvalidFilter(detail string) *Error {
	return newError(400, ScimTypeInvalidFilter, detail)
}

// ErrTooMany reports a filter yielding more results than the server is
// willing to process (Table 9, HTTP 400).
func ErrTooMany(detail string) *Error { return newError(400, ScimTypeTooMany, detail) }

// ErrUniqueness reports attribute values that are already in use or
// reserved (Table 9, HTTP 409).
func ErrUniqueness(detail string) *Error { return newError(409, ScimTypeUniqueness, detail) }

// ErrMutability reports a modification incompatible with an attribute's
// mutability or current state (Table 9, HTTP 400).
func ErrMutability(detail string) *Error { return newError(400, ScimTypeMutability, detail) }

// ErrInvalidSyntax reports a request body that does not conform to the
// request schema (Table 9, HTTP 400).
func ErrInvalidSyntax(detail string) *Error { return newError(400, ScimTypeInvalidSyntax, detail) }

// ErrInvalidPath reports a malformed PATCH path (Table 9, HTTP 400).
func ErrInvalidPath(detail string) *Error { return newError(400, ScimTypeInvalidPath, detail) }

// ErrNoTarget reports a PATCH path that yields no attribute or value to
// operate on (Table 9, HTTP 400).
func ErrNoTarget(detail string) *Error { return newError(400, ScimTypeNoTarget, detail) }

// ErrInvalidValue reports a missing required value or one incompatible with
// the operation, attribute type, or resource schema (Table 9, HTTP 400).
func ErrInvalidValue(detail string) *Error { return newError(400, ScimTypeInvalidValue, detail) }

// ErrInvalidVers reports an unsupported SCIM protocol version path
// (Table 9, HTTP 400; see RFC 7644 §3.13 — this is not the ETag-mismatch
// error, which is ErrPreconditionFailed).
func ErrInvalidVers(detail string) *Error { return newError(400, ScimTypeInvalidVers, detail) }

// ErrSensitive reports sensitive information passed in a request URI; use
// POST /.search instead (Table 9, HTTP 400).
func ErrSensitive(detail string) *Error { return newError(400, ScimTypeSensitive, detail) }

// ErrNotFound reports a missing resource (RFC 7644 Table 8, HTTP 404).
func ErrNotFound(detail string) *Error { return newError(404, "", detail) }

// ErrPreconditionFailed reports a failed If-Match precondition: the resource
// changed on the server (RFC 7644 §3.14, HTTP 412).
func ErrPreconditionFailed(detail string) *Error { return newError(412, "", detail) }
