package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/donaldgifford/scimkit/scim"
)

// mediaType is the SCIM media type every response body carries
// (RFC 7644 §3.1).
const mediaType = "application/scim+json"

// writeError maps an error to the RFC 7644 §3.12 wire body. *scim.Error
// values keep their status and scimType; anything else becomes a generic
// 500 so internal details never leak onto the wire.
func writeError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var scimErr *scim.Error
	if !errors.As(err, &scimErr) {
		logger.Error("internal error", "err", err)
		scimErr = &scim.Error{Status: http.StatusInternalServerError, Detail: "internal error"}
	}
	body, merr := json.Marshal(scimErr)
	if merr != nil {
		logger.Error("marshaling error body", "err", merr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeBody(w, logger, scimErr.Status, body)
}

// writeJSON marshals v and writes it with the SCIM media type.
func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, logger, err)
		return
	}
	writeBody(w, logger, status, body)
}

// writeBody writes a pre-marshaled SCIM body.
func writeBody(w http.ResponseWriter, logger *slog.Logger, status int, body []byte) {
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(status)
	// body is JSON marshaled by encoding/json and served as
	// application/scim+json, never text/html — no markup injection vector.
	if _, err := w.Write(body); err != nil { //nolint:gosec // G705: JSON body, non-HTML content type
		logger.Debug("writing response body", "err", err)
	}
}
