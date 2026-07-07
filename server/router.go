package server

import (
	"net/http"

	"github.com/donaldgifford/scimkit/scim"
)

// New validates the config and returns the SCIM service provider as an
// http.Handler, ready to mount under Config.BaseURL's path:
//
//	srv, err := server.New(cfg)
//	mux.Handle("/scim/v2/", http.StripPrefix("/scim/v2", srv))
//
// Per registered type it serves CRUD, PATCH, list, and POST .search;
// alongside them the discovery endpoints (/ServiceProviderConfig,
// /ResourceTypes, /Schemas), 501 for the unimplemented /Bulk and /Me,
// and SCIM error bodies for unknown paths.
//
//nolint:gocritic // cfg is passed by value on purpose: server.New(Config{…}) is the public constructor API per DESIGN-0001
func New(cfg Config) (http.Handler, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	disc, err := newDiscovery(&cfg)
	if err != nil {
		return nil, err
	}
	locks := new(stripedLocks)
	mux := http.NewServeMux()
	for i := range cfg.ResourceTypes {
		registerType(mux, newTypeHandler(&cfg.ResourceTypes[i], &cfg, locks))
	}
	registerDiscovery(mux, disc)
	registerUnsupported(mux, &cfg)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, cfg.Logger, scim.ErrNotFound("no resource at "+r.URL.Path))
	})
	return chain(mux, &cfg), nil
}

func registerType(mux *http.ServeMux, h *typeHandler) {
	ep := h.rt.Endpoint
	mux.HandleFunc("POST "+ep, h.handleCreate)
	mux.HandleFunc("GET "+ep, h.handleList)
	mux.HandleFunc("POST "+ep+"/.search", h.handleSearch)
	mux.HandleFunc("GET "+ep+"/{id}", h.handleGet)
	mux.HandleFunc("PUT "+ep+"/{id}", h.handleReplace)
	mux.HandleFunc("PATCH "+ep+"/{id}", h.handlePatch)
	mux.HandleFunc("DELETE "+ep+"/{id}", h.handleDelete)
}

func registerDiscovery(mux *http.ServeMux, d *discovery) {
	mux.HandleFunc("GET /ServiceProviderConfig", d.handleSPC)
	mux.HandleFunc("GET /ResourceTypes", d.handleResourceTypes)
	mux.HandleFunc("GET /ResourceTypes/{name}", d.handleResourceType)
	mux.HandleFunc("GET /Schemas", d.handleSchemas)
	mux.HandleFunc("GET /Schemas/{id}", d.handleSchema)
}

// registerUnsupported answers 501 for the optional endpoints this server
// does not implement; ServiceProviderConfig advertises both as
// unsupported.
func registerUnsupported(mux *http.ServeMux, cfg *Config) {
	notImplemented := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, cfg.Logger, &scim.Error{
			Status: http.StatusNotImplemented,
			Detail: "endpoint not implemented",
		})
	}
	for _, pattern := range []string{"/Bulk", "/Bulk/{rest...}", "/Me", "/Me/{rest...}"} {
		mux.HandleFunc(pattern, notImplemented)
	}
}

// chain assembles the middleware stack: recover → logging → auth →
// media type → routing. The optional ServiceProviderConfig exemption
// splits routing before the auth layer so capability discovery works
// without credentials.
func chain(mux *http.ServeMux, cfg *Config) http.Handler {
	core := withMediaType(mux, cfg.Logger)
	handler := core
	if cfg.Auth != nil {
		if la, ok := cfg.Auth.(loggerAware); ok {
			la.useLogger(cfg.Logger)
		}
		handler = cfg.Auth.Wrap(core)
		if cfg.ExemptServiceProviderConfig {
			outer := http.NewServeMux()
			outer.Handle("GET /ServiceProviderConfig", core)
			outer.Handle("/", handler)
			handler = outer
		}
	}
	return withRecover(withLogging(handler, cfg.Logger), cfg.Logger)
}
