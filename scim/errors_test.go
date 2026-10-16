package scim

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorConstructors(t *testing.T) {
	tests := []struct {
		name         string
		err          *Error
		wantStatus   int
		wantScimType string
	}{
		{name: "invalid filter", err: ErrInvalidFilter("d"), wantStatus: 400, wantScimType: "invalidFilter"},
		{name: "too many", err: ErrTooMany("d"), wantStatus: 400, wantScimType: "tooMany"},
		{name: "uniqueness", err: ErrUniqueness("d"), wantStatus: 409, wantScimType: "uniqueness"},
		{name: "mutability", err: ErrMutability("d"), wantStatus: 400, wantScimType: "mutability"},
		{name: "invalid syntax", err: ErrInvalidSyntax("d"), wantStatus: 400, wantScimType: "invalidSyntax"},
		{name: "invalid path", err: ErrInvalidPath("d"), wantStatus: 400, wantScimType: "invalidPath"},
		{name: "no target", err: ErrNoTarget("d"), wantStatus: 400, wantScimType: "noTarget"},
		{name: "invalid value", err: ErrInvalidValue("d"), wantStatus: 400, wantScimType: "invalidValue"},
		{name: "invalid vers", err: ErrInvalidVers("d"), wantStatus: 400, wantScimType: "invalidVers"},
		{name: "sensitive", err: ErrSensitive("d"), wantStatus: 400, wantScimType: "sensitive"},
		{name: "not found", err: ErrNotFound("d"), wantStatus: 404, wantScimType: ""},
		{name: "precondition failed", err: ErrPreconditionFailed("d"), wantStatus: 412, wantScimType: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantStatus, tt.err.Status)
			assert.Equal(t, tt.wantScimType, tt.err.ScimType)
			assert.Equal(t, "d", tt.err.Detail)
		})
	}
}

func TestErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "status type and detail",
			err:  ErrInvalidFilter("bad operator"),
			want: "scim: status 400 invalidFilter: bad operator",
		},
		{
			name: "no scim type",
			err:  ErrNotFound("user 42"),
			want: "scim: status 404: user 42",
		},
		{
			name: "no detail",
			err:  &Error{Status: 500},
			want: "scim: status 500",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestErrorWireRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
	}{
		{name: "with scim type", err: ErrUniqueness("userName taken")},
		{name: "without scim type", err: ErrNotFound("nope")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.err)
			require.NoError(t, err)

			// Status must be a JSON string and the Error message URN present.
			var raw map[string]any
			require.NoError(t, json.Unmarshal(data, &raw))
			assert.IsType(t, "", raw["status"])
			assert.Equal(t, []any{MessageError}, raw["schemas"])

			var back Error
			require.NoError(t, json.Unmarshal(data, &back))
			assert.Equal(t, *tt.err, back)
		})
	}
}

func TestErrorUnmarshalStatusForms(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantErr    bool
	}{
		{
			name:       "string status per spec examples",
			body:       `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"404"}`,
			wantStatus: 404,
		},
		{
			name:       "integer status from lax implementations",
			body:       `{"status":404,"detail":"gone"}`,
			wantStatus: 404,
		},
		{
			name:    "non-numeric string status",
			body:    `{"status":"abc"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			body:    `{`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e Error
			err := json.Unmarshal([]byte(tt.body), &e)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, e.Status)
		})
	}
}

func TestErrorWorksWithErrorsAs(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantHit bool
	}{
		{name: "direct", err: ErrNoTarget("x"), wantHit: true},
		{name: "wrapped", err: fmt.Errorf("applying patch: %w", ErrNoTarget("x")), wantHit: true},
		{name: "unrelated", err: errors.New("boom"), wantHit: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var scimErr *Error
			assert.Equal(t, tt.wantHit, errors.As(tt.err, &scimErr))
			if tt.wantHit {
				assert.Equal(t, 400, scimErr.Status)
			}
		})
	}
}

func TestErrorIs(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{name: "same status and type", err: ErrNoTarget("a"), target: ErrNoTarget("b"), want: true},
		{name: "empty target type matches any type", err: ErrNoTarget("a"), target: &Error{Status: 400}, want: true},
		{name: "different type", err: ErrNoTarget("a"), target: ErrInvalidPath("a"), want: false},
		{name: "different status", err: ErrNotFound("a"), target: ErrPreconditionFailed("a"), want: false},
		{name: "typed target against untyped error", err: ErrNotFound("a"), target: ErrNoTarget("a"), want: false},
		{name: "wrapped", err: fmt.Errorf("apply: %w", ErrMutability("x")), target: ErrMutability(""), want: true},
		{name: "deeply wrapped", err: fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", ErrNotFound("x"))), target: ErrNotFound(""), want: true},
		{name: "non-scim target", err: ErrNotFound("a"), target: errors.New("boom"), want: false},
		{name: "non-scim error", err: errors.New("boom"), target: ErrNotFound(""), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, errors.Is(tt.err, tt.target))
		})
	}
}

func TestIsStatus(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		want   bool
	}{
		{name: "direct hit", err: ErrNotFound("x"), status: 404, want: true},
		{name: "typed error by status only", err: ErrUniqueness("x"), status: 409, want: true},
		{name: "wrapped", err: fmt.Errorf("lookup: %w", ErrPreconditionFailed("x")), status: 412, want: true},
		{name: "miss", err: ErrNotFound("x"), status: 400, want: false},
		{name: "nil", err: nil, status: 404, want: false},
		{name: "plain error", err: errors.New("boom"), status: 500, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsStatus(tt.err, tt.status))
		})
	}
}
