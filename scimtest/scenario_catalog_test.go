package scimtest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScenarioNames(t *testing.T) {
	names := ScenarioNames()
	assert.Len(t, names, 6)
	seen := map[string]bool{}
	for _, n := range names {
		assert.False(t, seen[n], "duplicate scenario name %q", n)
		seen[n] = true
	}
}

func TestLookupScenarioResolvesAll(t *testing.T) {
	for _, name := range ScenarioNames() {
		t.Run(name, func(t *testing.T) {
			sc, err := LookupScenario(name)
			require.NoError(t, err)
			assert.Equal(t, name, sc.Name)
			assert.NotEmpty(t, sc.Description)
			require.NotEmpty(t, sc.Steps)
			for i := range sc.Steps {
				s := &sc.Steps[i]
				assert.NotEmpty(t, s.Method, "step %q missing method", s.Name)
				assert.NotZero(t, s.Expect.Status, "step %q missing expected status", s.Name)
				assert.NotEmpty(t, s.Path, "step %q missing path", s.Name)
			}
		})
	}
}

func TestLookupScenarioCorpusBodiesEmbedded(t *testing.T) {
	// Every PATCH step in an IdP scenario must carry a non-empty body,
	// proving the corpus fixture resolved.
	for _, name := range []string{
		ScenarioEntraUserLifecycle,
		ScenarioOktaUserLifecycle,
		ScenarioOktaGroupPUT,
		ScenarioOneLoginLookupDance,
	} {
		t.Run(name, func(t *testing.T) {
			sc, err := LookupScenario(name)
			require.NoError(t, err)
			for i := range sc.Steps {
				s := &sc.Steps[i]
				if s.Method == "PATCH" {
					assert.NotEmpty(t, s.Body, "PATCH step %q has empty body", s.Name)
				}
			}
		})
	}
}

func TestLookupScenarioUnknown(t *testing.T) {
	_, err := LookupScenario("does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown scenario")
}
