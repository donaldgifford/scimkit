package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// patchRetries bounds the compare-and-swap loop on the PATCH fallback:
// under the stripe lock only out-of-band writers can invalidate the
// token, so contention beyond a few rounds means something is wrong.
const patchRetries = 3

// handlePatch serves PATCH /{type}/{id} (RFC 7644 §3.5.2). The response
// is always 200 with the full (projected) resource — never 204 — because
// Okta requires the body back. A no-op patch (Changed == false) returns
// the stored resource without bumping version or lastModified.
func (h *typeHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	proj, err := requestProjection(r)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	body, err := h.readBody(w, r)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	ops, err := patch.Decode(body, &h.rt, h.cfg.Profile)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	id := r.PathValue("id")
	var res *scim.Resource
	if h.patcher != nil {
		res, err = h.patchNative(r, id, ops)
	} else {
		res, err = h.patchFallback(r, id, ops)
	}
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	h.writeResource(w, res, proj, http.StatusOK)
}

// patchNative delegates to the store's Patcher, which owns atomicity and
// version bumps. An If-Match precondition is checked against the current
// resource first — best effort, since the store applies independently.
// Under patch.Strict the ops are also pre-flighted through patch.Apply
// and the schema validator so canonicalValues and required rules hold on
// PATCH exactly as on PUT; the Patcher's own apply is the one that
// commits.
func (h *typeHandler) patchNative(r *http.Request, id string, ops []patch.Op) (*scim.Resource, error) {
	strict := h.cfg.strict()
	if r.Header.Get("If-Match") == "" && !strict {
		return h.patcher.Patch(r.Context(), id, ops)
	}
	current, err := h.store.Get(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if err := checkIfMatch(r, current); err != nil {
		return nil, err
	}
	if strict {
		if err := h.preflightPatch(current, ops); err != nil {
			return nil, err
		}
	}
	return h.patcher.Patch(r.Context(), id, ops)
}

// preflightPatch applies ops to current and validates the outcome
// without persisting anything; patch.Apply never mutates its input.
func (h *typeHandler) preflightPatch(current *scim.Resource, ops []patch.Op) error {
	result, err := patch.Apply(current, ops, h.cfg.Profile)
	if err != nil {
		return err
	}
	if !result.Changed {
		return nil
	}
	return validateResource(result.Resource, &h.rt, true)
}

// patchFallback is the Get → patch.Apply → validate → conditional Replace
// cycle for stores without native PATCH. It runs under the resource's
// stripe lock; the compare-and-swap retry loop covers writers that
// bypass this server. The If-Match precondition is checked once, against
// the first read — later retries reconcile with concurrent writers,
// which is exactly what the precondition-free CAS loop is for.
func (h *typeHandler) patchFallback(r *http.Request, id string, ops []patch.Op) (*scim.Resource, error) {
	unlock := h.locks.lock(h.rt.Endpoint, id)
	defer unlock()

	for attempt := 0; ; attempt++ {
		current, err := h.store.Get(r.Context(), id)
		if err != nil {
			return nil, err
		}
		if attempt == 0 {
			if err := checkIfMatch(r, current); err != nil {
				return nil, err
			}
		}
		result, err := patch.Apply(current, ops, h.cfg.Profile)
		if err != nil {
			return nil, err
		}
		if !result.Changed {
			return current, nil
		}
		next := result.Resource
		if err := validateResource(next, &h.rt, h.cfg.strict()); err != nil {
			return nil, err
		}
		m := next.Meta()
		m.LastModified = time.Now().UTC()
		next.SetMeta(m)
		replaced, err := h.store.Replace(r.Context(), id, next)
		if err == nil {
			return replaced, nil
		}
		scimErr, ok := errors.AsType[*scim.Error](err)
		if !ok || scimErr.Status != http.StatusPreconditionFailed || attempt+1 >= patchRetries {
			return nil, err
		}
	}
}
