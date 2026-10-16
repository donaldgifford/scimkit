package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/donaldgifford/scimkit/scimtest"
)

// runMock serves an in-memory SCIM mock until the context is cancelled
// (SIGINT/SIGTERM), then shuts down gracefully.
func runMock(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock", flag.ContinueOnError)
	fs.SetOutput(stdout)
	addr := fs.String("addr", ":8080", "listen address")
	seedPath := fs.String("seed", "", "path to a JSON seed file")
	profileName := fs.String("profile", "strict", "compatibility profile: strict|default|entra|okta")
	token := fs.String("token", "", "require this bearer token (default $"+tokenEnv+")")
	latency := fs.Duration("latency", 0, "artificial latency added to every response")
	failRate := fs.Float64("fail-rate", 0, "fraction of requests [0,1] to fail with 503")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tok := resolveToken(*token)

	opts, err := mockOptions(*profileName, tok, *seedPath, *latency, *failRate)
	if err != nil {
		return err
	}
	// Seeding is synchronous in-memory setup with no request context to
	// inherit; NewMock deliberately takes no context.
	mock, err := scimtest.NewMock(opts...) //nolint:contextcheck // constructor-time seeding, no ctx to thread
	if err != nil {
		return err
	}
	if tok == "" && !isLoopback(*addr) {
		slog.Warn("scimkit mock serving without a bearer token on a non-loopback address",
			"addr", *addr, "hint", "pass --token or set "+tokenEnv)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mock.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return serve(ctx, srv)
}

// tokenEnv is the environment fallback for --token on both subcommands,
// so a secret need not appear in process listings or shell history.
const tokenEnv = "SCIMKIT_TOKEN" //nolint:gosec // an env var name, not a credential

// resolveToken prefers the flag and falls back to the environment.
func resolveToken(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(tokenEnv)
}

// isLoopback reports whether addr binds only a loopback interface. An
// unparseable or empty host (e.g. ":8080") binds all interfaces.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// mockOptions translates the mock flags into scimtest options.
func mockOptions(
	profileName, token, seedPath string, latency time.Duration, failRate float64,
) ([]scimtest.Option, error) {
	profile, err := scimtest.ParseProfileName(profileName)
	if err != nil {
		return nil, err
	}
	if failRate < 0 || failRate > 1 || math.IsNaN(failRate) {
		return nil, fmt.Errorf("scimkit mock: --fail-rate %v is outside [0,1]", failRate)
	}
	if latency < 0 {
		return nil, fmt.Errorf("scimkit mock: --latency %v is negative", latency)
	}
	opts := []scimtest.Option{scimtest.WithProfile(profile)}
	if token != "" {
		opts = append(opts, scimtest.WithBearerToken(token))
	}
	if seedPath != "" {
		seed, serr := scimtest.LoadSeedFile(seedPath)
		if serr != nil {
			return nil, serr
		}
		opts = append(opts, scimtest.WithSeed(seed))
	}
	if latency > 0 || failRate > 0 {
		opts = append(opts, scimtest.WithChaos(scimtest.ChaosConfig{Latency: latency, FailRate: failRate}))
	}
	return opts, nil
}

// serve runs srv until ctx is cancelled, then drains connections within a
// bounded window. The buffered error channel lets the ListenAndServe
// goroutine always deliver its result and exit, even after shutdown.
func serve(ctx context.Context, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("scimkit mock listening", "addr", srv.Addr)

	select {
	case <-ctx.Done():
		slog.Info("scimkit mock shutting down")
		// The grace window must not inherit the already-cancelled ctx.
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shCtx) //nolint:contextcheck // drain deliberately uses a fresh, non-cancelled ctx
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("scimkit mock: serve: %w", err)
	}
}
