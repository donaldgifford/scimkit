package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/donaldgifford/scimkit/scim"
)

// ErrAmbiguousLookup is returned by Lookup when the filter matches more
// than one resource.
var ErrAmbiguousLookup = errors.New("scimkit client: filter matched more than one resource")

// ErrPaginationStalled is yielded by List when the server returns the
// same page for consecutive startIndex values — a target that ignores
// pagination parameters and would otherwise be walked forever.
var ErrPaginationStalled = errors.New("scimkit client: pagination stalled")

// ConflictError reports a failed If-Match precondition (HTTP 412): the
// resource changed on the server since the version the write asserted. It
// wraps the protocol error, so errors.As(err, &scimErr) still works.
type ConflictError struct {
	Err *scim.Error
}

func (e *ConflictError) Error() string { return e.Err.Error() }
func (e *ConflictError) Unwrap() error { return e.Err }

// RetryExhaustedError reports that the retry policy gave up. Last is the
// final attempt's error (usually a *scim.Error or a wrapped network
// error) and is exposed via Unwrap.
type RetryExhaustedError struct {
	Attempts int
	Last     error
}

func (e *RetryExhaustedError) Error() string {
	return fmt.Sprintf("scimkit client: retries exhausted after %d attempts: %v", e.Attempts, e.Last)
}

func (e *RetryExhaustedError) Unwrap() error { return e.Last }

// decodeError turns a non-2xx response body into a *scim.Error. A body
// that is not a SCIM error document still yields a *scim.Error carrying
// the HTTP status, so callers can always errors.As their way to one. A
// 412 is additionally wrapped in *ConflictError.
func decodeError(status int, body []byte) error {
	var scimErr scim.Error
	if err := json.Unmarshal(body, &scimErr); err != nil || scimErr.Status == 0 {
		scimErr = scim.Error{Status: status, Detail: nonSCIMDetail(body)}
	}
	if scimErr.Status == http.StatusPreconditionFailed {
		return &ConflictError{Err: &scimErr}
	}
	return &scimErr
}

// nonSCIMDetail renders a short, safe detail for a non-SCIM error body.
func nonSCIMDetail(body []byte) string {
	const limit = 200
	trimmed := body
	if len(trimmed) > limit {
		trimmed = trimmed[:limit]
	}
	if len(trimmed) == 0 {
		return "non-SCIM error response"
	}
	return "non-SCIM error response: " + string(trimmed)
}
