package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

// withSleeper injects a test clock so retries do not sleep for real. It
// is unexported and test-only, keeping the public Option surface clean.
func withSleeper(s sleeper) Option {
	return func(c *config) { c.sleeper = s }
}

// fakeSleeper records requested delays and never actually waits. A
// non-nil err is returned instead of sleeping, to simulate a cancelled
// context mid-backoff.
type fakeSleeper struct {
	calls []time.Duration
	err   error
}

func (f *fakeSleeper) sleep(_ context.Context, d time.Duration) error {
	f.calls = append(f.calls, d)
	return f.err
}

// scriptedHandler replies with a queued sequence of responses, so a test
// can drive the retry loop through 429/5xx then success.
type scriptedHandler struct {
	responses []scriptedResponse
	calls     atomic.Int32
}

type scriptedResponse struct {
	status     int
	retryAfter string
	body       string
}

func (h *scriptedHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	n := int(h.calls.Add(1)) - 1
	resp := h.responses[len(h.responses)-1]
	if n < len(h.responses) {
		resp = h.responses[n]
	}
	if resp.retryAfter != "" {
		w.Header().Set("Retry-After", resp.retryAfter)
	}
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(resp.status)
	_, _ = w.Write([]byte(resp.body))
}

func newRetryClient(t *testing.T, handler http.Handler, sleep *fakeSleeper, rc RetryConfig) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c, err := New(ts.URL, WithRetry(rc), withSleeper(sleep))
	require.NoError(t, err)
	return c
}

func fastRetry() RetryConfig {
	rc := DefaultRetryConfig()
	rc.MaxAttempts = 4
	return rc
}

func TestRetrySucceedsAfterTransientFailures(t *testing.T) {
	tests := []struct {
		name      string
		responses []scriptedResponse
		wantCalls int32
	}{
		{
			name: "503 then 200",
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable},
				{status: http.StatusOK, body: `{"id":"u1","userName":"a@example.com"}`},
			},
			wantCalls: 2,
		},
		{
			name: "429 with Retry-After seconds then 200",
			responses: []scriptedResponse{
				{status: http.StatusTooManyRequests, retryAfter: "1"},
				{status: http.StatusOK, body: `{"id":"u1","userName":"a@example.com"}`},
			},
			wantCalls: 2,
		},
		{
			name: "two failures then success",
			responses: []scriptedResponse{
				{status: http.StatusBadGateway},
				{status: http.StatusGatewayTimeout},
				{status: http.StatusOK, body: `{"id":"u1","userName":"a@example.com"}`},
			},
			wantCalls: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &scriptedHandler{responses: tt.responses}
			sleep := &fakeSleeper{}
			c := newRetryClient(t, handler, sleep, fastRetry())
			res, err := c.Users().Get(context.Background(), "u1")
			require.NoError(t, err)
			require.NotNil(t, res)
			assert.Equal(t, tt.wantCalls, handler.calls.Load())
			assert.Len(t, sleep.calls, int(tt.wantCalls)-1, "one sleep per retry")
		})
	}
}

func TestRetryHonorsRetryAfterHeader(t *testing.T) {
	handler := &scriptedHandler{responses: []scriptedResponse{
		{status: http.StatusTooManyRequests, retryAfter: "5"},
		{status: http.StatusOK, body: `{"id":"u1","userName":"a@example.com"}`},
	}}
	sleep := &fakeSleeper{}
	c := newRetryClient(t, handler, sleep, fastRetry())
	_, err := c.Users().Get(context.Background(), "u1")
	require.NoError(t, err)
	require.Len(t, sleep.calls, 1)
	assert.Equal(t, 5*time.Second, sleep.calls[0], "Retry-After overrides computed backoff")
}

func TestRetryExhaustion(t *testing.T) {
	handler := &scriptedHandler{responses: []scriptedResponse{
		{status: http.StatusServiceUnavailable, body: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"503","detail":"down"}`},
	}}
	sleep := &fakeSleeper{}
	rc := DefaultRetryConfig()
	rc.MaxAttempts = 3
	c := newRetryClient(t, handler, sleep, rc)

	_, err := c.Users().Get(context.Background(), "u1")
	require.Error(t, err)
	var exhausted *RetryExhaustedError
	require.ErrorAs(t, err, &exhausted, "exhaustion is typed")
	assert.Equal(t, 3, exhausted.Attempts)
	assert.Equal(t, int32(3), handler.calls.Load(), "tried MaxAttempts times")
	assert.Len(t, sleep.calls, 2, "slept between attempts, not after the last")

	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr, "the final protocol error is reachable")
	assert.Equal(t, 503, scimErr.Status)
}

func TestRetryContextCancellation(t *testing.T) {
	handler := &scriptedHandler{responses: []scriptedResponse{
		{status: http.StatusServiceUnavailable},
	}}
	sleep := &fakeSleeper{err: context.Canceled}
	c := newRetryClient(t, handler, sleep, fastRetry())

	_, err := c.Users().Get(context.Background(), "u1")
	require.ErrorIs(t, err, context.Canceled, "cancellation during backoff aborts, not exhaustion")
	var exhausted *RetryExhaustedError
	assert.NotErrorAs(t, err, &exhausted, "cancellation is not RetryExhaustedError")
}

func TestNonRetryableStatusReturnsImmediately(t *testing.T) {
	handler := &scriptedHandler{responses: []scriptedResponse{
		{status: http.StatusNotFound, body: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"404"}`},
	}}
	sleep := &fakeSleeper{}
	c := newRetryClient(t, handler, sleep, fastRetry())

	_, err := c.Users().Get(context.Background(), "missing")
	require.Error(t, err)
	var scimErr *scim.Error
	require.ErrorAs(t, err, &scimErr)
	assert.Equal(t, 404, scimErr.Status)
	assert.Equal(t, int32(1), handler.calls.Load(), "404 does not retry")
	assert.Empty(t, sleep.calls)
}
