package scim

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testUserBody = `{
	"schemas": [
		"urn:ietf:params:scim:schemas:core:2.0:User",
		"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
	],
	"id": "2819c223",
	"externalId": "701984",
	"userName": "bjensen@example.com",
	"name": {"givenName": "Barbara", "familyName": "Jensen"},
	"emails": [{"value": "bjensen@example.com", "type": "work", "primary": true}],
	"password": "t1meMa$heen",
	"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User": {
		"employeeNumber": "701984",
		"manager": {"displayName": "John Smith"}
	},
	"meta": {
		"resourceType": "User",
		"created": "2010-01-23T04:56:22Z",
		"lastModified": "2011-05-13T04:42:34Z",
		"version": "W/\"a330bc54f0671c9\""
	}
}`

func TestDecodeResource(t *testing.T) {
	rt := UserResourceType()
	tests := []struct {
		name    string
		body    string
		getPath string
		want    any
	}{
		{name: "core attribute", body: testUserBody, getPath: "userName", want: "bjensen@example.com"},
		{name: "complex sub-attribute", body: testUserBody, getPath: "name.givenName", want: "Barbara"},
		{
			name:    "multi-valued complex",
			body:    testUserBody,
			getPath: "emails",
			want: MultiComplexValue{
				{"value": "bjensen@example.com", "type": "work", "primary": true},
			},
		},
		{name: "extension attribute", body: testUserBody, getPath: enterpriseAttr, want: "701984"},
		{
			name:    "extension sub-attribute",
			body:    testUserBody,
			getPath: SchemaEnterpriseUser + ":manager.displayName",
			want:    "John Smith",
		},
		{name: "common id", body: testUserBody, getPath: "id", want: "2819c223"},
		{name: "common externalId", body: testUserBody, getPath: "externalId", want: "701984"},
		{
			name:    "keys fold case-insensitively",
			body:    `{"USERNAME": "bjensen", "NAME": {"GIVENNAME": "Barbara"}}`,
			getPath: "name.givenName",
			want:    "Barbara",
		},
		{
			name:    "extension URN key folds too",
			body:    `{"URN:IETF:PARAMS:SCIM:SCHEMAS:EXTENSION:ENTERPRISE:2.0:USER": {"EMPLOYEENUMBER": "7"}}`,
			getPath: enterpriseAttr,
			want:    "7",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := DecodeResource([]byte(tt.body), &rt)
			require.NoError(t, err)
			got, ok := r.Get(tt.getPath)
			require.True(t, ok, "attribute should be assigned")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDecodeResourceMeta(t *testing.T) {
	rt := UserResourceType()
	r, err := DecodeResource([]byte(testUserBody), &rt)
	require.NoError(t, err)
	assert.Equal(t, Meta{
		ResourceType: "User",
		Created:      time.Date(2010, 1, 23, 4, 56, 22, 0, time.UTC),
		LastModified: time.Date(2011, 5, 13, 4, 42, 34, 0, time.UTC),
		Version:      `W/"a330bc54f0671c9"`,
	}, r.Meta())
}

func TestDecodeResourceTolerance(t *testing.T) {
	rt := UserResourceType()
	tests := []struct {
		name    string
		body    string
		getPath string
		// assigned=false asserts the path decoded to unassigned.
		assigned bool
		want     any
	}{
		{
			name:     "unknown attributes ignored",
			body:     `{"userName": "b", "favoriteColor": "teal"}`,
			getPath:  "userName",
			assigned: true,
			want:     "b",
		},
		{
			name:    "unknown extension URN ignored",
			body:    `{"urn:example:params:scim:schemas:extension:custom:2.0:User": {"a": 1}}`,
			getPath: "userName",
		},
		{
			name:    "schemas is not data",
			body:    `{"schemas": ["urn:totally:bogus"]}`,
			getPath: "userName",
		},
		{name: "null is unassigned", body: `{"userName": null}`, getPath: "userName"},
		{name: "empty array is unassigned", body: `{"emails": []}`, getPath: "emails"},
		{name: "null extension is unassigned", body: `{"` + SchemaEnterpriseUser + `": null}`, getPath: enterpriseAttr},
		{
			name:    "null sub-attribute is unassigned",
			body:    `{"name": {"givenName": null}}`,
			getPath: "name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := DecodeResource([]byte(tt.body), &rt)
			require.NoError(t, err)
			got, ok := r.Get(tt.getPath)
			require.Equal(t, tt.assigned, ok)
			if tt.assigned {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestDecodeResourceErrors(t *testing.T) {
	rt := UserResourceType()
	tests := []struct {
		name         string
		body         string
		wantScimType string
	}{
		{name: "not an object", body: `[1, 2]`, wantScimType: ScimTypeInvalidSyntax},
		{name: "invalid json", body: `{`, wantScimType: ScimTypeInvalidSyntax},
		{name: "trailing data", body: `{} {}`, wantScimType: ScimTypeInvalidSyntax},
		{name: "string attr type mismatch", body: `{"userName": 42}`, wantScimType: ScimTypeInvalidValue},
		{name: "boolean attr type mismatch", body: `{"active": "yes"}`, wantScimType: ScimTypeInvalidValue},
		{
			name:         "extension container not an object",
			body:         `{"` + SchemaEnterpriseUser + `": "nope"}`,
			wantScimType: ScimTypeInvalidSyntax,
		},
		{
			name:         "extension attr type mismatch",
			body:         `{"` + SchemaEnterpriseUser + `": {"employeeNumber": 7}}`,
			wantScimType: ScimTypeInvalidValue,
		},
		{name: "meta not an object", body: `{"meta": "nope"}`, wantScimType: ScimTypeInvalidSyntax},
		{name: "meta bad dateTime", body: `{"meta": {"created": "not-a-date"}}`, wantScimType: ScimTypeInvalidValue},
		{
			name:         "two primaries",
			body:         `{"emails": [{"value": "a", "primary": true}, {"value": "b", "primary": true}]}`,
			wantScimType: ScimTypeInvalidValue,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeResource([]byte(tt.body), &rt)
			var scimErr *Error
			require.ErrorAs(t, err, &scimErr)
			assert.Equal(t, tt.wantScimType, scimErr.ScimType)
		})
	}
}

func TestDecodeResourceNumberTyping(t *testing.T) {
	schema := Schema{
		ID:   "urn:example:2.0:Widget",
		Name: "Widget",
		Attributes: []Attribute{
			{Name: "count", Type: TypeInteger},
			{Name: "ratio", Type: TypeDecimal},
		},
	}.WithDefaults()
	rt := ResourceType{Name: "Widget", Endpoint: "/Widgets", Schema: schema}
	tests := []struct {
		name    string
		body    string
		getPath string
		want    any
		wantErr bool
	}{
		{name: "integer stays int64", body: `{"count": 42}`, getPath: "count", want: int64(42)},
		{name: "decimal stays float64", body: `{"ratio": 0.5}`, getPath: "ratio", want: 0.5},
		{name: "whole-number decimal stays float64", body: `{"ratio": 2}`, getPath: "ratio", want: 2.0},
		{name: "fractional integer rejected", body: `{"count": 4.2}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := DecodeResource([]byte(tt.body), &rt)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			got, ok := r.Get(tt.getPath)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestDecodeResourceDropReadOnly pins RFC 7644 §3.3: readOnly data in
// client-supplied bodies is silently ignored, never an error.
func TestDecodeResourceDropReadOnly(t *testing.T) {
	rt := UserResourceType()
	body := `{
		"id": "client-chosen-id",
		"userName": "bjensen",
		"externalId": "701984",
		"groups": [{"value": "g1"}],
		"meta": {"resourceType": "User", "version": "W/\"fake\""}
	}`
	tests := []struct {
		name     string
		getPath  string
		assigned bool
		want     any
	}{
		{name: "id dropped", getPath: "id"},
		{name: "readOnly groups dropped", getPath: "groups"},
		{name: "userName kept", getPath: "userName", assigned: true, want: "bjensen"},
		{name: "readWrite externalId kept", getPath: "externalId", assigned: true, want: "701984"},
	}
	r, err := DecodeResource([]byte(body), &rt, DropReadOnly())
	require.NoError(t, err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := r.Get(tt.getPath)
			require.Equal(t, tt.assigned, ok)
			if tt.assigned {
				assert.Equal(t, tt.want, got)
			}
		})
	}
	t.Run("meta dropped", func(t *testing.T) {
		assert.Equal(t, Meta{}, r.Meta())
	})
}

// TestEncodeResourceGolden pins the full wire form: declared casing
// everywhere, deterministic field order (schemas, id, externalId, core
// attributes in declaration order, extensions, meta last), and
// returned=never attributes (password) stripped.
func TestEncodeResourceGolden(t *testing.T) {
	rt := UserResourceType()
	r, err := DecodeResource([]byte(testUserBody), &rt)
	require.NoError(t, err)

	got, err := EncodeResource(r, Projection{})
	require.NoError(t, err)

	want := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User",` +
		`"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"],` +
		`"id":"2819c223","externalId":"701984","userName":"bjensen@example.com",` +
		`"name":{"familyName":"Jensen","givenName":"Barbara"},` +
		`"emails":[{"primary":true,"type":"work","value":"bjensen@example.com"}],` +
		`"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User":` +
		`{"employeeNumber":"701984","manager":{"displayName":"John Smith"}},` +
		`"meta":{"created":"2010-01-23T04:56:22Z","lastModified":"2011-05-13T04:42:34Z",` +
		`"resourceType":"User","version":"W/\"a330bc54f0671c9\""}}`
	assert.Equal(t, want, string(got))

	again, err := EncodeResource(r, Projection{})
	require.NoError(t, err)
	assert.Equal(t, string(got), string(again), "encoding is deterministic")
}

func TestEncodeResourceProjection(t *testing.T) {
	rt := UserResourceType()
	r, err := DecodeResource([]byte(testUserBody), &rt)
	require.NoError(t, err)

	tests := []struct {
		name        string
		projection  Projection
		wantPresent []string
		wantAbsent  []string
	}{
		{
			name:        "default emits returned=default and strips returned=never",
			projection:  Projection{},
			wantPresent: []string{`"userName"`, `"emails"`, `"meta"`},
			wantAbsent:  []string{`"password"`},
		},
		{
			name:        "attributes keeps requested plus returned=always",
			projection:  Projection{Attributes: []string{"userName"}},
			wantPresent: []string{`"schemas"`, `"id"`, `"userName"`},
			wantAbsent:  []string{`"emails"`, `"name"`, `"meta"`, `"externalId"`},
		},
		{
			name:        "attributes sub-path emits partial complex",
			projection:  Projection{Attributes: []string{"name.givenName"}},
			wantPresent: []string{`"name":{"givenName":"Barbara"}`},
			wantAbsent:  []string{`"familyName"`},
		},
		{
			name:        "requesting returned=never is still stripped",
			projection:  Projection{Attributes: []string{"password"}},
			wantAbsent:  []string{`"password"`},
			wantPresent: []string{`"id"`},
		},
		{
			name:        "excludedAttributes drops from the default set",
			projection:  Projection{ExcludedAttributes: []string{"emails", "meta"}},
			wantPresent: []string{`"userName"`, `"name"`},
			wantAbsent:  []string{`"emails"`, `"meta"`},
		},
		{
			name:        "excluding returned=always has no effect",
			projection:  Projection{ExcludedAttributes: []string{"id"}},
			wantPresent: []string{`"id"`},
		},
		{
			name:        "excluded sub-path drops only that sub-attribute",
			projection:  Projection{ExcludedAttributes: []string{"name.familyName"}},
			wantPresent: []string{`"name":{"givenName":"Barbara"}`},
			wantAbsent:  []string{`"familyName"`},
		},
		{
			name:        "extension attribute via URN path",
			projection:  Projection{Attributes: []string{enterpriseAttr}},
			wantPresent: []string{`"employeeNumber"`},
			wantAbsent:  []string{`"manager"`, `"userName"`},
		},
		{
			name:        "unresolvable projection paths are ignored",
			projection:  Projection{Attributes: []string{"noSuchAttr", "userName"}},
			wantPresent: []string{`"userName"`},
		},
		{
			name:        "bare extension URN emits the whole container",
			projection:  Projection{Attributes: []string{SchemaEnterpriseUser}},
			wantPresent: []string{`"employeeNumber"`, `"manager"`, `"` + SchemaEnterpriseUser + `":{`},
			wantAbsent:  []string{`"userName"`},
		},
		{
			name:        "bare extension URN folds case",
			projection:  Projection{Attributes: []string{strings.ToUpper(SchemaEnterpriseUser)}},
			wantPresent: []string{`"employeeNumber"`},
			wantAbsent:  []string{`"userName"`},
		},
		{
			name:        "excluded bare extension URN drops the container",
			projection:  Projection{ExcludedAttributes: []string{SchemaEnterpriseUser}},
			wantPresent: []string{`"userName"`},
			wantAbsent:  []string{`"employeeNumber"`, `"` + SchemaEnterpriseUser + `":{`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeResource(r, tt.projection)
			require.NoError(t, err)
			for _, want := range tt.wantPresent {
				assert.Contains(t, string(got), want)
			}
			for _, notWant := range tt.wantAbsent {
				assert.NotContains(t, string(got), notWant)
			}
		})
	}
}

// TestCodecRoundTrip pins decode → encode → decode stability: the second
// decode must see exactly the first resource. returned=never attributes
// (password) are excluded — they never appear on the wire, so they cannot
// round-trip by design.
func TestCodecRoundTrip(t *testing.T) {
	rt := UserResourceType()
	fullUser := strings.Replace(testUserBody, `"password": "t1meMa$heen",`, "", 1)
	tests := []struct {
		name string
		body string
	}{
		{name: "full user", body: fullUser},
		{name: "minimal user", body: `{"userName": "b"}`},
		{name: "extension only", body: `{"` + SchemaEnterpriseUser + `": {"employeeNumber": "7"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := DecodeResource([]byte(tt.body), &rt)
			require.NoError(t, err)
			wire, err := EncodeResource(first, Projection{})
			require.NoError(t, err)
			second, err := DecodeResource(wire, &rt)
			require.NoError(t, err)

			assert.Empty(t, cmp.Diff(first.attrs, second.attrs))
			assert.Equal(t, first.Meta(), second.Meta())
			assert.Equal(t, first.Schemas(), second.Schemas())
		})
	}
}

// TestEncodeResourceViaJSONMarshal proves json.Marshal integration uses the
// default projection.
func TestEncodeResourceViaJSONMarshal(t *testing.T) {
	rt := UserResourceType()
	r, err := DecodeResource([]byte(`{"userName": "b", "password": "secret"}`), &rt)
	require.NoError(t, err)

	direct, err := EncodeResource(r, Projection{})
	require.NoError(t, err)
	viaMarshal, err := r.MarshalJSON()
	require.NoError(t, err)
	assert.Equal(t, string(direct), string(viaMarshal))
	assert.NotContains(t, string(viaMarshal), "secret")
}

// TestEncodeDateTimePrecision pins that dateTime values keep sub-second
// precision on the wire and survive a decode → encode → decode round trip
// unchanged; whole-second instants still render as plain RFC 3339.
func TestEncodeDateTimePrecision(t *testing.T) {
	rt := UserResourceType()
	tests := []struct {
		name     string
		created  string
		wantWire string
	}{
		{name: "whole seconds", created: "2011-05-13T04:42:34Z", wantWire: `"created":"2011-05-13T04:42:34Z"`},
		{name: "milliseconds", created: "2011-05-13T04:42:34.123Z", wantWire: `"created":"2011-05-13T04:42:34.123Z"`},
		{
			name:     "nanoseconds",
			created:  "2011-05-13T04:42:34.123456789Z",
			wantWire: `"created":"2011-05-13T04:42:34.123456789Z"`,
		},
		{name: "offset preserved", created: "2011-05-13T04:42:34.5+02:00", wantWire: `"created":"2011-05-13T04:42:34.5+02:00"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"userName": "b", "meta": {"created": "` + tt.created + `"}}`
			first, err := DecodeResource([]byte(body), &rt)
			require.NoError(t, err)
			wire, err := EncodeResource(first, Projection{})
			require.NoError(t, err)
			assert.Contains(t, string(wire), tt.wantWire)
			second, err := DecodeResource(wire, &rt)
			require.NoError(t, err)
			assert.True(t, first.Meta().Created.Equal(second.Meta().Created))
			assert.Equal(t, first.Meta().Created.Nanosecond(), second.Meta().Created.Nanosecond())
		})
	}
}
