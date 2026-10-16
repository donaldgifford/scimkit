package client

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

// spcHandler serves ServiceProviderConfig with a configurable delay and
// optional first-call failure, counting hits.
type spcHandler struct {
	delay     time.Duration
	failFirst bool
	hits      atomic.Int32
}

func (h *spcHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	n := h.hits.Add(1)
	time.Sleep(h.delay)
	w.Header().Set("Content-Type", mediaType)
	if h.failFirst && n == 1 {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(spcBody))
}

func noRetry() RetryConfig {
	rc := DefaultRetryConfig()
	rc.MaxAttempts = 1
	return rc
}

func TestCapabilitiesSingleFlight(t *testing.T) {
	tests := []struct {
		name      string
		callers   int
		wantHits  int32
		failFirst bool
	}{
		{name: "concurrent cold callers share one fetch", callers: 20, wantHits: 1},
		{name: "a failed fetch is not cached", callers: 1, wantHits: 2, failFirst: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &spcHandler{delay: 50 * time.Millisecond, failFirst: tt.failFirst}
			ts := httptest.NewServer(handler)
			t.Cleanup(ts.Close)
			c, err := New(ts.URL, WithRetry(noRetry()))
			require.NoError(t, err)

			if tt.failFirst {
				_, err := c.Capabilities(context.Background())
				require.Error(t, err, "first fetch fails")
			}
			var wg sync.WaitGroup
			errs := make([]error, tt.callers)
			for i := range tt.callers {
				wg.Go(func() {
					caps, err := c.Capabilities(context.Background())
					errs[i] = err
					if err == nil && !caps.PatchSupported {
						errs[i] = errors.New("unexpected capabilities")
					}
				})
			}
			wg.Wait()
			for i, err := range errs {
				require.NoError(t, err, "caller %d", i)
			}
			assert.Equal(t, tt.wantHits, handler.hits.Load())
		})
	}
}

func TestCapabilitiesCallerDeadlineIsHonored(t *testing.T) {
	handler := &spcHandler{delay: 300 * time.Millisecond}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c, err := New(ts.URL, WithRetry(noRetry()))
	require.NoError(t, err)

	short, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = c.Capabilities(short)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), 250*time.Millisecond, "the caller returned on its own deadline")

	// The shared fetch continued; a patient caller gets the cached result
	// without a second round trip.
	caps, err := c.Capabilities(context.Background())
	require.NoError(t, err)
	assert.True(t, caps.PatchSupported)
	assert.Equal(t, int32(1), handler.hits.Load())
}

func TestListPaginationStalled(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "server ignores startIndex and reports no total",
			body: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],"totalResults":0,"Resources":[` + userBody + `]}`,
		},
		{
			name: "server echoes the same startIndex every page",
			body: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],"totalResults":0,"startIndex":1,"Resources":[` + userBody + `]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &scriptedHandler{responses: []scriptedResponse{{status: http.StatusOK, body: tt.body}}}
			ts := httptest.NewServer(handler)
			t.Cleanup(ts.Close)
			c, err := New(ts.URL, WithRetry(noRetry()))
			require.NoError(t, err)

			var got []*scim.Resource
			var iterErr error
			for res, err := range c.Users().List(context.Background(), Query{}) {
				if err != nil {
					iterErr = err
					break
				}
				got = append(got, res)
			}
			require.ErrorIs(t, iterErr, ErrPaginationStalled)
			assert.Len(t, got, 1, "the first page is yielded before the stall is detected")
			assert.Equal(t, int32(2), handler.calls.Load(), "exactly one extra page was fetched")
		})
	}
}

func TestPatchCapabilityFailureWarnsOnce(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ServiceProviderConfig", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", mediaType)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(errBody401))
	})
	mux.HandleFunc("PATCH /Users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", mediaType)
		_, _ = w.Write([]byte(userBody))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	c, err := New(ts.URL, WithRetry(noRetry()), WithLogger(logger))
	require.NoError(t, err)

	for range 3 {
		require.NoError(t, opPatch(context.Background(), c), "PATCH itself succeeds")
	}
	lines := strings.Count(buf.String(), "capability check failed")
	assert.Equal(t, 1, lines, "warned exactly once:\n%s", buf.String())
	assert.Contains(t, buf.String(), "status=401")
}
