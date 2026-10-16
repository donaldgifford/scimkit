package server_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

// sortFixtureType is a resource type exercising every sort-comparable
// attribute kind: a case-exact string, an integer, a decimal, a boolean,
// a dateTime, and a multi-valued complex attribute with a primary flag.
func sortFixtureType() scim.ResourceType {
	return scim.ResourceType{
		Name:     "Widget",
		Endpoint: "/Widgets",
		Schema: scim.Schema{
			ID:   "urn:example:Widget",
			Name: "Widget",
			Attributes: []scim.Attribute{
				{Name: "code", Type: scim.TypeString, CaseExact: true},
				{Name: "label", Type: scim.TypeString},
				{Name: "rank", Type: scim.TypeInteger},
				{Name: "ratio", Type: scim.TypeDecimal},
				{Name: "active", Type: scim.TypeBoolean},
				{Name: "seen", Type: scim.TypeDateTime},
				{
					Name: "emails", Type: scim.TypeComplex, MultiValued: true,
					SubAttributes: []scim.Attribute{
						{Name: "value", Type: scim.TypeString},
						{Name: "type", Type: scim.TypeString},
						{Name: "primary", Type: scim.TypeBoolean},
					},
				},
			},
		}.WithDefaults(),
	}
}

func TestSortResources(t *testing.T) {
	rt := sortFixtureType()

	tests := []struct {
		name      string
		attrs     []map[string]any // one map per resource, keyed by id
		sortBy    string
		ascending bool
		wantIDs   []string
	}{
		{
			name: "case-exact string ascending",
			attrs: []map[string]any{
				{"id": "a", "code": "Bravo"},
				{"id": "b", "code": "alpha"},
				{"id": "c", "code": "Alpha"},
			},
			sortBy: "code", ascending: true,
			// caseExact: uppercase sorts before lowercase.
			wantIDs: []string{"c", "a", "b"},
		},
		{
			name: "case-insensitive string descending",
			attrs: []map[string]any{
				{"id": "a", "label": "banana"},
				{"id": "b", "label": "Apple"},
				{"id": "c", "label": "cherry"},
			},
			sortBy: "label", ascending: false,
			wantIDs: []string{"c", "a", "b"},
		},
		{
			name: "integer ascending",
			attrs: []map[string]any{
				{"id": "a", "rank": int64(30)},
				{"id": "b", "rank": int64(10)},
				{"id": "c", "rank": int64(20)},
			},
			sortBy: "rank", ascending: true,
			wantIDs: []string{"b", "c", "a"},
		},
		{
			name: "decimal descending",
			attrs: []map[string]any{
				{"id": "a", "ratio": 1.5},
				{"id": "b", "ratio": 2.5},
				{"id": "c", "ratio": 0.5},
			},
			sortBy: "ratio", ascending: false,
			wantIDs: []string{"b", "a", "c"},
		},
		{
			name: "boolean ascending false first",
			attrs: []map[string]any{
				{"id": "a", "active": true},
				{"id": "b", "active": false},
			},
			sortBy: "active", ascending: true,
			wantIDs: []string{"b", "a"},
		},
		{
			name: "dateTime ascending",
			attrs: []map[string]any{
				{"id": "a", "seen": "2024-03-01T00:00:00Z"},
				{"id": "b", "seen": "2024-01-01T00:00:00Z"},
				{"id": "c", "seen": "2024-02-01T00:00:00Z"},
			},
			sortBy: "seen", ascending: true,
			wantIDs: []string{"b", "c", "a"},
		},
		{
			name: "missing sort key sorts last ascending",
			attrs: []map[string]any{
				{"id": "a", "rank": int64(5)},
				{"id": "b"},
				{"id": "c", "rank": int64(1)},
			},
			sortBy: "rank", ascending: true,
			wantIDs: []string{"c", "a", "b"},
		},
		{
			name: "missing sort key sorts last descending too",
			attrs: []map[string]any{
				{"id": "a", "rank": int64(5)},
				{"id": "b"},
				{"id": "c", "rank": int64(1)},
			},
			sortBy: "rank", ascending: false,
			wantIDs: []string{"a", "c", "b"},
		},
		{
			name: "multi-valued complex uses primary element",
			attrs: []map[string]any{
				{"id": "a", "emails": []any{
					map[string]any{"value": "z@x.com", "primary": false},
					map[string]any{"value": "a@x.com", "primary": true},
				}},
				{"id": "b", "emails": []any{
					map[string]any{"value": "m@x.com", "primary": true},
				}},
			},
			sortBy: "emails", ascending: true,
			// a's primary is a@x.com, b's is m@x.com.
			wantIDs: []string{"a", "b"},
		},
		{
			name: "value filter selects the sorted element",
			attrs: []map[string]any{
				{"id": "a", "emails": []any{
					map[string]any{"value": "work-b@x.com", "type": "work"},
					map[string]any{"value": "home-a@x.com", "type": "home"},
				}},
				{"id": "b", "emails": []any{
					map[string]any{"value": "work-a@x.com", "type": "work"},
				}},
			},
			sortBy: `emails[type eq "work"].value`, ascending: true,
			wantIDs: []string{"b", "a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := buildResources(t, &rt, tt.attrs)
			by, err := filter.ParsePath(tt.sortBy)
			require.NoError(t, err)
			server.SortResources(list, &rt, &by, tt.ascending)
			assert.Equal(t, tt.wantIDs, resourceIDs(list))
		})
	}
}

func buildResources(t *testing.T, rt *scim.ResourceType, attrs []map[string]any) []*scim.Resource {
	t.Helper()
	list := make([]*scim.Resource, 0, len(attrs))
	for _, a := range attrs {
		res := scim.NewResource(rt)
		for k, v := range a {
			require.NoError(t, res.Set(k, v), "set %s", k)
		}
		list = append(list, res)
	}
	return list
}

func resourceIDs(list []*scim.Resource) []string {
	ids := make([]string, 0, len(list))
	for _, res := range list {
		id, _ := res.Get("id")
		ids = append(ids, id.(string))
	}
	return ids
}
