package server

import (
	"encoding/json"
	"net/http"

	"github.com/donaldgifford/scimkit/scim"
)

// handleList serves GET /{type} (RFC 7644 §3.4.2).
func (h *typeHandler) handleList(w http.ResponseWriter, r *http.Request) {
	lr, err := parseListParams(r.URL.Query(), h.cfg.DefaultPageSize)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	h.respondList(w, r, &lr)
}

// handleSearch serves POST /{type}/.search (RFC 7644 §3.4.3) — the same
// query engine as GET behind a transport that keeps filters out of URLs.
func (h *typeHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	lr, err := parseSearchBody(body, h.cfg.DefaultPageSize)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	h.respondList(w, r, &lr)
}

// respondList runs the query and writes the ListResponse, stamping each
// resource's entity tag into meta.version and applying the projection.
func (h *typeHandler) respondList(w http.ResponseWriter, r *http.Request, lr *listRequest) {
	page, err := h.runQuery(r.Context(), lr.query)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	encoded := make([]json.RawMessage, 0, len(page.Resources))
	for _, res := range page.Resources {
		if _, err := stampETag(res); err != nil {
			writeError(w, h.cfg.Logger, err)
			return
		}
		body, err := scim.EncodeResource(res, lr.projection)
		if err != nil {
			writeError(w, h.cfg.Logger, err)
			return
		}
		encoded = append(encoded, body)
	}
	writeJSON(w, h.cfg.Logger, http.StatusOK, scim.ListResponse{
		TotalResults: page.TotalResults,
		StartIndex:   lr.query.StartIndex,
		ItemsPerPage: len(encoded),
		Resources:    encoded,
	})
}
