package client

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/donaldgifford/scimkit/scim"
)

// Client is a SCIM 2.0 service-provider client. It is safe for
// concurrent use by multiple goroutines once constructed; it needs no
// Close. Build services with Resources, Users, or Groups.
type Client struct {
	baseURL       *url.URL
	httpClient    *http.Client
	token         string
	retry         RetryConfig
	logger        *slog.Logger
	search        bool
	patchFallback bool
	clock         sleeper

	capsMu sync.Mutex
	caps   *Capabilities

	typesMu sync.Mutex
	types   map[string]*scim.ResourceType
}

// New builds a client for the SCIM service provider at baseURL — an
// absolute URL like "https://target.example.com/scim/v2". A trailing
// slash is trimmed; resource paths are appended per registered type.
func New(baseURL string, opts ...Option) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("scimkit client: parse base URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("scimkit client: base URL %q must be absolute", baseURL)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	cfg.retry = cfg.retry.normalize()

	return &Client{
		baseURL:       u,
		httpClient:    cfg.httpClient,
		token:         cfg.token,
		retry:         cfg.retry,
		logger:        cfg.logger,
		search:        cfg.search,
		patchFallback: cfg.patchFallback,
		clock:         cfg.sleeper,
		types:         make(map[string]*scim.ResourceType),
	}, nil
}

// Resources returns a service for a resource type resolved from the
// target's discovery documents (GET /ResourceTypes/{name} + /Schemas).
// For the standard User and Group types prefer Users and Groups, which
// need no discovery round trip.
func (c *Client) Resources(name string) *ResourceService {
	return &ResourceService{
		client:   c,
		resolver: discoveredType{client: c, name: name},
	}
}

// Users returns a service for the standard User type, built from the
// library's own schema — no discovery round trip.
func (c *Client) Users() *UserService {
	rt := scim.UserResourceType()
	return &UserService{ResourceService: &ResourceService{
		client:   c,
		resolver: staticType{rt: &rt},
	}}
}

// Groups returns a service for the standard Group type, built from the
// library's own schema — no discovery round trip.
func (c *Client) Groups() *GroupService {
	rt := scim.GroupResourceType()
	return &GroupService{ResourceService: &ResourceService{
		client:   c,
		resolver: staticType{rt: &rt},
	}}
}

// typeResolver yields the resource type a service operates on, resolving
// discovery lazily for custom types.
type typeResolver interface {
	resourceType(ctx context.Context) (*scim.ResourceType, error)
}

// staticType is a resolver for the built-in types: no I/O, ever.
type staticType struct{ rt *scim.ResourceType }

func (t staticType) resourceType(context.Context) (*scim.ResourceType, error) { return t.rt, nil }

// discoveredType resolves a custom type from the target's discovery
// endpoints, caching the result on the Client.
type discoveredType struct {
	client *Client
	name   string
}

func (t discoveredType) resourceType(ctx context.Context) (*scim.ResourceType, error) {
	key := scim.FoldName(t.name)
	t.client.typesMu.Lock()
	defer t.client.typesMu.Unlock()
	if rt, ok := t.client.types[key]; ok {
		return rt, nil
	}
	rt, err := t.client.fetchResourceType(ctx, t.name)
	if err != nil {
		return nil, err
	}
	t.client.types[key] = rt
	return rt, nil
}
