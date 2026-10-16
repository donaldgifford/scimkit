package scimtest_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scimtest"
)

// oneStep is a minimal scenario whose single step expects a 201 create.
func oneStep() scimtest.Scenario {
	return scimtest.Scenario{
		Name: "one-step",
		Steps: []scimtest.Step{{
			Name:   "create",
			Method: http.MethodPost,
			Path:   "/Users",
			Body:   []byte(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"x"}`),
			Expect: scimtest.Expectation{Status: http.StatusCreated},
		}},
	}
}

// hangingHandler drains the request body and then never writes until the
// request is cancelled or release is closed. Draining matters: the server
// only watches for a client disconnect once the body has been consumed.
func hangingHandler(release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
}

// redirectHandler answers every request with a 3xx to an unreachable URL.
func redirectHandler(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/elsewhere", code)
	})
}

func TestSimulateTransportGuards(t *testing.T) {
	tests := []struct {
		name       string
		handler    func(release <-chan struct{}) http.Handler
		ctxTimeout time.Duration
		opts       []scimtest.SimulateOption
		wantErrIs  error // Simulate's returned error, nil when the step merely fails
		wantStatus int
		wantFail   string // substring expected in the step's failure messages
	}{
		{
			name:       "cancelled caller context returns a wrapped ctx error",
			handler:    hangingHandler,
			ctxTimeout: 50 * time.Millisecond,
			wantErrIs:  context.DeadlineExceeded,
		},
		{
			name:     "per-step timeout fails the step, not the run",
			handler:  hangingHandler,
			opts:     []scimtest.SimulateOption{scimtest.WithStepTimeout(50 * time.Millisecond)},
			wantFail: "transport:",
		},
		{
			name:       "redirects are recorded, never followed",
			handler:    func(<-chan struct{}) http.Handler { return redirectHandler(http.StatusFound) },
			wantStatus: http.StatusFound,
			wantFail:   "status",
		},
		{
			name:       "caller-supplied client is copied with the redirect policy",
			handler:    func(<-chan struct{}) http.Handler { return redirectHandler(http.StatusMovedPermanently) },
			opts:       []scimtest.SimulateOption{scimtest.WithHTTPClient(&http.Client{Timeout: time.Second})},
			wantStatus: http.StatusMovedPermanently,
			wantFail:   "status",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := make(chan struct{})
			ts := httptest.NewServer(tt.handler(release))
			defer ts.Close()
			defer close(release)

			ctx := context.Background()
			if tt.ctxTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.ctxTimeout)
				defer cancel()
			}

			start := time.Now()
			report, err := scimtest.SimulateWith(ctx, ts.URL, "", oneStep(), tt.opts...)
			assert.Less(t, time.Since(start), 5*time.Second, "simulator must not hang")

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}
			require.NoError(t, err)
			require.Len(t, report.Steps, 1)
			step := report.Steps[0]
			assert.False(t, step.Passed)
			assert.False(t, report.Passed)
			if tt.wantStatus != 0 {
				assert.Equal(t, tt.wantStatus, step.Status)
			}
			require.NotEmpty(t, step.Failures)
			assert.Contains(t, step.Failures[0], tt.wantFail)
		})
	}
}

func TestWithHTTPClientDoesNotMutateCaller(t *testing.T) {
	hc := &http.Client{Timeout: time.Second}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	_, err := scimtest.SimulateWith(context.Background(), ts.URL, "", oneStep(), scimtest.WithHTTPClient(hc))
	require.NoError(t, err)
	assert.Nil(t, hc.CheckRedirect, "the caller's client keeps its own redirect policy")
}

func TestSimulateOptionsIgnoreZeroValues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	report, err := scimtest.SimulateWith(context.Background(), ts.URL, "", oneStep(),
		scimtest.WithHTTPClient(nil), scimtest.WithStepTimeout(0), scimtest.WithStepTimeout(-time.Second))
	require.NoError(t, err)
	assert.True(t, report.Passed)
	assert.NotErrorIs(t, err, context.DeadlineExceeded)
}
