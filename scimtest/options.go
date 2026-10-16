package scimtest

import (
	"encoding/json"
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
	types         []typeSpec
	noDefaults    bool
	profile       patch.Profile
	token         string
	baseURL       string
	logger        *slog.Logger
	seed          Seed
	seedJSON      []byte
	chaos         *ChaosConfig
	recorderLimit int
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

// WithResourceType registers a resource type backed by a fresh memstore,
// alongside the default User and Group types. A type whose name folds
// equal to a default (e.g. a customized User) replaces that default
// instead. Use WithoutDefaultTypes to serve only the registered types. The
// store inherits the configured profile; extra memstore options (indexes)
// are appended.
//
//nolint:gocritic // rt is passed by value to match scim/memstore constructor style
func WithResourceType(rt scim.ResourceType, opts ...memstore.Option) Option {
	return func(c *mockConfig) { c.types = append(c.types, typeSpec{rt: rt, opts: opts}) }
}

// WithoutDefaultTypes drops the built-in User and Group registrations so
// the mock serves only the types supplied via WithResourceType.
func WithoutDefaultTypes() Option {
	return func(c *mockConfig) { c.noDefaults = true }
}

// WithRecorderLimit keeps only the most recent n requests in the
// Recorder, bounding memory for long chaos or retry loops. n <= 0 (the
// default) keeps every request.
func WithRecorderLimit(n int) Option {
	return func(c *mockConfig) { c.recorderLimit = max(n, 0) }
}

// WithSeed preloads the mock with resources before it serves any request.
// Each resource is created through the real pipeline, so it carries a
// server-assigned id and meta. Multiple WithSeed/WithSeedJSON/
// WithSeedResources options accumulate.
func WithSeed(seed Seed) Option {
	return func(c *mockConfig) { c.seed.merge(seed) }
}

// WithSeedResources preloads bare resource bodies for the resource type
// with the given declared name (case-insensitive), so custom types
// registered via WithResourceType can be seeded like Users and Groups.
func WithSeedResources(typeName string, docs ...json.RawMessage) Option {
	return func(c *mockConfig) {
		c.seed.merge(Seed{Resources: map[string][]json.RawMessage{typeName: docs}})
	}
}

// WithSeedJSON preloads the mock from a raw seed document — the same shape
// LoadSeedFile decodes — for inline go test literals. A malformed document
// surfaces as a NewMock error.
func WithSeedJSON(data []byte) Option {
	return func(c *mockConfig) { c.seedJSON = append(c.seedJSON, data...) }
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
