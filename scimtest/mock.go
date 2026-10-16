package scimtest

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// mockMaxFilterResults caps the mock's scan-and-match filter fallback. It
// is generous because the mock holds test-scale data in memory.
const mockMaxFilterResults = 1000

// errBadToken rejects a request whose bearer token does not match the one
// configured with WithBearerToken. Its identity does not matter — the
// server answers any non-nil verify error with a SCIM 401.
var errBadToken = errors.New("scimtest: bearer token mismatch")

// Mock is an in-memory SCIM service provider backed by the real server
// and memstore packages. It carries no test dependency, so both go test
// (via NewServer) and the CLI mock command drive the same implementation.
type Mock struct {
	handler  http.Handler
	stores   map[string]server.Store
	recorder *Recorder
	token    string
	baseURL  string
}

// NewMock builds a mock service provider. It serves the library's built-in
// User and Group types plus any registered via WithResourceType, each
// backed by a fresh memstore under the configured profile.
func NewMock(opts ...Option) (*Mock, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	types := resolveTypes(&cfg)

	regs := make([]server.Registration, 0, len(types))
	stores := make(map[string]server.Store, len(types))
	endpoints := make(map[string]string, len(types))
	for i := range types {
		ts := &types[i]
		storeOpts := append([]memstore.Option{memstore.WithProfile(cfg.profile)}, ts.opts...)
		st := memstore.New(ts.rt, storeOpts...)
		regs = append(regs, server.Registration{Type: ts.rt, Store: st})
		stores[storeKey(ts.rt.Name)] = st
		endpoints[storeKey(ts.rt.Name)] = ts.rt.Endpoint
	}

	handler, err := server.New(server.Config{
		BaseURL:                     cfg.baseURL,
		ResourceTypes:               regs,
		Profile:                     cfg.profile,
		Auth:                        mockAuth(cfg.token),
		ExemptServiceProviderConfig: true,
		MaxFilterResults:            mockMaxFilterResults,
		Logger:                      cfg.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("scimtest: build mock: %w", err)
	}

	seed, err := resolveSeed(&cfg)
	if err != nil {
		return nil, err
	}
	if !seed.empty() {
		if serr := applySeed(handler, seed, endpoints, cfg.token); serr != nil {
			return nil, serr
		}
	}

	// Chaos wraps the SCIM handler; the recorder wraps chaos so injected
	// failures are still captured. Seeding above used the bare handler, so
	// it is unaffected by either layer.
	served := handler
	if cfg.chaos != nil {
		served = newChaosHandler(*cfg.chaos, handler)
	}
	rec := &Recorder{limit: cfg.recorderLimit}
	return &Mock{
		handler:  rec.wrap(served),
		stores:   stores,
		recorder: rec,
		token:    cfg.token,
		baseURL:  cfg.baseURL,
	}, nil
}

// resolveTypes materializes the registration list: the default User and
// Group pair (unless WithoutDefaultTypes), with any WithResourceType whose
// name folds equal to a default replacing it in place, and the rest
// appended in option order.
func resolveTypes(cfg *mockConfig) []typeSpec {
	var types []typeSpec
	if !cfg.noDefaults {
		types = []typeSpec{{rt: scim.UserResourceType()}, {rt: scim.GroupResourceType()}}
	}
	for i := range cfg.types {
		custom := &cfg.types[i]
		idx := slices.IndexFunc(types, func(ts typeSpec) bool {
			return storeKey(ts.rt.Name) == storeKey(custom.rt.Name)
		})
		if idx >= 0 {
			types[idx] = *custom
			continue
		}
		types = append(types, *custom)
	}
	return types
}

// resolveSeed merges the structured seed with any raw seedJSON supplied via
// WithSeedJSON, decoding the latter and reporting a malformed document.
func resolveSeed(cfg *mockConfig) (Seed, error) {
	seed := cfg.seed
	if len(cfg.seedJSON) == 0 {
		return seed, nil
	}
	extra, err := DecodeSeed(bytes.NewReader(cfg.seedJSON))
	if err != nil {
		return Seed{}, err
	}
	seed.merge(extra)
	return seed, nil
}

// Handler returns the mock's http.Handler for mounting on a listener or an
// httptest server.
func (m *Mock) Handler() http.Handler { return m.handler }

// Store returns the backing store for a resource type by its declared name
// (case-insensitive), e.g. "User" or "Group", or nil for an unregistered
// type. The concrete type is *memstore.Store; assert server.Patcher to
// reach the native PATCH path.
func (m *Mock) Store(name string) server.Store { return m.stores[storeKey(name)] }

// Recorder returns the request recorder capturing all inbound traffic.
func (m *Mock) Recorder() *Recorder { return m.recorder }

// mockAuth builds the bearer guard, or nil when no token is configured.
func mockAuth(token string) server.Auth {
	if token == "" {
		return nil
	}
	want := []byte(token)
	return server.BearerToken(func(_ context.Context, got string) error {
		// Constant-time so the mock does not model a timing side channel
		// that a consumer might copy into production.
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			return errBadToken
		}
		return nil
	})
}

// storeKey folds a resource-type name to its case-insensitive lookup key.
func storeKey(name string) string { return scim.FoldName(name) }
