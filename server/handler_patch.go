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
	body, err := readBody(r)
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
func (h *typeHandler) patchNative(r *http.Request, id string, ops []patch.Op) (*scim.Resource, error) {
	if r.Header.Get("If-Match") != "" {
		current, err := h.store.Get(r.Context(), id)
		if err != nil {
			return nil, err
		}
		if err := checkIfMatch(r, current); err != nil {
			return nil, err
		}
	}
	return h.patcher.Patch(r.Context(), id, ops)
}

// patchFallback is the Get → patch.Apply → conditional Replace cycle for
// stores without native PATCH. It runs under the resource's stripe lock;
// the compare-and-swap retry loop covers writers that bypass this
// server. The If-Match precondition is checked once, against the first
// read — later retries reconcile with concurrent writers, which is
// exactly what the precondition-free CAS loop is for.
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
		m := next.Meta()
		m.LastModified = time.Now().UTC()
		next.SetMeta(m)
		replaced, err := h.store.Replace(r.Context(), id, next)
		if err == nil {
			return replaced, nil
		}
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != http.StatusPreconditionFailed ||
			attempt+1 >= patchRetries {
			return nil, err
		}
	}
}
