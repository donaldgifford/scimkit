package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

func TestConflictError(t *testing.T) {
	inner := &scim.Error{Status: 412, Detail: "version mismatch"}
	err := &ConflictError{Err: inner}
	assert.Contains(t, err.Error(), "version mismatch")

	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr, "unwraps to the protocol error")
	assert.Equal(t, 412, scimErr.Status)
}

func TestRetryExhaustedError(t *testing.T) {
	inner := &scim.Error{Status: 503, Detail: "down"}
	err := &RetryExhaustedError{Attempts: 3, Last: inner}
	assert.Contains(t, err.Error(), "3 attempts")
	assert.ErrorIs(t, err, inner, "unwraps to the last error")
}

func TestDecodeError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantStatus int
		wantConfl  bool
	}{
		{
			name: "scim error body", status: 400,
			body:       `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"400","scimType":"invalidValue"}`,
			wantStatus: 400,
		},
		{
			name: "412 becomes conflict", status: 412,
			body:       `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"412"}`,
			wantStatus: 412, wantConfl: true,
		},
		{
			name: "non-scim body keeps http status", status: 502,
			body: `<html>bad gateway</html>`, wantStatus: 502,
		},
		{
			name: "empty body keeps http status", status: 500,
			body: ``, wantStatus: 500,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := decodeError(tt.status, []byte(tt.body))
			var scimErr *scim.Error
			require.ErrorAs(t, err, &scimErr)
			assert.Equal(t, tt.wantStatus, scimErr.Status)
			var confl *ConflictError
			assert.Equal(t, tt.wantConfl, errors.As(err, &confl))
		})
	}
}

func TestRealSleeper(t *testing.T) {
	s := realSleeper{}

	t.Run("waits then returns nil", func(t *testing.T) {
		assert.NoError(t, s.sleep(context.Background(), time.Millisecond))
	})

	t.Run("zero duration returns immediately", func(t *testing.T) {
		assert.NoError(t, s.sleep(context.Background(), 0))
	})

	t.Run("cancelled context aborts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.ErrorIs(t, s.sleep(ctx, time.Hour), context.Canceled)
	})
}
