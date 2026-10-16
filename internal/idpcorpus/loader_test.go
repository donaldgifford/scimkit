package idpcorpus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAll pins the corpus contract every fixture must satisfy: it
// parses, cites its vendor documentation, targets a known resource
// type, and asserts at least one profile case.
func TestAll(t *testing.T) {
	fixtures, err := All()
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	providers := make(map[string]int)
	for _, f := range fixtures {
		t.Run(f.Provider+"/"+f.Name, func(t *testing.T) {
			assert.NotEmpty(t, f.Source, "every fixture must cite its source")
			assert.NotEmpty(t, f.Description)
			assert.Contains(t, []string{"User", "Group"}, f.ResourceType)
			assert.NotEmpty(t, f.Request)
			assert.NotEmpty(t, f.Cases)
			for _, c := range f.Cases {
				assert.Contains(t, []string{"strict", "default", "entra", "okta"}, c.Profile)
			}
		})
		providers[f.Provider]++
	}
	for _, p := range []string{"entra", "okta", "onelogin"} {
		assert.Positive(t, providers[p], "provider %s must have fixtures", p)
	}
}
