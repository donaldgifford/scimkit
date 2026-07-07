package scim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFoldName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "mixed case attribute", in: "userName", want: "username"},
		{name: "upper case URN", in: "URN:IETF:PARAMS:SCIM:SCHEMAS:CORE:2.0:User", want: "urn:ietf:params:scim:schemas:core:2.0:user"},
		{name: "already folded", in: "emails", want: "emails"},
		{name: "dollar ref", in: "$Ref", want: "$ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FoldName(tt.in))
			assert.Equal(t, tt.want, foldName(tt.in), "exported and internal fold must agree")
		})
	}
}

func TestCommonAttributes(t *testing.T) {
	attrs := CommonAttributes()

	tests := []struct {
		name      string
		attr      string
		wantType  AttrType
		caseExact bool
	}{
		{name: "id", attr: "id", wantType: TypeString, caseExact: true},
		{name: "externalId", attr: "externalId", wantType: TypeString, caseExact: true},
		{name: "meta", attr: "meta", wantType: TypeComplex},
		{name: "schemas", attr: "schemas", wantType: TypeString, caseExact: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := findAttribute(attrs, tt.attr)
			require.NotNil(t, attr)
			assert.Equal(t, tt.wantType, attr.Type)
			assert.Equal(t, tt.caseExact, attr.CaseExact)
		})
	}

	t.Run("meta sub-attributes carry dateTime types", func(t *testing.T) {
		meta := findAttribute(attrs, "meta")
		require.NotNil(t, meta)
		lastModified := findAttribute(meta.SubAttributes, "lastModified")
		require.NotNil(t, lastModified)
		assert.Equal(t, TypeDateTime, lastModified.Type)
	})

	t.Run("returns fresh values", func(t *testing.T) {
		first := CommonAttributes()
		first[0].Name = "mutated"
		firstMeta := findAttribute(first, "meta")
		if firstMeta != nil {
			firstMeta.SubAttributes[0].Name = "mutated"
		}

		second := CommonAttributes()
		assert.Equal(t, "id", second[0].Name)
		meta := findAttribute(second, "meta")
		require.NotNil(t, meta)
		assert.Equal(t, "resourceType", meta.SubAttributes[0].Name)
	})
}
