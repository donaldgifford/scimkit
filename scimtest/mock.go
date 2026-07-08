package scimtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"

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

// NewMock builds a mock service provider. With no resource-type options it
// serves the library's built-in User and Group types, each backed by a
// fresh memstore under the configured profile.
func NewMock(opts ...Option) (*Mock, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	if len(cfg.types) == 0 {
		cfg.types = []typeSpec{{rt: scim.UserResourceType()}, {rt: scim.GroupResourceType()}}
	}

	regs := make([]server.Registration, 0, len(cfg.types))
	stores := make(map[string]server.Store, len(cfg.types))
	endpoints := make(map[string]string, len(cfg.types))
	for i := range cfg.types {
		ts := &cfg.types[i]
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

	rec := &Recorder{}
	return &Mock{
		handler:  rec.wrap(handler),
		stores:   stores,
		recorder: rec,
		token:    cfg.token,
		baseURL:  cfg.baseURL,
	}, nil
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
	seed.Users = append(seed.Users, extra.Users...)
	seed.Groups = append(seed.Groups, extra.Groups...)
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
	return server.BearerToken(func(_ context.Context, got string) error {
		if got != token {
			return errBadToken
		}
		return nil
	})
}

// storeKey folds a resource-type name to its case-insensitive lookup key.
func storeKey(name string) string { return scim.FoldName(name) }
