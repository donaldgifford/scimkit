package scim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePath(t *testing.T) {
	rt := UserResourceType()
	tests := []struct {
		name string
		path string
		// wantAttr/wantSub assert declared casing: resolution accepts any
		// casing but must hand back schema-declared names so wire output
		// never leaks folded names.
		wantURN    string
		wantAttr   string
		wantSub    string
		wantCommon bool
		wantErr    string
	}{
		{name: "simple attribute", path: "userName", wantAttr: "userName"},
		{name: "case-insensitive lookup emits declared casing", path: "USERNAME", wantAttr: "userName"},
		{name: "sub-attribute", path: "name.givenName", wantAttr: "name", wantSub: "givenName"},
		{name: "sub-attribute folded", path: "NAME.GIVENNAME", wantAttr: "name", wantSub: "givenName"},
		{
			name:     "core URN prefix",
			path:     "urn:ietf:params:scim:schemas:core:2.0:User:userName",
			wantAttr: "userName",
		},
		{
			name:     "core URN prefix case-insensitive",
			path:     "URN:IETF:PARAMS:SCIM:SCHEMAS:CORE:2.0:USER:USERNAME",
			wantAttr: "userName",
		},
		{
			name:     "extension attribute",
			path:     "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber",
			wantURN:  SchemaEnterpriseUser,
			wantAttr: "employeeNumber",
		},
		{
			name:     "extension sub-attribute",
			path:     "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:manager.displayName",
			wantURN:  SchemaEnterpriseUser,
			wantAttr: "manager",
			wantSub:  "displayName",
		},
		{
			name:    "bare extension URN is the container",
			path:    "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User",
			wantURN: SchemaEnterpriseUser,
		},
		{name: "common attribute id", path: "id", wantAttr: "id", wantCommon: true},
		{name: "common attribute externalId", path: "ExternalId", wantAttr: "externalId", wantCommon: true},
		{name: "meta sub-attribute", path: "meta.lastModified", wantAttr: "meta", wantSub: "lastModified", wantCommon: true},
		{name: "schemas", path: "schemas", wantAttr: "schemas", wantCommon: true},
		{
			name:       "common attribute under core URN prefix",
			path:       "urn:ietf:params:scim:schemas:core:2.0:User:id",
			wantAttr:   "id",
			wantCommon: true,
		},
		{name: "empty path", path: "", wantErr: "empty path"},
		{name: "unknown attribute", path: "nope", wantErr: `no attribute "nope"`},
		{name: "unknown schema URN", path: "urn:example:custom:2.0:Thing:field", wantErr: "not part of resource type"},
		{name: "bare core URN", path: "urn:ietf:params:scim:schemas:core:2.0:User", wantErr: "needs an attribute name"},
		{name: "sub-attribute of non-complex", path: "userName.foo", wantErr: "is not complex"},
		{name: "unknown sub-attribute", path: "name.middle", wantErr: `no sub-attribute "middle"`},
		{name: "too deep", path: "name.given.name", wantErr: "malformed attribute expression"},
		{name: "trailing dot", path: "name.", wantErr: "malformed attribute expression"},
		{name: "leading dot", path: ".givenName", wantErr: "malformed attribute expression"},
		{
			name:    "sub-attribute of multi-valued needs value filter",
			path:    "emails.value",
			wantErr: "requires a value filter",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp, err := resolvePath(&rt, tt.path)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				var scimErr *Error
				require.ErrorAs(t, err, &scimErr)
				assert.Equal(t, ScimTypeInvalidPath, scimErr.ScimType)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantURN, rp.urn)
			assert.Equal(t, tt.wantCommon, rp.common)
			if tt.wantAttr == "" {
				assert.Nil(t, rp.attr)
			} else {
				require.NotNil(t, rp.attr)
				assert.Equal(t, tt.wantAttr, rp.attr.Name)
			}
			if tt.wantSub == "" {
				assert.Nil(t, rp.sub)
			} else {
				require.NotNil(t, rp.sub)
				assert.Equal(t, tt.wantSub, rp.sub.Name)
			}
		})
	}
}

func TestResolvePathWithoutExtensions(t *testing.T) {
	rt := GroupResourceType()
	tests := []struct {
		name     string
		path     string
		wantAttr string
		wantErr  string
	}{
		{name: "group attribute", path: "displayName", wantAttr: "displayName"},
		{
			name:    "enterprise URN not on Group",
			path:    "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber",
			wantErr: "not part of resource type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp, err := resolvePath(&rt, tt.path)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, rp.attr)
			assert.Equal(t, tt.wantAttr, rp.attr.Name)
		})
	}
}
