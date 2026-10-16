package scimtest

import (
	"context"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// okHandler is the downstream that chaos wraps in unit tests.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func serveChaos(cfg ChaosConfig) *httptest.ResponseRecorder {
	h := newChaosHandler(cfg, okHandler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/Users", http.NoBody)
	h.ServeHTTP(rec, req)
	return rec
}

func TestChaosFailRate(t *testing.T) {
	tests := []struct {
		name       string
		cfg        ChaosConfig
		wantStatus int
	}{
		{
			name:       "always fails with default 503",
			cfg:        ChaosConfig{FailRate: 1, Rand: rand.New(rand.NewPCG(1, 2))},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "always fails with custom status",
			cfg:        ChaosConfig{FailRate: 1, FailStatus: http.StatusBadGateway, Rand: rand.New(rand.NewPCG(1, 2))},
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "never fails passes through",
			cfg:        ChaosConfig{FailRate: 0, Rand: rand.New(rand.NewPCG(1, 2))},
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serveChaos(tt.cfg)
			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestChaosDeterministic(t *testing.T) {
	// The same seed yields the same pass/fail sequence, so chaos is
	// reproducible in consumer retry tests.
	outcomes := func() []int {
		h := newChaosHandler(ChaosConfig{FailRate: 0.5, Rand: rand.New(rand.NewPCG(42, 7))}, okHandler)
		codes := make([]int, 0, 20)
		for range 20 {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/Users", http.NoBody)
			h.ServeHTTP(rec, req)
			codes = append(codes, rec.Code)
		}
		return codes
	}
	first := outcomes()
	second := outcomes()
	assert.Equal(t, first, second, "identical seed produces identical outcomes")
	assert.Contains(t, first, http.StatusOK, "0.5 rate lets some through")
	assert.Contains(t, first, http.StatusServiceUnavailable, "0.5 rate fails some")
}

func TestChaosLatency(t *testing.T) {
	const latency = 20 * time.Millisecond
	h := newChaosHandler(ChaosConfig{Latency: latency}, okHandler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/Users", http.NoBody)

	start := time.Now()
	h.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, latency, "latency was injected")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestChaosLatencyCancelled(t *testing.T) {
	// A request whose context is already done returns without invoking the
	// handler, so a cancelled client does not block a goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := newChaosHandler(ChaosConfig{Latency: time.Hour}, okHandler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/Users", http.NoBody)

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		require.Fail(t, "chaos latency did not abort on cancelled context")
	}
}

func TestWithChaosIntegration(t *testing.T) {
	srv := NewServer(t, WithChaos(ChaosConfig{FailRate: 1}))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL()+"/Users", http.NoBody)
	require.NoError(t, err)
	resp, err := srv.ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Positive(t, srv.Recorder().Count(), "chaos failures are still recorded")
}
