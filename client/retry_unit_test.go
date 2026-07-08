package client

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRetryConfigNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   RetryConfig
		want RetryConfig
	}{
		{
			name: "zero value gets safe defaults",
			in:   RetryConfig{},
			want: RetryConfig{
				MaxAttempts: 1, Factor: 1,
				BaseDelay: 500 * time.Millisecond, MaxDelay: 500 * time.Millisecond,
				RetryStatus: DefaultRetryConfig().RetryStatus,
			},
		},
		{
			name: "negative attempts clamp to one",
			in:   RetryConfig{MaxAttempts: -5, BaseDelay: time.Second, Factor: 2, MaxDelay: time.Minute},
			want: RetryConfig{
				MaxAttempts: 1, BaseDelay: time.Second, Factor: 2, MaxDelay: time.Minute,
				RetryStatus: DefaultRetryConfig().RetryStatus,
			},
		},
		{
			name: "jitter clamped to unit interval",
			in:   RetryConfig{MaxAttempts: 2, BaseDelay: time.Second, Factor: 2, MaxDelay: time.Minute, Jitter: 5},
			want: RetryConfig{
				MaxAttempts: 2, BaseDelay: time.Second, Factor: 2, MaxDelay: time.Minute, Jitter: 1,
				RetryStatus: DefaultRetryConfig().RetryStatus,
			},
		},
		{
			name: "explicit empty status set is preserved",
			in:   RetryConfig{MaxAttempts: 2, BaseDelay: time.Second, Factor: 2, MaxDelay: time.Minute, RetryStatus: []int{}},
			want: RetryConfig{
				MaxAttempts: 2, BaseDelay: time.Second, Factor: 2, MaxDelay: time.Minute, RetryStatus: []int{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.in.normalize())
		})
	}
}

func TestBackoffDelayGrowsAndCaps(t *testing.T) {
	rc := RetryConfig{BaseDelay: time.Second, Factor: 2, MaxDelay: 8 * time.Second}.normalize()
	// No jitter: deterministic doubling, capped at MaxDelay.
	assert.Equal(t, 1*time.Second, rc.backoffDelay(1))
	assert.Equal(t, 2*time.Second, rc.backoffDelay(2))
	assert.Equal(t, 4*time.Second, rc.backoffDelay(3))
	assert.Equal(t, 8*time.Second, rc.backoffDelay(4))
	assert.Equal(t, 8*time.Second, rc.backoffDelay(5), "capped at MaxDelay")
}

func TestBackoffDelayJitterWithinBounds(t *testing.T) {
	rc := RetryConfig{BaseDelay: time.Second, Factor: 2, MaxDelay: time.Minute, Jitter: 0.5}.normalize()
	for range 100 {
		d := rc.backoffDelay(3) // capped base is 4s; jitter floor 2s
		assert.GreaterOrEqual(t, d, 2*time.Second)
		assert.LessOrEqual(t, d, 4*time.Second)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header string
		want   time.Duration
		wantOK bool
	}{
		{name: "absent", header: "", wantOK: false},
		{name: "delta seconds", header: "30", want: 30 * time.Second, wantOK: true},
		{name: "zero seconds", header: "0", want: 0, wantOK: true},
		{name: "negative seconds retries now", header: "-1", want: 0, wantOK: true},
		{name: "unparseable", header: "soon", wantOK: false},
		{
			name: "http-date in the future", header: now.Add(10 * time.Second).Format(http.TimeFormat),
			want: 10 * time.Second, wantOK: true,
		},
		{
			name: "http-date in the past retries now", header: now.Add(-time.Hour).Format(http.TimeFormat),
			want: 0, wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.header, now)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestShouldRetry(t *testing.T) {
	rc := DefaultRetryConfig()
	tests := []struct {
		status int
		want   bool
	}{
		{http.StatusTooManyRequests, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusBadGateway, true},
		{http.StatusOK, false},
		{http.StatusNotFound, false},
		{http.StatusConflict, false},
		{http.StatusBadRequest, false},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			assert.Equal(t, tt.want, rc.shouldRetry(tt.status))
		})
	}
}
