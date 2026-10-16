package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/donaldgifford/scimkit/scimtest"
)

// printUsage writes the top-level help text. A write error to the help
// stream is not actionable, so it is discarded after checking.
func printUsage(w io.Writer) {
	if _, err := fmt.Fprintf(w, `scimkit — SCIM 2.0 mock server and IdP traffic simulator

Usage:
  scimkit mock [flags]        Serve an in-memory SCIM service provider
  scimkit exercise [flags]    Replay an IdP scenario against a target
  scimkit version             Print build metadata

mock flags:
  --addr        Listen address (default ":8080")
  --seed        Path to a JSON seed file (users/groups)
  --profile     Compatibility profile: strict|default|entra|okta (default "strict")
  --token       Require this bearer token (default $SCIMKIT_TOKEN)
  --latency     Artificial latency added to every response (e.g. 50ms)
  --fail-rate   Fraction of requests [0,1] to fail with 503

exercise flags:
  --target      Target SCIM base URL (required)
  --token       Bearer token (default $SCIMKIT_TOKEN)
  --scenario    Scenario name (required)
  --report      Report format: text|json (default "text")
  --timeout     Overall run timeout (default 30s)

Environment:
  SCIMKIT_TOKEN  Bearer token used when --token is not given, keeping the
                 secret out of process listings and shell history.

Scenarios:
  %s
`, strings.Join(scimtest.ScenarioNames(), "\n  ")); err != nil {
		return
	}
}
