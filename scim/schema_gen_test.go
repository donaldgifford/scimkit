package scim

import (
	"embed"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The vendored RFC fixtures are embedded for tests only; runtime schema
// construction uses the generated literals in schema_gen.go.
//
//go:embed schemas/*.json
var schemaFixtures embed.FS

// TestGeneratedSchemasMatchVendoredRFC pins the generated literals to the
// vendored RFC 7643 §8.7 documents: parsing a fixture must yield exactly the
// value the corresponding generated accessor returns. A mismatch means
// either the fixture changed without `go generate ./...` or the generator's
// emission logic drifted.
func TestGeneratedSchemasMatchVendoredRFC(t *testing.T) {
	tests := []struct {
		file      string
		generated func() Schema
	}{
		{file: "schemas/user.json", generated: UserSchema},
		{file: "schemas/group.json", generated: GroupSchema},
		{file: "schemas/enterprise-user.json", generated: EnterpriseUserSchema},
		{file: "schemas/service-provider-config.json", generated: ServiceProviderConfigSchema},
		{file: "schemas/resource-type.json", generated: ResourceTypeSchema},
		{file: "schemas/schema.json", generated: SchemaMetaSchema},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data, err := schemaFixtures.ReadFile(tt.file)
			require.NoError(t, err)
			parsed, err := ParseSchema(data)
			require.NoError(t, err)
			if diff := cmp.Diff(parsed, tt.generated()); diff != "" {
				t.Errorf("generated schema drifted from vendored RFC fixture (-fixture +generated):\n%s", diff)
			}
		})
	}
}

// TestGeneratedAccessorsReturnFreshValues verifies that mutating one
// returned value never leaks into a subsequent call.
func TestGeneratedAccessorsReturnFreshValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func()
		check  func(t *testing.T)
	}{
		{
			name: "schema attribute mutation does not persist",
			mutate: func() {
				s := UserSchema()
				s.Attributes[0].Name = "mutated"
			},
			check: func(t *testing.T) {
				t.Helper()
				assert.Equal(t, "userName", UserSchema().Attributes[0].Name)
			},
		},
		{
			name: "canonical values mutation does not persist",
			mutate: func() {
				sub := findSubAttr(UserSchema(), "emails", "type")
				sub.CanonicalValues[0] = "mutated"
			},
			check: func(t *testing.T) {
				t.Helper()
				sub := findSubAttr(UserSchema(), "emails", "type")
				assert.Equal(t, "work", sub.CanonicalValues[0])
			},
		},
		{
			name: "resource type extension mutation does not persist",
			mutate: func() {
				rt := UserResourceType()
				rt.Extensions[0].Required = true
				rt.Schema.Attributes[0].Name = "mutated"
			},
			check: func(t *testing.T) {
				t.Helper()
				rt := UserResourceType()
				assert.False(t, rt.Extensions[0].Required)
				assert.Equal(t, "userName", rt.Schema.Attributes[0].Name)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mutate()
			tt.check(t)
		})
	}
}

// findSubAttr returns the named sub-attribute of the named attribute, or
// panics — test helper for reaching into known built-in schema shapes.
func findSubAttr(s Schema, attr, sub string) *Attribute {
	for i := range s.Attributes {
		if s.Attributes[i].Name != attr {
			continue
		}
		for j := range s.Attributes[i].SubAttributes {
			if s.Attributes[i].SubAttributes[j].Name == sub {
				return &s.Attributes[i].SubAttributes[j]
			}
		}
	}
	panic("sub-attribute not found: " + attr + "." + sub)
}

func TestBuiltinResourceTypes(t *testing.T) {
	tests := []struct {
		name         string
		rt           ResourceType
		wantEndpoint string
		wantSchemaID string
		wantExtIDs   []string
	}{
		{
			name:         "user",
			rt:           UserResourceType(),
			wantEndpoint: "/Users",
			wantSchemaID: SchemaUser,
			wantExtIDs:   []string{SchemaEnterpriseUser},
		},
		{
			name:         "group",
			rt:           GroupResourceType(),
			wantEndpoint: "/Groups",
			wantSchemaID: SchemaGroup,
			wantExtIDs:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantEndpoint, tt.rt.Endpoint)
			assert.Equal(t, tt.wantSchemaID, tt.rt.Schema.ID)
			var extIDs []string
			for _, e := range tt.rt.Extensions {
				extIDs = append(extIDs, e.Schema.ID)
			}
			assert.Equal(t, tt.wantExtIDs, extIDs)
		})
	}
}
