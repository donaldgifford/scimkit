package scimtest_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scimtest"
)

// TestSimulateCatalog runs every built-in scenario against a mock
// configured for the profile that scenario targets, and asserts it passes.
// This is the end-to-end validation of the scenario catalog's authoring.
func TestSimulateCatalog(t *testing.T) {
	tests := []struct {
		scenario string
		profile  patch.Profile
	}{
		{scenario: scimtest.ScenarioRFCStrictLifecycle, profile: patch.Strict},
		{scenario: scimtest.ScenarioEntraUserLifecycle, profile: patch.Entra},
		{scenario: scimtest.ScenarioEntraGroupMembership, profile: patch.Default},
		{scenario: scimtest.ScenarioOktaUserLifecycle, profile: patch.Okta},
		{scenario: scimtest.ScenarioOktaGroupPUT, profile: patch.Okta},
		{scenario: scimtest.ScenarioOneLoginLookupDance, profile: patch.Default},
	}
	for _, tt := range tests {
		t.Run(tt.scenario, func(t *testing.T) {
			srv := scimtest.NewServer(t, scimtest.WithProfile(tt.profile))
			sc, err := scimtest.LookupScenario(tt.scenario)
			require.NoError(t, err)

			report, err := scimtest.Simulate(context.Background(), srv.URL(), "", sc)
			require.NoError(t, err)
			assert.True(t, report.Passed, "scenario failed:\n%s", report.Text())
		})
	}
}

// TestSimulateSelfCheck is the CI self-check contract: a healthy mock
// passes rfc-strict-lifecycle, and a broken config (wrong token) fails it.
func TestSimulateSelfCheck(t *testing.T) {
	tests := []struct {
		name     string
		opts     []scimtest.Option
		token    string
		wantPass bool
	}{
		{name: "healthy mock passes", wantPass: true},
		{
			name:     "wrong token fails every step",
			opts:     []scimtest.Option{scimtest.WithBearerToken("real")},
			token:    "wrong",
			wantPass: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := scimtest.NewServer(t, tt.opts...)
			sc, err := scimtest.LookupScenario(scimtest.ScenarioRFCStrictLifecycle)
			require.NoError(t, err)

			report, err := scimtest.Simulate(context.Background(), srv.URL(), tt.token, sc)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPass, report.Passed, report.Text())
		})
	}
}

func TestSimulateUnknownScenarioIsBuildError(t *testing.T) {
	_, err := scimtest.LookupScenario("nope")
	require.Error(t, err)
}

func TestSimulateReportRendering(t *testing.T) {
	srv := scimtest.NewServer(t)
	sc, err := scimtest.LookupScenario(scimtest.ScenarioRFCStrictLifecycle)
	require.NoError(t, err)
	report, err := scimtest.Simulate(context.Background(), srv.URL(), "", sc)
	require.NoError(t, err)

	text := report.Text()
	assert.Contains(t, text, "Scenario: "+scimtest.ScenarioRFCStrictLifecycle)
	assert.Contains(t, text, "PASS")
	assert.Contains(t, text, "create user")

	raw, err := report.JSON()
	require.NoError(t, err)
	var round scimtest.Report
	require.NoError(t, json.Unmarshal(raw, &round))
	assert.Equal(t, report.Scenario, round.Scenario)
	assert.True(t, round.Passed)
	assert.NotEmpty(t, round.Steps)
}

// TestSimulateAbortsOnFailure verifies fail-fast: a target that rejects an
// early step leaves later steps unrun.
func TestSimulateAbortsOnFailure(t *testing.T) {
	srv := scimtest.NewServer(t, scimtest.WithBearerToken("real"))
	sc, err := scimtest.LookupScenario(scimtest.ScenarioRFCStrictLifecycle)
	require.NoError(t, err)

	report, err := scimtest.Simulate(context.Background(), srv.URL(), "", sc)
	require.NoError(t, err)
	assert.False(t, report.Passed)
	assert.True(t, report.Aborted)
	assert.Len(t, report.Steps, 1, "aborted at the first step, later steps not attempted")
	assert.NotEmpty(t, report.Steps[0].Failures)
	assert.True(t, strings.HasPrefix(report.AbortedAt, "create"))
}
