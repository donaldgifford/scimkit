package patch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

func TestSynthesizeAddValuePath(t *testing.T) {
	tests := []struct {
		name  string
		op    string
		check func(t *testing.T, res *scim.Resource)
	}{
		{
			name: "sub-attribute add manufactures the element",
			op:   `{"op": "add", "path": "emails[type eq \"other\"].value", "value": "o@example.com"}`,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 3)
				assert.Equal(t, scim.ComplexValue{"type": "other", "value": "o@example.com"}, list[2])
			},
		},
		{
			name: "whole-element add merges value over the seed",
			op:   `{"op": "add", "path": "emails[type eq \"other\"]", "value": {"display": "Other"}}`,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 3)
				assert.Equal(t, scim.ComplexValue{"type": "other", "display": "Other"}, list[2])
			},
		},
		{
			name: "eq conjunction seeds several sub-attributes",
			op:   `{"op": "add", "path": "emails[type eq \"other\" and display eq \"Alt\"].value", "value": "o@example.com"}`,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 3)
				assert.Equal(t, "Alt", list[2]["display"])
				assert.Equal(t, "other", list[2]["type"])
			},
		},
		{
			name: "synthesized primary demotes the previous one",
			op:   `{"op": "add", "path": "emails[type eq \"other\"].primary", "value": true}`,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 3)
				assert.Equal(t, false, list[0]["primary"], "home email demoted")
				assert.Equal(t, true, list[2]["primary"])
			},
		},
		{
			name: "appends alongside non-matching elements",
			op:   `{"op": "add", "path": "singles[note eq \"fresh\"].note", "value": "fresh"}`,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, ok := res.Get("singles")
				require.True(t, ok)
				assert.Equal(t, scim.MultiComplexValue{{"note": "only"}, {"note": "fresh"}}, v)
			},
		},
		{
			name: "matching element suppresses synthesis",
			op:   `{"op": "add", "path": "emails[type eq \"work\"].display", "value": "Work"}`,
			check: func(t *testing.T, res *scim.Resource) {
				t.Helper()
				v, _ := res.Get("emails")
				list := v.(scim.MultiComplexValue)
				require.Len(t, list, 2, "no third element manufactured")
				assert.Equal(t, "Work", list[1]["display"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := removeWidget(t)
			result, err := applyOne(t, res, Entra, `[`+tt.op+`]`)
			require.NoError(t, err)
			assert.True(t, result.Changed)
			tt.check(t, result.Resource)
		})
	}
}

// TestSynthesizeIntoUnassigned pins synthesis onto a fully unassigned
// attribute: the manufactured element becomes the first one.
func TestSynthesizeIntoUnassigned(t *testing.T) {
	rt := applyType(t)
	res := scim.NewResource(&rt)
	result, err := applyOne(t, res, Entra,
		`[{"op": "add", "path": "emails[type eq \"work\"].value", "value": "w@example.com"}]`)
	require.NoError(t, err)
	assert.True(t, result.Changed)
	v, ok := result.Resource.Get("emails")
	require.True(t, ok)
	assert.Equal(t, scim.MultiComplexValue{{"type": "work", "value": "w@example.com"}}, v)
}

func TestReplaceNoTargetAsAdd(t *testing.T) {
	res := removeWidget(t)
	result, err := applyOne(t, res, Entra,
		`[{"op": "replace", "path": "emails[type eq \"other\"].value", "value": "o@example.com"}]`)
	require.NoError(t, err)
	assert.True(t, result.Changed)
	v, _ := result.Resource.Get("emails")
	list := v.(scim.MultiComplexValue)
	require.Len(t, list, 3)
	assert.Equal(t, scim.ComplexValue{"type": "other", "value": "o@example.com"}, list[2])
}

// TestSynthesizeRejectsNonSeedableFilters pins that only eq-only
// conjunctions manufacture elements: everything else stays the strict
// noTarget error even under the most tolerant profile.
func TestSynthesizeRejectsNonSeedableFilters(t *testing.T) {
	tests := []struct {
		name string
		op   string
	}{
		{
			name: "sw operator",
			op:   `{"op": "add", "path": "emails[type sw \"o\"].value", "value": "x"}`,
		},
		{
			name: "or disjunction",
			op:   `{"op": "add", "path": "emails[type eq \"a\" or type eq \"b\"].value", "value": "x"}`,
		},
		{
			name: "not negation",
			op:   `{"op": "add", "path": "emails[not (value pr)].display", "value": "x"}`,
		},
		{
			name: "presence test",
			op:   `{"op": "add", "path": "emails[display pr].value", "value": "x"}`,
		},
		{
			name: "conflicting eq clauses",
			op:   `{"op": "add", "path": "emails[type eq \"a\" and type eq \"b\"].value", "value": "x"}`,
		},
		{
			name: "replace with non-seedable filter",
			op:   `{"op": "replace", "path": "emails[type co \"zzz\"]", "value": {"display": "x"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := removeWidget(t)
			_, err := applyOne(t, res, Entra, `[`+tt.op+`]`)
			requireScimType(t, err, scim.ScimTypeNoTarget)
		})
	}
}
