package main

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scimtest"
)

func TestRunDispatch(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		contains string
	}{
		{name: "version", args: []string{"version"}, contains: "scimkit dev"},
		{name: "help", args: []string{"help"}, contains: "Usage:"},
		{name: "no args", args: []string{}, wantErr: true},
		{name: "unknown subcommand", args: []string{"bogus"}, wantErr: true},
		{name: "mock invalid profile", args: []string{"mock", "--profile", "nope"}, wantErr: true},
		{name: "exercise missing target", args: []string{"exercise", "--scenario", "rfc-strict-lifecycle"}, wantErr: true},
		{name: "exercise missing scenario", args: []string{"exercise", "--target", "http://x"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := run(context.Background(), tt.args, &buf)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.contains != "" {
				assert.Contains(t, buf.String(), tt.contains)
			}
		})
	}
}

func TestRunExerciseAgainstMock(t *testing.T) {
	srv := scimtest.NewServer(t)
	tests := []struct {
		name   string
		report string
		verify func(t *testing.T, out string)
	}{
		{
			name:   "text report",
			report: "text",
			verify: func(t *testing.T, out string) {
				t.Helper()
				assert.Contains(t, out, "Result:   PASS")
			},
		},
		{
			name:   "json report",
			report: "json",
			verify: func(t *testing.T, out string) {
				t.Helper()
				assert.Contains(t, out, `"passed": true`)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := run(context.Background(),
				[]string{"exercise", "--target", srv.URL(), "--scenario", "rfc-strict-lifecycle", "--report", tt.report},
				&buf)
			require.NoError(t, err)
			tt.verify(t, buf.String())
		})
	}
}

func TestRunExerciseBrokenConfigFails(t *testing.T) {
	// A mock requiring a token, exercised without one: the scenario must
	// fail, and run must surface errExerciseFailed for a non-zero exit.
	srv := scimtest.NewServer(t, scimtest.WithBearerToken("real-token"))
	var buf bytes.Buffer
	err := run(context.Background(),
		[]string{"exercise", "--target", srv.URL(), "--scenario", "rfc-strict-lifecycle"},
		&buf)
	require.ErrorIs(t, err, errExerciseFailed)
	assert.Contains(t, buf.String(), "Result:   FAIL")
}

func TestServeGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := &http.Server{Addr: "127.0.0.1:0", Handler: http.NewServeMux(), ReadHeaderTimeout: time.Second}

	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv) }()
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err, "graceful shutdown returns nil")
	case <-time.After(3 * time.Second):
		require.Fail(t, "serve did not return after context cancellation")
	}
}
