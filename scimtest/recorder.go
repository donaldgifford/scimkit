package scimtest

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"time"
)

// RecordedRequest is one request the mock received, captured before it
// reached the SCIM handler. Body is the fully buffered request body; the
// handler still reads its own copy.
type RecordedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
	Time   time.Time
}

// Recorder captures every request a provisioner sends to the mock so a
// test can assert on the wire traffic — how many PATCHes a membership
// change took, what headers a client set, the exact body of a create. It
// is safe for concurrent use.
type Recorder struct {
	mu   sync.Mutex
	reqs []RecordedRequest
}

// Requests returns a copy of every captured request in arrival order.
func (r *Recorder) Requests() []RecordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RecordedRequest(nil), r.reqs...)
}

// Last returns the most recently captured request, or false when none
// have been recorded.
func (r *Recorder) Last() (RecordedRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reqs) == 0 {
		return RecordedRequest{}, false
	}
	return r.reqs[len(r.reqs)-1], true
}

// Count returns the number of requests captured so far.
func (r *Recorder) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reqs)
}

// Reset discards all captured requests.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = nil
}

// wrap is the outermost middleware: it buffers and records the request,
// then restores the body so the downstream handler reads it unchanged. It
// sits ahead of auth and chaos so every attempt is captured, including
// ones later layers reject.
func (r *Recorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body []byte
		if req.Body != nil {
			buf, err := io.ReadAll(req.Body)
			if err == nil {
				body = buf
			}
			_ = req.Body.Close()
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		r.mu.Lock()
		r.reqs = append(r.reqs, RecordedRequest{
			Method: req.Method,
			Path:   req.URL.Path,
			Header: req.Header.Clone(),
			Body:   body,
			Time:   time.Now(),
		})
		r.mu.Unlock()
		next.ServeHTTP(w, req)
	})
}
