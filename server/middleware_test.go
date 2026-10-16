package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// plainWriter is a ResponseWriter with no optional interfaces, so a
// ResponseController over it has nothing to unwrap into.
type plainWriter struct {
	header http.Header
}

func (p *plainWriter) Header() http.Header       { return p.header }
func (*plainWriter) Write(b []byte) (int, error) { return len(b), nil }
func (*plainWriter) WriteHeader(int)             {}

func TestStatusRecorderUnwrap(t *testing.T) {
	tests := []struct {
		name      string
		inner     func() http.ResponseWriter
		wantFlush error
		flushed   func(w http.ResponseWriter) bool
	}{
		{
			name:    "flush reaches the recorder underneath",
			inner:   func() http.ResponseWriter { return httptest.NewRecorder() },
			flushed: func(w http.ResponseWriter) bool { return w.(*httptest.ResponseRecorder).Flushed },
		},
		{
			name:      "non-flusher underneath reports not supported",
			inner:     func() http.ResponseWriter { return &plainWriter{header: http.Header{}} },
			wantFlush: http.ErrNotSupported,
			flushed:   func(http.ResponseWriter) bool { return false },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inner := tt.inner()
			var flushErr error
			chain := withLogging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				flushErr = http.NewResponseController(w).Flush()
			}), discardTestLogger())
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/Users", http.NoBody)
			chain.ServeHTTP(inner, req)

			if tt.wantFlush == nil {
				require.NoError(t, flushErr)
			} else {
				require.ErrorIs(t, flushErr, tt.wantFlush)
			}
			assert.Equal(t, tt.wantFlush == nil, tt.flushed(inner))
		})
	}

	t.Run("unwrap returns the wrapped writer", func(t *testing.T) {
		inner := httptest.NewRecorder()
		rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}
		assert.Same(t, inner, rec.Unwrap())
	})
}
