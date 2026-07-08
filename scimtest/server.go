package scimtest

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/donaldgifford/scimkit/client"
)

// Server is a running mock bound to a test's lifecycle: an httptest.Server
// in front of a Mock, torn down automatically via t.Cleanup. Use it from
// go test; the CLI uses NewMock directly.
type Server struct {
	*Mock
	ts     *httptest.Server
	tb     testing.TB
	client *client.Client
}

// NewServer starts a mock on an httptest server and registers cleanup on
// tb. It fails the test on a construction error rather than returning one,
// giving the ergonomic helper shape. The mock's meta.location values point
// at the live server URL.
func NewServer(tb testing.TB, opts ...Option) *Server {
	tb.Helper()

	// The base URL is only known once httptest allocates a port, but the
	// handler bakes it into meta.location — so serve through a lazy
	// indirection, learn the URL, then build the real handler and install
	// it. atomic.Pointer keeps the swap race-free under the detector.
	var lazy lazyHandler
	ts := httptest.NewServer(&lazy)

	mock, err := NewMock(append(opts, withBaseURL(ts.URL))...)
	if err != nil {
		ts.Close()
		tb.Fatalf("scimtest: %v", err)
		return nil
	}
	lazy.set(mock.Handler())
	tb.Cleanup(ts.Close)
	return &Server{Mock: mock, ts: ts, tb: tb}
}

// URL is the base URL the mock is reachable at.
func (s *Server) URL() string { return s.ts.URL }

// Client returns a scimkit client wired to the mock, carrying the
// configured bearer token and the test server's HTTP client. It is built
// once and cached.
func (s *Server) Client() *client.Client {
	s.tb.Helper()
	if s.client != nil {
		return s.client
	}
	opts := []client.Option{client.WithHTTPClient(s.ts.Client())}
	if s.token != "" {
		opts = append(opts, client.WithBearerToken(s.token))
	}
	c, err := client.New(s.ts.URL, opts...)
	if err != nil {
		s.tb.Fatalf("scimtest: build client: %v", err)
		return nil
	}
	s.client = c
	return c
}

// lazyHandler serves requests through a handler installed after the
// listener is up. Until then it answers 503, which no correctly-sequenced
// test can observe because the handler is set before NewServer returns.
type lazyHandler struct {
	h atomic.Pointer[http.Handler]
}

func (l *lazyHandler) set(h http.Handler) { l.h.Store(&h) }

func (l *lazyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if hp := l.h.Load(); hp != nil {
		(*hp).ServeHTTP(w, r)
		return
	}
	http.Error(w, "scimtest: mock not ready", http.StatusServiceUnavailable)
}
