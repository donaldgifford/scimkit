package scimtest

import (
	"log/slog"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// Option configures a Mock. The same options drive both NewMock (used by
// the CLI) and NewServer (used from go test).
type Option func(*mockConfig)

// typeSpec is a deferred resource-type registration: the store is not
// built until NewMock knows the configured profile, so WithProfile and
// WithResourceType compose regardless of option order.
type typeSpec struct {
	rt   scim.ResourceType
	opts []memstore.Option
}

// mockConfig accumulates option state before NewMock materializes the
// stores and handler.
type mockConfig struct {
	types   []typeSpec
	profile patch.Profile
	token   string
	baseURL string
	logger  *slog.Logger
}

// defaultConfig is the mock's baseline: strict validation (the scimtest
// posture — IdP quirks belong in named profiles, not the reference mock),
// no auth, and a placeholder base URL that NewServer overrides with its
// live httptest address.
func defaultConfig() mockConfig {
	return mockConfig{
		profile: patch.Strict,
		baseURL: "https://scim.test/v2",
		logger:  slog.Default(),
	}
}

// WithProfile sets the PATCH compatibility and write-validation profile.
// The default is patch.Strict.
func WithProfile(p patch.Profile) Option {
	return func(c *mockConfig) { c.profile = p }
}

// WithBearerToken requires the given token on every request except
// unauthenticated ServiceProviderConfig discovery. An empty token (the
// default) disables auth.
func WithBearerToken(token string) Option {
	return func(c *mockConfig) { c.token = token }
}

// WithResourceType registers an additional resource type backed by a fresh
// memstore. Supplying any type replaces the default User+Group pair, so a
// caller that wants both must register them explicitly alongside a custom
// type. The store inherits the configured profile; extra memstore options
// (indexes) are appended.
//
//nolint:gocritic // rt is passed by value to match scim/memstore constructor style
func WithResourceType(rt scim.ResourceType, opts ...memstore.Option) Option {
	return func(c *mockConfig) { c.types = append(c.types, typeSpec{rt: rt, opts: opts}) }
}

// WithLogger routes the mock's request and startup logging to l.
func WithLogger(l *slog.Logger) Option {
	return func(c *mockConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// withBaseURL overrides the base URL used for meta.location. It is
// unexported: NewServer sets it to the live httptest URL so the mock
// reports Location values that actually resolve.
func withBaseURL(u string) Option {
	return func(c *mockConfig) { c.baseURL = u }
}
