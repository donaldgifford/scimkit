package server

import (
	"io"
	"net/http"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

// typeHandler serves one registered resource type: it owns the store,
// its sniffed capabilities, and every endpoint under the type's path.
type typeHandler struct {
	rt        scim.ResourceType
	store     Store
	patcher   Patcher
	filterCap FilterUnsupported
	cfg       *Config
	locks     *stripedLocks
}

// newTypeHandler sniffs the store's optional capabilities once at wiring
// time and logs the capability report the operator debugs against.
func newTypeHandler(reg *Registration, cfg *Config, locks *stripedLocks) *typeHandler {
	h := &typeHandler{rt: reg.Type, store: reg.Store, cfg: cfg, locks: locks}
	if p, ok := reg.Store.(Patcher); ok {
		h.patcher = p
	}
	if fc, ok := reg.Store.(FilterUnsupported); ok {
		h.filterCap = fc
	}
	cfg.Logger.Info("scim resource type registered",
		"type", reg.Type.Name,
		"endpoint", reg.Type.Endpoint,
		"nativePatch", h.patcher != nil,
		"filterCapabilityReported", h.filterCap != nil,
	)
	return h
}

// location renders the resource's meta.location URL.
func (h *typeHandler) location(id string) string {
	return h.cfg.BaseURL + h.rt.Endpoint + "/" + id
}

// assignCreateIdentity stamps the server-owned identity onto a freshly
// decoded resource: a new id and the full meta block.
func (h *typeHandler) assignCreateIdentity(res *scim.Resource) error {
	id, err := scim.NewID()
	if err != nil {
		return err
	}
	if err := res.Set("id", id); err != nil {
		return err
	}
	now := time.Now().UTC()
	res.SetMeta(scim.Meta{
		ResourceType: h.rt.Name,
		Created:      now,
		LastModified: now,
		Location:     h.location(id),
	})
	return nil
}

// readBody drains the request body, mapping failure to invalidSyntax.
func readBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, scim.ErrInvalidSyntax("reading request body: " + err.Error())
	}
	return body, nil
}

// requestProjection parses the attributes/excludedAttributes query
// parameters, which apply to every resource-returning response
// (RFC 7644 §3.4.2.5) — writes included.
func requestProjection(r *http.Request) (scim.Projection, error) {
	values := r.URL.Query()
	return parseProjection(values.Get("attributes"), values.Get("excludedAttributes"))
}

// writeResource stamps the entity tag into meta.version, sets the ETag
// and (on 201) Location headers, and writes the projected body.
func (h *typeHandler) writeResource(
	w http.ResponseWriter, res *scim.Resource, p scim.Projection, status int,
) {
	tag, err := stampETag(res)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	body, err := scim.EncodeResource(res, p)
	if err != nil {
		writeError(w, h.cfg.Logger, err)
		return
	}
	w.Header().Set("ETag", tag)
	if status == http.StatusCreated {
		w.Header().Set("Location", res.Meta().Location)
	}
	writeBody(w, h.cfg.Logger, status, body)
}
