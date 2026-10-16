package client

import (
	"log/slog"
	"net/http"
	"time"
)

// Option configures a Client. Pass options to New.
type Option func(*config)

// config is the accumulated option state, applied in New.
type config struct {
	httpClient    *http.Client
	token         string
	retry         RetryConfig
	logger        *slog.Logger
	search        bool
	patchFallback bool
	sleeper       sleeper
}

func defaultConfig() config {
	return config{
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		retry:         DefaultRetryConfig(),
		logger:        slog.Default(),
		patchFallback: true,
		sleeper:       realSleeper{},
	}
}

// WithBearerToken sets a constant OAuth bearer token sent as the
// Authorization header on every request. There is no refresh hook in v0.
func WithBearerToken(token string) Option {
	return func(c *config) { c.token = token }
}

// WithHTTPClient sets the underlying HTTP client; its Transport and
// Timeout are respected as-is. The default has a 30s timeout.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithRetry replaces the retry policy wholesale (no merge onto the
// defaults). Start from DefaultRetryConfig and override fields.
func WithRetry(rc RetryConfig) Option {
	return func(c *config) { c.retry = rc }
}

// WithSearchTransport makes List and Lookup use POST {endpoint}/.search
// by default instead of GET, keeping filters out of URLs and access
// logs. A per-call Query.Search forces it regardless.
func WithSearchTransport(enabled bool) Option {
	return func(c *config) { c.search = enabled }
}

// WithPatchFallback controls the PATCH→PUT downgrade when the target
// advertises patch.supported=false or answers 501. It is on by default;
// disable it to make an unsupported PATCH fail loudly instead.
func WithPatchFallback(enabled bool) Option {
	return func(c *config) { c.patchFallback = enabled }
}

// WithLogger sets the logger for retry and backoff diagnostics (Debug
// level). Request and response bodies are never logged.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}
