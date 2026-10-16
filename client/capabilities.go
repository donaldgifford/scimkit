package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/donaldgifford/scimkit/scim"
)

// Capabilities is the parsed ServiceProviderConfig: what the target
// supports. It drives the PATCH→PUT fallback and informs callers.
type Capabilities struct {
	PatchSupported          bool
	ETagSupported           bool
	FilterSupported         bool
	MaxFilterResults        int
	SortSupported           bool
	BulkSupported           bool
	BulkMaxOperations       int
	BulkMaxPayloadSize      int
	ChangePasswordSupported bool
}

// capsFetch is one in-flight ServiceProviderConfig fetch shared by every
// concurrent Capabilities caller (single-flight), so a cold cache costs
// one round trip and a hung endpoint never holds the cache lock.
type capsFetch struct {
	done chan struct{}
	caps Capabilities
	err  error
}

// Capabilities fetches and caches the target's ServiceProviderConfig.
// The result is cached for the client's lifetime; a transient fetch
// failure is not cached. Use InvalidateCapabilities to force a refetch.
//
// Concurrent callers on a cold cache share one fetch. The fetch runs
// detached from the first caller's cancellation (bounded by the HTTP
// client's timeout and the retry policy) so that one caller giving up
// does not fail the others; each caller still returns as soon as its
// own context is done.
func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	c.capsMu.Lock()
	if c.caps != nil {
		caps := *c.caps
		c.capsMu.Unlock()
		return caps, nil
	}
	f := c.capsInflight
	if f == nil {
		f = &capsFetch{done: make(chan struct{})}
		c.capsInflight = f
		go c.runCapsFetch(context.WithoutCancel(ctx), f)
	}
	c.capsMu.Unlock()

	select {
	case <-f.done:
		return f.caps, f.err
	case <-ctx.Done():
		return Capabilities{}, fmt.Errorf("scimkit client: capabilities: %w", ctx.Err())
	}
}

// runCapsFetch performs the shared fetch, publishes a success into the
// cache, and releases the in-flight slot so a failure is retried by the
// next caller rather than cached.
func (c *Client) runCapsFetch(ctx context.Context, f *capsFetch) {
	f.caps, f.err = c.fetchCapabilities(ctx)
	c.capsMu.Lock()
	if f.err == nil {
		caps := f.caps
		c.caps = &caps
	}
	if c.capsInflight == f {
		c.capsInflight = nil
	}
	c.capsMu.Unlock()
	close(f.done)
}

// InvalidateCapabilities clears the cached ServiceProviderConfig so the
// next Capabilities call refetches it.
func (c *Client) InvalidateCapabilities() {
	c.capsMu.Lock()
	defer c.capsMu.Unlock()
	c.caps = nil
}

// spcWire is the ServiceProviderConfig subset the client reads.
type spcWire struct {
	Patch          spcFlag       `json:"patch"`
	Bulk           spcBulkWire   `json:"bulk"`
	Filter         spcFilterWire `json:"filter"`
	ChangePassword spcFlag       `json:"changePassword"`
	Sort           spcFlag       `json:"sort"`
	ETag           spcFlag       `json:"etag"`
}

type spcFlag struct {
	Supported bool `json:"supported"`
}

type spcBulkWire struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations"`
	MaxPayloadSize int  `json:"maxPayloadSize"`
}

type spcFilterWire struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults"`
}

func (c *Client) fetchCapabilities(ctx context.Context) (Capabilities, error) {
	resp, err := c.do(ctx, http.MethodGet, c.requestURL("ServiceProviderConfig"), nil, nil)
	if err != nil {
		return Capabilities{}, err
	}
	if resp.status >= 400 {
		return Capabilities{}, decodeError(resp.status, resp.body)
	}
	var w spcWire
	if err := json.Unmarshal(resp.body, &w); err != nil {
		return Capabilities{}, fmt.Errorf("scimkit client: decode ServiceProviderConfig: %w", err)
	}
	return Capabilities{
		PatchSupported:          w.Patch.Supported,
		ETagSupported:           w.ETag.Supported,
		FilterSupported:         w.Filter.Supported,
		MaxFilterResults:        w.Filter.MaxResults,
		SortSupported:           w.Sort.Supported,
		BulkSupported:           w.Bulk.Supported,
		BulkMaxOperations:       w.Bulk.MaxOperations,
		BulkMaxPayloadSize:      w.Bulk.MaxPayloadSize,
		ChangePasswordSupported: w.ChangePassword.Supported,
	}, nil
}

// resourceTypeWire is the ResourceType discovery document the client
// reads to resolve a custom type's endpoint and schemas.
type resourceTypeWire struct {
	Name             string `json:"name"`
	Endpoint         string `json:"endpoint"`
	Schema           string `json:"schema"`
	SchemaExtensions []struct {
		Schema   string `json:"schema"`
		Required bool   `json:"required"`
	} `json:"schemaExtensions"`
}

// fetchResourceType resolves a custom resource type from the target's
// discovery endpoints: GET /ResourceTypes/{name} for the endpoint and
// schema URNs, then GET /Schemas/{id} for each schema.
func (c *Client) fetchResourceType(ctx context.Context, name string) (*scim.ResourceType, error) {
	resp, err := c.do(ctx, http.MethodGet, c.requestURL("ResourceTypes", name), nil, nil)
	if err != nil {
		return nil, err
	}
	if resp.status >= 400 {
		return nil, decodeError(resp.status, resp.body)
	}
	var w resourceTypeWire
	if err := json.Unmarshal(resp.body, &w); err != nil {
		return nil, fmt.Errorf("scimkit client: decode ResourceType %q: %w", name, err)
	}
	base, err := c.fetchSchema(ctx, w.Schema)
	if err != nil {
		return nil, err
	}
	rt := &scim.ResourceType{Name: w.Name, Endpoint: w.Endpoint, Schema: base}
	rt.Extensions = make([]scim.SchemaExtension, 0, len(w.SchemaExtensions))
	for _, ext := range w.SchemaExtensions {
		es, err := c.fetchSchema(ctx, ext.Schema)
		if err != nil {
			return nil, err
		}
		rt.Extensions = append(rt.Extensions, scim.SchemaExtension{Schema: es, Required: ext.Required})
	}
	return rt, nil
}

// fetchSchema retrieves and parses one schema document by URN.
func (c *Client) fetchSchema(ctx context.Context, id string) (scim.Schema, error) {
	resp, err := c.do(ctx, http.MethodGet, c.requestURL("Schemas", id), nil, nil)
	if err != nil {
		return scim.Schema{}, err
	}
	if resp.status >= 400 {
		return scim.Schema{}, decodeError(resp.status, resp.body)
	}
	schema, err := scim.ParseSchema(resp.body)
	if err != nil {
		return scim.Schema{}, fmt.Errorf("scimkit client: parse schema %q: %w", id, err)
	}
	return schema, nil
}
