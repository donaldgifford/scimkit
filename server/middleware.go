package server

import (
	"log/slog"
	"mime"
	"net/http"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

// withRecover converts a handler panic into a generic 500 error body so a
// single bad request can never take the process down.
func withRecover(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				logger.Error("panic serving request", "panic", v, "method", r.Method, "path", r.URL.Path)
				writeError(w, logger, &scim.Error{Status: http.StatusInternalServerError, Detail: "internal error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status for the request log line.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(status int) {
	sr.status = status
	sr.ResponseWriter.WriteHeader(status)
}

// Unwrap exposes the underlying writer so http.ResponseController (and
// any consumer middleware that unwraps) reaches Flush, Hijack, and the
// deadline setters through the logging layer.
func (sr *statusRecorder) Unwrap() http.ResponseWriter {
	return sr.ResponseWriter
}

// withLogging emits one structured log line per request.
func withLogging(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("scim request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

// withMediaType rejects request bodies that are not SCIM JSON. Both
// application/scim+json and plain application/json are accepted
// (RFC 7644 §3.1); several IdPs send the latter. Requests without a body
// pass through untouched.
func withMediaType(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if r.ContentLength != 0 && ct != "" {
			parsed, _, err := mime.ParseMediaType(ct)
			if err != nil || (parsed != mediaType && parsed != "application/json") {
				writeError(w, logger, &scim.Error{
					Status: http.StatusUnsupportedMediaType,
					Detail: "content type must be application/scim+json",
				})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
