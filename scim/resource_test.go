package scim

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUserResource(t *testing.T) *Resource {
	t.Helper()
	rt := UserResourceType()
	return NewResource(&rt)
}

const enterpriseAttr = SchemaEnterpriseUser + ":employeeNumber"

// TestResourcePresenceSemantics pins the RFC 7643 §2.5 rule that JSON null,
// empty arrays, and absent keys are one state: unassigned.
func TestResourcePresenceSemantics(t *testing.T) {
	tests := []struct {
		name string
		path string
		// steps run in order; presence is checked after the last one.
		steps        func(t *testing.T, r *Resource)
		wantAssigned bool
		wantValue    any
	}{
		{
			name:         "never set is unassigned",
			path:         "userName",
			steps:        func(*testing.T, *Resource) {},
			wantAssigned: false,
		},
		{
			name: "set then present",
			path: "userName",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("userName", "bjensen"))
			},
			wantAssigned: true,
			wantValue:    "bjensen",
		},
		{
			name: "set nil unassigns",
			path: "userName",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("userName", "bjensen"))
				require.NoError(t, r.Set("userName", nil))
			},
			wantAssigned: false,
		},
		{
			name: "unassign removes",
			path: "userName",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("userName", "bjensen"))
				require.NoError(t, r.Unassign("userName"))
			},
			wantAssigned: false,
		},
		{
			name: "unassign when already absent is a no-op",
			path: "userName",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Unassign("userName"))
			},
			wantAssigned: false,
		},
		{
			name: "empty multi-value canonicalizes to unassigned",
			path: "emails",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("emails", []any{map[string]any{"value": "a@b.c"}}))
				require.NoError(t, r.Set("emails", []any{}))
			},
			wantAssigned: false,
		},
		{
			name: "empty complex canonicalizes to unassigned",
			path: "name",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("name", map[string]any{}))
			},
			wantAssigned: false,
		},
		{
			name: "unassigning last sub-attribute deletes the parent",
			path: "name",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("name.givenName", "Barbara"))
				require.NoError(t, r.Unassign("name.givenName"))
			},
			wantAssigned: false,
		},
		{
			name: "sibling sub-attribute keeps the parent",
			path: "name",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("name.givenName", "Barbara"))
				require.NoError(t, r.Set("name.familyName", "Jensen"))
				require.NoError(t, r.Unassign("name.givenName"))
			},
			wantAssigned: true,
			wantValue:    ComplexValue{"familyname": "Jensen"},
		},
		{
			name: "unassigning last extension attribute drops the container",
			path: SchemaEnterpriseUser,
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set(enterpriseAttr, "701984"))
				require.NoError(t, r.Unassign(enterpriseAttr))
			},
			wantAssigned: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newUserResource(t)
			tt.steps(t, r)
			got, ok := r.Get(tt.path)
			assert.Equal(t, tt.wantAssigned, ok)
			if tt.wantValue != nil {
				assert.Equal(t, tt.wantValue, got)
			}
		})
	}
}

func TestResourceGetSet(t *testing.T) {
	tests := []struct {
		name    string
		setPath string
		set     any
		getPath string
		want    any
	}{
		{name: "string", setPath: "userName", set: "bjensen", getPath: "USERNAME", want: "bjensen"},
		{name: "boolean", setPath: "active", set: true, getPath: "active", want: true},
		{
			name:    "complex sub-attribute",
			setPath: "name.givenName",
			set:     "Barbara",
			getPath: "name.GIVENNAME",
			want:    "Barbara",
		},
		{
			name:    "whole complex",
			setPath: "name",
			set:     map[string]any{"givenName": "Barbara", "familyName": "Jensen"},
			getPath: "name.familyName",
			want:    "Jensen",
		},
		{
			name:    "multi-valued complex",
			setPath: "emails",
			set: []any{
				map[string]any{"value": "bjensen@example.com", "type": "work", "primary": true},
			},
			getPath: "emails",
			want: MultiComplexValue{
				{"value": "bjensen@example.com", "type": "work", "primary": true},
			},
		},
		{
			name:    "extension attribute",
			setPath: enterpriseAttr,
			set:     "701984",
			getPath: foldName(enterpriseAttr),
			want:    "701984",
		},
		{
			name:    "extension sub-attribute",
			setPath: SchemaEnterpriseUser + ":manager.displayName",
			set:     "John Smith",
			getPath: SchemaEnterpriseUser + ":manager.displayName",
			want:    "John Smith",
		},
		{
			name:    "whole extension container",
			setPath: SchemaEnterpriseUser,
			set: map[string]any{
				"employeeNumber": "701984",
				"manager":        map[string]any{"displayName": "John Smith"},
			},
			getPath: enterpriseAttr,
			want:    "701984",
		},
		{name: "common id", setPath: "id", set: "2819c223", getPath: "id", want: "2819c223"},
		{name: "common externalId", setPath: "externalId", set: "701984", getPath: "EXTERNALID", want: "701984"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newUserResource(t)
			require.NoError(t, r.Set(tt.setPath, tt.set))
			got, ok := r.Get(tt.getPath)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceSetErrors(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		value        any
		wantScimType string
	}{
		{name: "unknown attribute", path: "nope", value: "x", wantScimType: ScimTypeInvalidPath},
		{name: "type mismatch", path: "userName", value: 42, wantScimType: ScimTypeInvalidValue},
		{
			name:         "two primaries",
			path:         "emails",
			value:        []any{map[string]any{"value": "a", "primary": true}, map[string]any{"value": "b", "primary": true}},
			wantScimType: ScimTypeInvalidValue,
		},
		{name: "schemas is derived", path: "schemas", value: []string{"urn:x"}, wantScimType: ScimTypeMutability},
		{name: "meta via SetMeta only", path: "meta", value: map[string]any{}, wantScimType: ScimTypeMutability},
		{
			name:         "extension container must be an object",
			path:         SchemaEnterpriseUser,
			value:        "not an object",
			wantScimType: ScimTypeInvalidValue,
		},
		{
			name:         "extension sub-value type mismatch",
			path:         SchemaEnterpriseUser,
			value:        map[string]any{"employeeNumber": 7},
			wantScimType: ScimTypeInvalidValue,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newUserResource(t)
			err := r.Set(tt.path, tt.value)
			var scimErr *Error
			require.ErrorAs(t, err, &scimErr)
			assert.Equal(t, tt.wantScimType, scimErr.ScimType)
			assert.Equal(t, 400, scimErr.Status)
		})
	}
}

// TestResourceSchemas pins automatic schemas maintenance: the core URN is
// always present and extension URNs track container contents.
func TestResourceSchemas(t *testing.T) {
	tests := []struct {
		name  string
		steps func(t *testing.T, r *Resource)
		want  []string
	}{
		{
			name:  "new resource has core schema only",
			steps: func(*testing.T, *Resource) {},
			want:  []string{SchemaUser},
		},
		{
			name: "setting an extension attribute adds its URN",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set(enterpriseAttr, "701984"))
			},
			want: []string{SchemaUser, SchemaEnterpriseUser},
		},
		{
			name: "emptying the extension removes its URN",
			steps: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set(enterpriseAttr, "701984"))
				require.NoError(t, r.Unassign(enterpriseAttr))
			},
			want: []string{SchemaUser},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newUserResource(t)
			tt.steps(t, r)
			assert.Equal(t, tt.want, r.Schemas())

			got, ok := r.Get("schemas")
			require.True(t, ok, "schemas is always assigned")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResourceMeta(t *testing.T) {
	created := time.Date(2010, 1, 23, 4, 56, 22, 0, time.UTC)
	modified := time.Date(2011, 5, 13, 4, 42, 34, 0, time.UTC)
	meta := Meta{
		ResourceType: "User",
		Created:      created,
		LastModified: modified,
		Location:     "https://example.com/v2/Users/2819c223",
		Version:      `W/"3694e05e9dff590"`,
	}
	tests := []struct {
		name     string
		meta     Meta
		getPath  string
		want     any
		assigned bool
	}{
		{name: "zero meta is unassigned", meta: Meta{}, getPath: "meta", assigned: false},
		{name: "sub-attribute of zero meta", meta: Meta{}, getPath: "meta.created", assigned: false},
		{
			name:     "projection includes assigned fields",
			meta:     meta,
			getPath:  "meta",
			assigned: true,
			want: ComplexValue{
				"resourcetype": "User",
				"created":      created,
				"lastmodified": modified,
				"location":     "https://example.com/v2/Users/2819c223",
				"version":      `W/"3694e05e9dff590"`,
			},
		},
		{name: "dateTime sub-attribute", meta: meta, getPath: "meta.lastModified", assigned: true, want: modified},
		{
			name:     "partially assigned meta omits zero fields",
			meta:     Meta{ResourceType: "User"},
			getPath:  "meta",
			assigned: true,
			want:     ComplexValue{"resourcetype": "User"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newUserResource(t)
			r.SetMeta(tt.meta)
			assert.Equal(t, tt.meta, r.Meta())

			got, ok := r.Get(tt.getPath)
			require.Equal(t, tt.assigned, ok)
			if tt.assigned {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// TestResourceClone verifies deep-copy isolation for every mutable stored
// form: mutating the clone must never show through the original.
func TestResourceClone(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, r *Resource)
		mutate func(t *testing.T, clone *Resource)
		verify func(t *testing.T, original *Resource)
	}{
		{
			name: "complex value",
			setup: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("name.givenName", "Barbara"))
			},
			mutate: func(t *testing.T, clone *Resource) {
				t.Helper()
				require.NoError(t, clone.Set("name.givenName", "Changed"))
			},
			verify: func(t *testing.T, original *Resource) {
				t.Helper()
				got, ok := original.Get("name.givenName")
				require.True(t, ok)
				assert.Equal(t, "Barbara", got)
			},
		},
		{
			name: "multi-valued complex element",
			setup: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set("emails", []any{map[string]any{"value": "a@b.c"}}))
			},
			mutate: func(t *testing.T, clone *Resource) {
				t.Helper()
				v, ok := clone.Get("emails")
				require.True(t, ok)
				v.(MultiComplexValue)[0]["value"] = "hacked"
			},
			verify: func(t *testing.T, original *Resource) {
				t.Helper()
				v, ok := original.Get("emails")
				require.True(t, ok)
				assert.Equal(t, "a@b.c", v.(MultiComplexValue)[0]["value"])
			},
		},
		{
			name: "extension container",
			setup: func(t *testing.T, r *Resource) {
				t.Helper()
				require.NoError(t, r.Set(enterpriseAttr, "701984"))
			},
			mutate: func(t *testing.T, clone *Resource) {
				t.Helper()
				require.NoError(t, clone.Set(enterpriseAttr, "changed"))
			},
			verify: func(t *testing.T, original *Resource) {
				t.Helper()
				got, ok := original.Get(enterpriseAttr)
				require.True(t, ok)
				assert.Equal(t, "701984", got)
			},
		},
		{
			name: "meta is copied",
			setup: func(t *testing.T, r *Resource) {
				t.Helper()
				r.SetMeta(Meta{Version: "v1"})
			},
			mutate: func(t *testing.T, clone *Resource) {
				t.Helper()
				clone.SetMeta(Meta{Version: "v2"})
			},
			verify: func(t *testing.T, original *Resource) {
				t.Helper()
				assert.Equal(t, "v1", original.Meta().Version)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newUserResource(t)
			tt.setup(t, r)
			clone := r.Clone()
			tt.mutate(t, clone)
			tt.verify(t, r)
			assert.Same(t, r.ResourceType(), clone.ResourceType(), "ResourceType is shared")
		})
	}
}

func TestCloneValueSlices(t *testing.T) {
	tests := []struct {
		name string
		in   any
	}{
		{name: "strings", in: []string{"a"}},
		{name: "bools", in: []bool{true}},
		{name: "int64s", in: []int64{1}},
		{name: "float64s", in: []float64{1.5}},
		{name: "times", in: []time.Time{time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}},
		{name: "references", in: []ReferenceValue{"u"}},
		{name: "bytes", in: []byte("hi")},
		{name: "byte slices", in: [][]byte{[]byte("hi")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cloneValue(tt.in)
			assert.Equal(t, tt.in, got)
			assert.NotSame(t, &tt.in, &got)
		})
	}
}

// TestResourceLookup pins the Get/Lookup contrast: Get collapses unknown
// paths into unassigned, Lookup surfaces them as ErrInvalidPath.
func TestResourceLookup(t *testing.T) {
	r := newUserResource(t)
	require.NoError(t, r.Set("userName", "bjensen"))
	require.NoError(t, r.Set(SchemaEnterpriseUser+":employeeNumber", "701984"))
	tests := []struct {
		name         string
		path         string
		want         any
		wantAssigned bool
		wantErr      bool
	}{
		{name: "assigned", path: "userName", want: "bjensen", wantAssigned: true},
		{name: "assigned folds case", path: "USERNAME", want: "bjensen", wantAssigned: true},
		{name: "unassigned", path: "displayName"},
		{name: "unassigned sub-attribute", path: "name.givenName"},
		{name: "bare extension URN", path: SchemaEnterpriseUser, want: map[string]any{"employeenumber": "701984"}, wantAssigned: true},
		{name: "unknown attribute", path: "noSuchAttr", wantErr: true},
		{name: "unknown extension", path: "urn:example:ext:x", wantErr: true},
		{name: "sub-attribute of non-complex", path: "userName.x", wantErr: true},
		{name: "empty", path: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, assigned, err := r.Lookup(tt.path)
			gotGet, assignedGet := r.Get(tt.path)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidPath(""))
				assert.False(t, assigned)
				assert.Nil(t, got)
				assert.False(t, assignedGet, "Get reads unknown paths as unassigned")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAssigned, assigned)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, assigned, assignedGet)
			assert.Equal(t, got, gotGet)
		})
	}
}

// TestResourceCloneShared pins the shared-leaf clone: every Set/Unassign
// on the copy leaves the original untouched, the containers Set edits in
// place are copied, and multi-valued leaves are shared by reference.
func TestResourceCloneShared(t *testing.T) {
	seed := func(t *testing.T, r *Resource) {
		t.Helper()
		require.NoError(t, r.Set("userName", "bjensen"))
		require.NoError(t, r.Set("name", map[string]any{"givenName": "Barbara", "familyName": "Jensen"}))
		require.NoError(t, r.Set("emails", []any{map[string]any{"value": "a@b.c", "type": "work"}}))
		require.NoError(t, r.Set("x509Certificates", []any{map[string]any{"value": []byte{1, 2}}}))
		require.NoError(t, r.Set(enterpriseAttr, "701984"))
		require.NoError(t, r.Set(SchemaEnterpriseUser+":manager", map[string]any{"value": "m1"}))
	}
	tests := []struct {
		name   string
		mutate func(t *testing.T, c *Resource)
	}{
		{name: "replace scalar", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Set("userName", "changed"))
		}},
		{name: "unassign scalar", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Unassign("userName"))
		}},
		{name: "set sub-attribute in place", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Set("name.givenName", "changed"))
		}},
		{name: "unassign sub-attribute in place", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Unassign("name.familyName"))
		}},
		{name: "replace multi-valued", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Set("emails", []any{map[string]any{"value": "z@z.z"}}))
		}},
		{name: "unassign multi-valued", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Unassign("emails"))
		}},
		{name: "extension scalar", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Set(enterpriseAttr, "changed"))
		}},
		{name: "extension sub-attribute in place", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Set(SchemaEnterpriseUser+":manager.displayName", "Boss"))
		}},
		{name: "drop whole extension", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			require.NoError(t, c.Unassign(SchemaEnterpriseUser))
		}},
		{name: "meta", mutate: func(t *testing.T, c *Resource) {
			t.Helper()
			c.SetMeta(Meta{Version: "v2"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newUserResource(t)
			seed(t, r)
			r.SetMeta(Meta{Version: "v1"})
			snapshot := r.Clone()
			c := r.CloneShared()
			tt.mutate(t, c)
			assert.Equal(t, snapshot, r, "original must not change")
			assert.NotEqual(t, snapshot, c)
			assert.Same(t, r.ResourceType(), c.ResourceType())
		})
	}

	t.Run("multi-valued leaves are shared", func(t *testing.T) {
		r := newUserResource(t)
		seed(t, r)
		c := r.CloneShared()
		orig, _ := r.Get("emails")
		copied, _ := c.Get("emails")
		assert.Same(t, &orig.(MultiComplexValue)[0], &copied.(MultiComplexValue)[0])
	})
}
