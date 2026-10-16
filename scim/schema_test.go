package scim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttributeWithDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   Attribute
		want Attribute
	}{
		{
			name: "all absent gets section 2.2 defaults",
			in:   Attribute{Name: "displayName"},
			want: Attribute{
				Name:       "displayName",
				Type:       TypeString,
				Mutability: MutabilityReadWrite,
				Returned:   ReturnedDefault,
				Uniqueness: UniquenessNone,
			},
		},
		{
			name: "explicit values preserved",
			in: Attribute{
				Name:       "id",
				Type:       TypeString,
				CaseExact:  true,
				Mutability: MutabilityReadOnly,
				Returned:   ReturnedAlways,
				Uniqueness: UniquenessServer,
			},
			want: Attribute{
				Name:       "id",
				Type:       TypeString,
				CaseExact:  true,
				Mutability: MutabilityReadOnly,
				Returned:   ReturnedAlways,
				Uniqueness: UniquenessServer,
			},
		},
		{
			name: "defaults recurse into sub-attributes",
			in: Attribute{
				Name:          "emails",
				Type:          TypeComplex,
				MultiValued:   true,
				SubAttributes: []Attribute{{Name: "value"}},
			},
			want: Attribute{
				Name:        "emails",
				Type:        TypeComplex,
				MultiValued: true,
				Mutability:  MutabilityReadWrite,
				Returned:    ReturnedDefault,
				Uniqueness:  UniquenessNone,
				SubAttributes: []Attribute{{
					Name:       "value",
					Type:       TypeString,
					Mutability: MutabilityReadWrite,
					Returned:   ReturnedDefault,
					Uniqueness: UniquenessNone,
				}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Schema{ID: "urn:test", Attributes: []Attribute{tt.in}}.WithDefaults()
			if diff := cmp.Diff(tt.want, s.Attributes[0]); diff != "" {
				t.Errorf("WithDefaults() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSchemaWithDefaultsDeepCopies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Schema)
	}{
		{
			name:   "mutating copied attribute leaves original intact",
			mutate: func(s *Schema) { s.Attributes[0].Name = "changed" },
		},
		{
			name:   "mutating copied sub-attribute leaves original intact",
			mutate: func(s *Schema) { s.Attributes[0].SubAttributes[0].Name = "changed" },
		},
		{
			name:   "mutating copied canonical values leaves original intact",
			mutate: func(s *Schema) { s.Attributes[0].SubAttributes[0].CanonicalValues[0] = "changed" },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := Schema{ID: "urn:test", Attributes: []Attribute{{
				Name: "emails",
				Type: TypeComplex,
				SubAttributes: []Attribute{{
					Name:            "type",
					CanonicalValues: []string{"work", "home"},
				}},
			}}}
			cp := orig.WithDefaults()
			tt.mutate(&cp)
			assert.Equal(t, "emails", orig.Attributes[0].Name)
			assert.Equal(t, "type", orig.Attributes[0].SubAttributes[0].Name)
			assert.Equal(t, "work", orig.Attributes[0].SubAttributes[0].CanonicalValues[0])
		})
	}
}

func TestSchemaValidate(t *testing.T) {
	tests := []struct {
		name    string
		schema  Schema
		wantErr string
	}{
		{
			name: "valid schema",
			schema: Schema{ID: "urn:test", Attributes: []Attribute{
				{Name: "a", Type: TypeString, Mutability: MutabilityReadWrite, Returned: ReturnedDefault, Uniqueness: UniquenessNone},
			}},
		},
		{
			name:    "missing id",
			schema:  Schema{},
			wantErr: "missing id",
		},
		{
			name: "missing attribute name",
			schema: Schema{ID: "urn:test", Attributes: []Attribute{
				{Type: TypeString, Mutability: MutabilityReadWrite, Returned: ReturnedDefault, Uniqueness: UniquenessNone},
			}},
			wantErr: "missing name",
		},
		{
			name: "invalid type",
			schema: Schema{ID: "urn:test", Attributes: []Attribute{
				{Name: "a", Type: "text", Mutability: MutabilityReadWrite, Returned: ReturnedDefault, Uniqueness: UniquenessNone},
			}},
			wantErr: `invalid type "text"`,
		},
		{
			name: "zero-value mutability is invalid without defaults",
			schema: Schema{ID: "urn:test", Attributes: []Attribute{
				{Name: "a", Type: TypeString, Returned: ReturnedDefault, Uniqueness: UniquenessNone},
			}},
			wantErr: `invalid mutability ""`,
		},
		{
			name: "sub-attributes on non-complex type",
			schema: Schema{ID: "urn:test", Attributes: []Attribute{
				{
					Name: "a", Type: TypeString,
					Mutability: MutabilityReadWrite, Returned: ReturnedDefault, Uniqueness: UniquenessNone,
					SubAttributes: []Attribute{
						{Name: "b", Type: TypeString, Mutability: MutabilityReadWrite, Returned: ReturnedDefault, Uniqueness: UniquenessNone},
					},
				},
			}},
			wantErr: "subAttributes on non-complex",
		},
		{
			name: "duplicate attribute names fold case",
			schema: Schema{ID: "urn:test", Attributes: []Attribute{
				{Name: "userName", Type: TypeString, Mutability: MutabilityReadWrite, Returned: ReturnedDefault, Uniqueness: UniquenessNone},
				{Name: "USERNAME", Type: TypeString, Mutability: MutabilityReadWrite, Returned: ReturnedDefault, Uniqueness: UniquenessNone},
			}},
			wantErr: "duplicate attribute",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schema.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseSchemaVendoredFixtures(t *testing.T) {
	tests := []struct {
		file      string
		wantID    string
		wantAttrs int
	}{
		{file: "user.json", wantID: SchemaUser, wantAttrs: 21},
		{file: "group.json", wantID: SchemaGroup, wantAttrs: 2},
		{file: "enterprise-user.json", wantID: SchemaEnterpriseUser, wantAttrs: 6},
		{file: "service-provider-config.json", wantID: SchemaServiceProviderConfig, wantAttrs: 7},
		{file: "resource-type.json", wantID: SchemaResourceType, wantAttrs: 6},
		{file: "schema.json", wantID: SchemaSchema, wantAttrs: 4},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("schemas", tt.file))
			require.NoError(t, err)
			s, err := ParseSchema(data)
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, s.ID)
			assert.Len(t, s.Attributes, tt.wantAttrs)
		})
	}
}

func TestParseSchemaErrors(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr string
	}{
		{
			name:    "invalid json",
			data:    `{`,
			wantErr: "parse schema",
		},
		{
			name:    "invalid mutability value",
			data:    `{"id":"urn:test","attributes":[{"name":"a","type":"string","mutability":"writeOnce"}]}`,
			wantErr: `invalid mutability "writeOnce"`,
		},
		{
			name:    "missing id",
			data:    `{"attributes":[]}`,
			wantErr: "missing id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSchema([]byte(tt.data))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSchemaMarshalRoundTrip(t *testing.T) {
	files := []string{
		"user.json", "group.json", "enterprise-user.json",
		"service-provider-config.json", "resource-type.json", "schema.json",
	}
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("schemas", file))
			require.NoError(t, err)
			parsed, err := ParseSchema(data)
			require.NoError(t, err)

			wire, err := json.Marshal(parsed)
			require.NoError(t, err)
			reparsed, err := ParseSchema(wire)
			require.NoError(t, err)

			if diff := cmp.Diff(parsed, reparsed); diff != "" {
				t.Errorf("Schema -> JSON -> Schema round trip mismatch (-first +second):\n%s", diff)
			}
		})
	}
}
