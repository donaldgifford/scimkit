package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// ResourceService is the generic CRUD+PATCH client for one resource
// type. Get it from Client.Resources, or as the embedded base of the
// typed Users and Groups services.
type ResourceService struct {
	client   *Client
	resolver typeResolver
}

// Create posts a new resource and returns the server's stored form.
func (s *ResourceService) Create(ctx context.Context, res *scim.Resource) (*scim.Resource, error) {
	rt, err := s.resolver.resourceType(ctx)
	if err != nil {
		return nil, err
	}
	body, err := scim.EncodeResource(res, scim.Projection{})
	if err != nil {
		return nil, fmt.Errorf("scimkit client: encode resource: %w", err)
	}
	return s.client.doResource(ctx, http.MethodPost, s.client.requestURL(rt.Endpoint), body, rt, nil)
}

// Get fetches a resource by id.
func (s *ResourceService) Get(ctx context.Context, id string) (*scim.Resource, error) {
	rt, err := s.resolver.resourceType(ctx)
	if err != nil {
		return nil, err
	}
	return s.client.doResource(ctx, http.MethodGet, s.client.requestURL(rt.Endpoint, id), nil, rt, nil)
}

// Replace does a full PUT. When the resource carries a meta.version it is
// sent as an If-Match precondition, so a concurrent change surfaces as a
// *ConflictError rather than a silent overwrite.
func (s *ResourceService) Replace(ctx context.Context, id string, res *scim.Resource) (*scim.Resource, error) {
	rt, err := s.resolver.resourceType(ctx)
	if err != nil {
		return nil, err
	}
	body, err := scim.EncodeResource(res, scim.Projection{})
	if err != nil {
		return nil, fmt.Errorf("scimkit client: encode resource: %w", err)
	}
	headers := ifMatch(res.Meta().Version)
	return s.client.doResource(ctx, http.MethodPut, s.client.requestURL(rt.Endpoint, id), body, rt, headers)
}

// Delete removes a resource by id.
func (s *ResourceService) Delete(ctx context.Context, id string) error {
	rt, err := s.resolver.resourceType(ctx)
	if err != nil {
		return err
	}
	resp, err := s.client.do(ctx, http.MethodDelete, s.client.requestURL(rt.Endpoint, id), nil, nil)
	if err != nil {
		return err
	}
	if resp.status >= 400 {
		return decodeError(resp.status, resp.body)
	}
	return nil
}

// Patch applies ops via a PatchOp request, returning the updated
// resource. When the target does not support PATCH — advertised in
// ServiceProviderConfig, or answered with 501 — and the fallback is
// enabled (the default), it degrades to Get → patch.Apply → Replace,
// which preserves compare-and-swap through the fetched version.
func (s *ResourceService) Patch(ctx context.Context, id string, ops []patch.Op) (*scim.Resource, error) {
	if s.client.patchFallback {
		// The capability check is best-effort: a target that does not
		// implement ServiceProviderConfig (despite the RFC) should still
		// be tried natively and caught by the reactive 501 fallback
		// below, not fail here.
		if caps, err := s.client.Capabilities(ctx); err != nil {
			s.client.logCapabilityFailure(err)
		} else if !caps.PatchSupported {
			return s.replaceViaPatch(ctx, id, ops)
		}
	}
	res, err := s.patchRequest(ctx, id, ops)
	if err == nil || !s.client.patchFallback || !isNotImplemented(err) {
		return res, err
	}
	return s.replaceViaPatch(ctx, id, ops)
}

// logCapabilityFailure reports a failed capability probe. A protocol
// error (typically 401/403 on /ServiceProviderConfig) is logged at Warn
// once per client so a misconfigured token is visible before the first
// PATCH fails; anything else stays at Debug.
func (c *Client) logCapabilityFailure(err error) {
	if scimErr, ok := errors.AsType[*scim.Error](err); ok {
		c.capsWarnOnce.Do(func() {
			c.logger.Warn("scim client: capability check failed, attempting native PATCH",
				"status", scimErr.Status, "err", err)
		})
		return
	}
	c.logger.Debug("scim client: capability check failed, attempting native PATCH", "err", err)
}

// patchRequest issues the native PATCH and fills an empty response body
// with a follow-up Get, so callers always receive the full resource.
func (s *ResourceService) patchRequest(ctx context.Context, id string, ops []patch.Op) (*scim.Resource, error) {
	rt, err := s.resolver.resourceType(ctx)
	if err != nil {
		return nil, err
	}
	body, err := patch.EncodeOps(ops)
	if err != nil {
		return nil, fmt.Errorf("scimkit client: encode patch ops: %w", err)
	}
	res, err := s.client.doResource(ctx, http.MethodPatch, s.client.requestURL(rt.Endpoint, id), body, rt, nil)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return s.Get(ctx, id)
	}
	return res, nil
}

// replaceViaPatch is the PATCH→PUT fallback: read, apply the ops
// locally under the strict profile (the ops are the client's own
// well-formed output, no IdP quirk to tolerate), and write back with the
// fetched version as the CAS token.
func (s *ResourceService) replaceViaPatch(ctx context.Context, id string, ops []patch.Op) (*scim.Resource, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	result, err := patch.Apply(cur, ops, patch.Strict)
	if err != nil {
		return nil, err
	}
	return s.Replace(ctx, id, result.Resource)
}

// ifMatch builds the conditional-write header when a version is known.
func ifMatch(version string) map[string]string {
	if version == "" {
		return nil
	}
	return map[string]string{"If-Match": version}
}
