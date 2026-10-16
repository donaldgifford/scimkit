package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

const (
	userBody = `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"id":"u1","userName":"a@example.com"}`
	spcBody  = `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"],` +
		`"patch":{"supported":true},"filter":{"supported":true,"maxResults":200},"etag":{"supported":true}}`
	errBody503 = `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"503","detail":"down"}`
	errBody401 = `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"401","detail":"no token"}`
)

func newUserResource(t *testing.T) *scim.Resource {
	t.Helper()
	rt := scim.UserResourceType()
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("userName", "a@example.com"))
	return res
}

// op is one client operation driven through the retry loop, keyed by
// the HTTP method it issues.
type op func(ctx context.Context, c *Client) error

func opCreate(t *testing.T) op {
	t.Helper()
	return func(ctx context.Context, c *Client) error {
		_, err := c.Users().Create(ctx, newUserResource(t))
		return err
	}
}

func opGet(ctx context.Context, c *Client) error {
	_, err := c.Users().Get(ctx, "u1")
	return err
}

func opPatch(ctx context.Context, c *Client) error {
	_, err := c.Users().Patch(ctx, "u1", []patch.Op{{
		Kind: patch.KindReplace, Path: &filter.Path{Attr: "active"}, Value: false,
	}})
	return err
}

func opDelete(ctx context.Context, c *Client) error {
	return c.Users().Delete(ctx, "u1")
}

func TestRetryMethodGating(t *testing.T) {
	tests := []struct {
		name          string
		op            op
		responses     []scriptedResponse
		nonIdempotent bool
		wantCalls     int32
		wantStatus    int  // asserted via *scim.Error when > 0
		wantExhausted bool // RetryExhaustedError expected
		wantErr       bool
	}{
		{
			name: "POST 503 is not retried and surfaces the protocol error",
			op:   opCreate(t),
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable, body: errBody503},
				{status: http.StatusCreated, body: userBody},
			},
			wantCalls: 1, wantStatus: 503, wantErr: true,
		},
		{
			name: "POST 429 is retried: the server did not process it",
			op:   opCreate(t),
			responses: []scriptedResponse{
				{status: http.StatusTooManyRequests, retryAfter: "1"},
				{status: http.StatusCreated, body: userBody},
			},
			wantCalls: 2,
		},
		{
			name: "PATCH 502 is not retried",
			op:   opPatch,
			responses: []scriptedResponse{
				{status: http.StatusBadGateway},
				{status: http.StatusOK, body: userBody},
			},
			wantCalls: 1, wantStatus: 502, wantErr: true,
		},
		{
			name: "GET 503 is retried as before",
			op:   opGet,
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable},
				{status: http.StatusOK, body: userBody},
			},
			wantCalls: 2,
		},
		{
			name: "DELETE 503 is retried: idempotent",
			op:   opDelete,
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable},
				{status: http.StatusNoContent},
			},
			wantCalls: 2,
		},
		{
			name: "RetryNonIdempotent restores POST retry",
			op:   opCreate(t),
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable},
				{status: http.StatusCreated, body: userBody},
			},
			nonIdempotent: true,
			wantCalls:     2,
		},
		{
			name: "POST 503 forever with RetryNonIdempotent exhausts",
			op:   opCreate(t),
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable, body: errBody503},
			},
			nonIdempotent: true,
			wantCalls:     4, wantExhausted: true, wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &scriptedHandler{responses: tt.responses}
			sleep := &fakeSleeper{}
			rc := fastRetry()
			rc.RetryNonIdempotent = tt.nonIdempotent
			ts := httptest.NewServer(handler)
			t.Cleanup(ts.Close)
			// Fallback off: Patch would otherwise probe /ServiceProviderConfig
			// first and consume a scripted response.
			c, err := New(ts.URL, WithRetry(rc), withSleeper(sleep), WithPatchFallback(false))
			require.NoError(t, err)

			err = tt.op(context.Background(), c)
			if !tt.wantErr {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			assert.Equal(t, tt.wantCalls, handler.calls.Load(), "attempt count")
			if tt.wantStatus > 0 {
				scimErr, ok := errors.AsType[*scim.Error](err)
				require.True(t, ok, "protocol error reachable: %v", err)
				assert.Equal(t, tt.wantStatus, scimErr.Status)
			}
			_, exhausted := errors.AsType[*RetryExhaustedError](err)
			assert.Equal(t, tt.wantExhausted, exhausted, "RetryExhaustedError presence")
		})
	}
}

func TestRetryNetworkErrorGating(t *testing.T) {
	tests := []struct {
		name          string
		op            op
		nonIdempotent bool
		wantExhausted bool
		wantNotRetry  bool
	}{
		{name: "POST network error is returned without retry", op: opCreate(t), wantNotRetry: true},
		{name: "GET network error is retried then exhausted", op: opGet, wantExhausted: true},
		{name: "POST network error retried under RetryNonIdempotent", op: opCreate(t), nonIdempotent: true, wantExhausted: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A server that is already closed yields a connection error.
			ts := httptest.NewServer(http.NotFoundHandler())
			ts.Close()
			sleep := &fakeSleeper{}
			rc := fastRetry()
			rc.MaxAttempts = 2
			rc.RetryNonIdempotent = tt.nonIdempotent
			c, err := New(ts.URL, WithRetry(rc), withSleeper(sleep))
			require.NoError(t, err)

			err = tt.op(context.Background(), c)
			require.Error(t, err)
			_, exhausted := errors.AsType[*RetryExhaustedError](err)
			assert.Equal(t, tt.wantExhausted, exhausted)
			if tt.wantNotRetry {
				assert.Contains(t, err.Error(), "not retried")
				assert.Empty(t, sleep.calls, "no backoff for a non-retried request")
			} else {
				assert.Len(t, sleep.calls, 1)
			}
		})
	}
}

// headerRecorder captures the Authorization header of each request and
// replies with the scripted sequence.
type headerRecorder struct {
	scriptedHandler
	mu    sync.Mutex
	auths []string
}

func (h *headerRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.auths = append(h.auths, r.Header.Get("Authorization"))
	h.mu.Unlock()
	h.scriptedHandler.ServeHTTP(w, r)
}

func TestTokenSource(t *testing.T) {
	tests := []struct {
		name      string
		opt       func(calls *atomic.Int32) Option
		responses []scriptedResponse
		wantAuths []string
		wantErr   string
	}{
		{
			name: "source is consulted on every attempt",
			opt: func(calls *atomic.Int32) Option {
				return WithTokenSource(func(context.Context) (string, error) {
					return fmt.Sprintf("tok-%d", calls.Add(1)), nil
				})
			},
			responses: []scriptedResponse{
				{status: http.StatusServiceUnavailable},
				{status: http.StatusOK, body: userBody},
			},
			wantAuths: []string{"Bearer tok-1", "Bearer tok-2"},
		},
		{
			name:      "constant bearer token",
			opt:       func(*atomic.Int32) Option { return WithBearerToken("fixed") },
			responses: []scriptedResponse{{status: http.StatusOK, body: userBody}},
			wantAuths: []string{"Bearer fixed"},
		},
		{
			name:      "no source sends no header",
			opt:       func(*atomic.Int32) Option { return func(*config) {} },
			responses: []scriptedResponse{{status: http.StatusOK, body: userBody}},
			wantAuths: []string{""},
		},
		{
			name: "source error aborts before any request",
			opt: func(*atomic.Int32) Option {
				return WithTokenSource(func(context.Context) (string, error) {
					return "", errors.New("vault sealed")
				})
			},
			responses: []scriptedResponse{{status: http.StatusOK, body: userBody}},
			wantAuths: nil,
			wantErr:   "token source: vault sealed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &headerRecorder{scriptedHandler: scriptedHandler{responses: tt.responses}}
			ts := httptest.NewServer(handler)
			t.Cleanup(ts.Close)
			var calls atomic.Int32
			c, err := New(ts.URL, WithRetry(fastRetry()), withSleeper(&fakeSleeper{}), tt.opt(&calls))
			require.NoError(t, err)

			_, err = c.Users().Get(context.Background(), "u1")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantAuths, handler.auths)
		})
	}
}
