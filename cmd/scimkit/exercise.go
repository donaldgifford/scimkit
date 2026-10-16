package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/donaldgifford/scimkit/scimtest"
)

// runExercise replays a named scenario against a target and renders the
// report. It returns errExerciseFailed when the scenario does not pass, so
// main exits non-zero.
func runExercise(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("exercise", flag.ContinueOnError)
	fs.SetOutput(stdout)
	target := fs.String("target", "", "target SCIM base URL (required)")
	token := fs.String("token", "", "bearer token (default $"+tokenEnv+")")
	scenario := fs.String("scenario", "", "scenario name (required)")
	report := fs.String("report", "text", "report format: text|json")
	timeout := fs.Duration("timeout", 30*time.Second, "overall run timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tok := resolveToken(*token)
	if *target == "" {
		return errors.New("scimkit exercise: --target is required")
	}
	if *scenario == "" {
		return fmt.Errorf("scimkit exercise: --scenario is required (one of %v)", scimtest.ScenarioNames())
	}

	sc, err := scimtest.LookupScenario(*scenario)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	rep, err := scimtest.Simulate(runCtx, *target, tok, sc)
	if err != nil {
		return err
	}
	if rerr := renderReport(stdout, &rep, *report); rerr != nil {
		return rerr
	}
	if !rep.Passed {
		return errExerciseFailed
	}
	return nil
}

// renderReport writes the report in the requested format.
func renderReport(w io.Writer, rep *scimtest.Report, format string) error {
	switch format {
	case "json":
		out, err := rep.JSON()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(out))
		return err
	case "", "text":
		_, err := fmt.Fprint(w, rep.Text())
		return err
	default:
		return fmt.Errorf("scimkit exercise: unknown report format %q (text|json)", format)
	}
}
