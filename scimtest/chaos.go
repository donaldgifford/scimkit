package scimtest

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

// ChaosConfig injects controlled faults into the mock so consumers can
// exercise retry, backoff, and timeout handling against realistic
// flakiness. The zero value is inert.
type ChaosConfig struct {
	// Latency is added before every response; 0 disables it.
	Latency time.Duration
	// FailRate is the fraction of requests, in [0,1], answered with
	// FailStatus instead of reaching the handler.
	FailRate float64
	// FailStatus is the injected failure status. 0 defaults to 503, the
	// client's default-retryable code.
	FailStatus int
	// Rand makes fault injection deterministic for tests. Nil uses the
	// package-global source, which is auto-seeded and concurrency-safe.
	Rand *rand.Rand
}

// WithChaos wraps the mock with fault injection. Chaos sits between the
// recorder and the SCIM handler, so injected failures are still recorded
// (matching real infrastructure flakiness ahead of application logic) and
// seeded resources, loaded through the bare handler, are never affected.
func WithChaos(cfg ChaosConfig) Option {
	return func(c *mockConfig) { c.chaos = &cfg }
}

// chaosHandler applies a ChaosConfig around a downstream handler.
type chaosHandler struct {
	cfg  ChaosConfig
	mu   sync.Mutex // guards cfg.Rand, which is not safe for concurrent use
	next http.Handler
}

func newChaosHandler(cfg ChaosConfig, next http.Handler) *chaosHandler {
	return &chaosHandler{cfg: cfg, next: next}
}

func (h *chaosHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Latency > 0 {
		select {
		case <-time.After(h.cfg.Latency):
		case <-r.Context().Done():
			return
		}
	}
	if h.shouldFail() {
		h.writeFailure(w)
		return
	}
	h.next.ServeHTTP(w, r)
}

// shouldFail rolls against FailRate.
func (h *chaosHandler) shouldFail() bool {
	if h.cfg.FailRate <= 0 {
		return false
	}
	return h.roll() < h.cfg.FailRate
}

// roll returns a value in [0,1), locking a caller-supplied source.
func (h *chaosHandler) roll() float64 {
	if h.cfg.Rand == nil {
		return rand.Float64() //nolint:gosec // fault injection, not cryptographic
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.Rand.Float64()
}

// writeFailure answers with a SCIM error body carrying the failure status.
func (h *chaosHandler) writeFailure(w http.ResponseWriter) {
	status := h.cfg.FailStatus
	if status == 0 {
		status = http.StatusServiceUnavailable
	}
	body, err := json.Marshal(&scim.Error{Status: status, Detail: "scimtest: injected chaos failure"})
	if err != nil {
		http.Error(w, "scimtest: chaos", status)
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(status)
	if _, werr := w.Write(body); werr != nil {
		return
	}
}
