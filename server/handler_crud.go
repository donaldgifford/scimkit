package server

import (
	"net/http"

	"github.com/donaldgifford/scimkit/scim"
)

// handleCreate serves POST /{type} (RFC 7644 §3.3).
func (h *typeHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
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
	res, err := scim.DecodeResource(body, &h.rt, scim.DropReadOnly())
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	if err := validateResource(res, &h.rt, h.cfg.strict()); err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	if err := h.assignCreateIdentity(res); err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	created, err := h.store.Create(r.Context(), res)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	h.writeResource(w, created, proj, http.StatusCreated)
}

// handleGet serves GET /{type}/{id} (RFC 7644 §3.4.1), honoring
// If-None-Match with 304 — the one method conditional reads apply to.
func (h *typeHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	proj, err := requestProjection(r)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	res, err := h.store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	tag, err := resourceETag(res)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatch(inm, tag) {
		w.Header().Set("ETag", tag)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.writeResource(w, res, proj, http.StatusOK)
}

// handleReplace serves PUT /{type}/{id} (RFC 7644 §3.5.1) as a locked
// read-modify-write: readOnly attributes are retained from the current
// resource, immutable ones must match, and the current version rides
// along as the store's compare-and-swap token.
func (h *typeHandler) handleReplace(w http.ResponseWriter, r *http.Request) {
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
	id := r.PathValue("id")
	unlock := h.locks.lock(h.rt.Endpoint, id)
	defer unlock()

	current, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	if err := checkIfMatch(r, current); err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	incoming, err := scim.DecodeResource(body, &h.rt, scim.DropReadOnly())
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	if err := mergePut(incoming, current, &h.rt); err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	if err := validateResource(incoming, &h.rt, h.cfg.strict()); err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	replaced, err := h.store.Replace(r.Context(), id, incoming)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	h.writeResource(w, replaced, proj, http.StatusOK)
}

// handleDelete serves DELETE /{type}/{id} (RFC 7644 §3.6): 204 with no
// body and no Content-Type.
func (h *typeHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	unlock := h.locks.lock(h.rt.Endpoint, id)
	defer unlock()

	if r.Header.Get("If-Match") != "" {
		current, err := h.store.Get(r.Context(), id)
		if err != nil {
			writeError(w, h.cfg.Logger, err)
			return
		}
		if err := checkIfMatch(r, current); err != nil {
			writeError(w, h.cfg.Logger, err)
			return
		}
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
