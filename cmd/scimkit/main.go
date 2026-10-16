// Command scimkit is a SCIM 2.0 mock server and IdP traffic simulator. The
// mock subcommand serves an in-memory SCIM service provider; exercise
// replays a named IdP scenario against a target and reports conformance.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// Build metadata, injected via -ldflags at release time.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// errExerciseFailed signals a scenario failure. main exits non-zero
// without logging an extra error, because the printed report is the
// diagnostic.
var errExerciseFailed = errors.New("scenario failed")

func main() { os.Exit(realMain()) }

// realMain runs the CLI and returns the process exit code, so main holds
// the only os.Exit and every defer still runs.
func realMain() int {
	// Logs go to stderr; stdout is reserved for report output.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		if !errors.Is(err, errExerciseFailed) {
			slog.Error("fatal", "err", err)
		}
		return 1
	}
	return 0
}

// run dispatches a subcommand. It takes stdout explicitly so tests can
// capture output.
func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return errors.New("scimkit: missing subcommand")
	}
	switch args[0] {
	case "mock":
		return runMock(ctx, args[1:], stdout)
	case "exercise":
		return runExercise(ctx, args[1:], stdout)
	case "version":
		_, err := fmt.Fprintf(stdout, "scimkit %s (%s, %s)\n", version, commit, date)
		return err
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stdout)
		return fmt.Errorf("scimkit: unknown subcommand %q", args[0])
	}
}
