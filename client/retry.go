package client

import (
	"context"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// RetryConfig is the retry policy. It is pure data — comparable and easy
// to build in tests. The zero value is not usable directly; New
// normalizes a caller-supplied config and DefaultRetryConfig gives a
// ready starting point. Override fields on a DefaultRetryConfig copy
// rather than building one from scratch — WithRetry replaces the policy
// wholesale and does not merge onto the defaults.
type RetryConfig struct {
	// MaxAttempts is total tries including the first; <1 normalizes to 1.
	MaxAttempts int
	// BaseDelay is the first computed backoff, before jitter.
	BaseDelay time.Duration
	// Factor multiplies the delay each attempt; <1 normalizes to 1.
	Factor float64
	// MaxDelay caps the computed backoff before jitter and before any
	// Retry-After override; <=0 normalizes to BaseDelay.
	MaxDelay time.Duration
	// Jitter is the fraction of the capped delay randomized, clamped to
	// [0,1]; 0 disables jitter.
	Jitter float64
	// RetryStatus is the statuses that retry beyond 429 (which always
	// retries while MaxAttempts>1). Nil means the default 5xx set; an
	// explicit empty slice retries only 429.
	RetryStatus []int
}

// DefaultRetryConfig is Okta's documented provisioning retry protocol:
// up to ten attempts with doubling backoff, jittered, capped at 30s.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 10,
		BaseDelay:   500 * time.Millisecond,
		Factor:      2,
		MaxDelay:    30 * time.Second,
		Jitter:      0.2,
		RetryStatus: []int{
			http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		},
	}
}

// normalize clamps a caller-supplied config into a usable policy.
func (rc RetryConfig) normalize() RetryConfig {
	if rc.MaxAttempts < 1 {
		rc.MaxAttempts = 1
	}
	if rc.Factor < 1 {
		rc.Factor = 1
	}
	if rc.BaseDelay <= 0 {
		rc.BaseDelay = DefaultRetryConfig().BaseDelay
	}
	if rc.MaxDelay <= 0 {
		rc.MaxDelay = rc.BaseDelay
	}
	switch {
	case rc.Jitter < 0:
		rc.Jitter = 0
	case rc.Jitter > 1:
		rc.Jitter = 1
	}
	if rc.RetryStatus == nil {
		rc.RetryStatus = DefaultRetryConfig().RetryStatus
	}
	return rc
}

// shouldRetry reports whether a response status is retryable: 429 always
// (while attempts remain), plus the configured status set.
func (rc RetryConfig) shouldRetry(status int) bool {
	return status == http.StatusTooManyRequests || slices.Contains(rc.RetryStatus, status)
}

// backoffDelay computes the delay before retry attempt n (1-indexed:
// n=1 is the wait before the second try), capped then jittered.
func (rc RetryConfig) backoffDelay(n int) time.Duration {
	capped := float64(rc.BaseDelay)
	for i := 1; i < n; i++ {
		capped *= rc.Factor
		if capped >= float64(rc.MaxDelay) {
			capped = float64(rc.MaxDelay)
			break
		}
	}
	if rc.Jitter == 0 {
		return time.Duration(capped)
	}
	floor := capped * (1 - rc.Jitter)
	return time.Duration(floor + rand.Float64()*(capped-floor)) //nolint:gosec // jitter, not security-sensitive
}

// parseRetryAfter parses a Retry-After header in either delta-seconds or
// HTTP-date form, relative to now. A date in the past yields zero (retry
// immediately). ok is false when the header is absent or unparseable.
func parseRetryAfter(header string, now time.Time) (time.Duration, bool) {
	if header == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs < 0 {
			return 0, true
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(header); err == nil {
		if d := t.Sub(now); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// sleeper waits for d or until the context is done; it is the test seam
// that lets retry tests run without real sleeps.
type sleeper interface {
	sleep(ctx context.Context, d time.Duration) error
}

type realSleeper struct{}

func (realSleeper) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
