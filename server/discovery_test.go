package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

func TestServiceProviderConfig(t *testing.T) {
	h := newHarness(t, patch.Default)
	resp := h.do(http.MethodGet, "/ServiceProviderConfig", "", nil)
	require.Equal(t, http.StatusOK, resp.status, "%s", resp.body)

	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.body, &doc))

	// Capability flags reflect the actual wiring.
	assert.JSONEq(t, `{"supported":true}`, string(doc["patch"]))
	assert.JSONEq(t, `{"supported":false}`, string(doc["changePassword"]))
	assert.JSONEq(t, `{"supported":true}`, string(doc["sort"]))
	assert.JSONEq(t, `{"supported":true}`, string(doc["etag"]))
	assert.JSONEq(t, `{"supported":true,"maxResults":100}`, string(doc["filter"]))
	assert.Contains(t, string(doc["bulk"]), `"supported":false`)

	// authenticationSchemes is empty when no auth is configured.
	var schemes []json.RawMessage
	require.NoError(t, json.Unmarshal(doc["authenticationSchemes"], &schemes))
	assert.Empty(t, schemes)
}

func TestServiceProviderConfigWithAuth(t *testing.T) {
	verify := func(_ context.Context, _ string) error { return nil }
	h := newHarnessWith(t, patch.Default, server.BearerToken(verify), true)
	resp := h.do(http.MethodGet, "/ServiceProviderConfig", "", nil)
	require.Equal(t, http.StatusOK, resp.status, "%s", resp.body)

	// A configured auth scheme is advertised, and the document then
	// satisfies every required attribute of the vendored §8.7.2
	// ServiceProviderConfig meta-schema.
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(resp.body, &doc))
	assert.Contains(t, string(doc["authenticationSchemes"]), "oauthbearertoken")
	assertValidatesAgainst(t, resp.body, metaResourceType(
		"ServiceProviderConfig", "/ServiceProviderConfig", scim.ServiceProviderConfigSchema()))
}

func TestResourceTypesDiscovery(t *testing.T) {
	h := newHarness(t, patch.Default)

	list := h.do(http.MethodGet, "/ResourceTypes", "", nil)
	require.Equal(t, http.StatusOK, list.status)
	var lr scim.ListResponse
	require.NoError(t, lr.UnmarshalJSON(list.body))
	assert.Equal(t, 2, lr.TotalResults, "User and Group are registered")

	one := h.do(http.MethodGet, "/ResourceTypes/User", "", nil)
	require.Equal(t, http.StatusOK, one.status, "%s", one.body)
	assertValidatesAgainst(t, one.body, metaResourceType(
		"ResourceType", "/ResourceTypes", scim.ResourceTypeSchema()))

	missing := h.do(http.MethodGet, "/ResourceTypes/Nonexistent", "", nil)
	assert.Equal(t, http.StatusNotFound, missing.status)
}

func TestSchemasDiscovery(t *testing.T) {
	h := newHarness(t, patch.Default)

	list := h.do(http.MethodGet, "/Schemas", "", nil)
	require.Equal(t, http.StatusOK, list.status)
	var lr scim.ListResponse
	require.NoError(t, lr.UnmarshalJSON(list.body))
	// User (+ Enterprise extension) and Group core schemas.
	assert.GreaterOrEqual(t, lr.TotalResults, 3)

	one := h.do(http.MethodGet, "/Schemas/"+scim.SchemaUser, "", nil)
	require.Equal(t, http.StatusOK, one.status, "%s", one.body)
	// The Schema representation nests complex-within-complex
	// (attributes.subAttributes) — the RFC 7643 §2.3.1 exception the
	// decoder rightly forbids for ordinary resources — so it is validated
	// structurally against the meta-schema's required attributes.
	assertSchemaDoc(t, one.body)
}

// assertSchemaDoc checks a Schema representation carries the attributes
// the §8.7.2 Schema meta-schema requires: id, name, and a non-empty
// attributes array whose elements each declare a name and type.
func assertSchemaDoc(t *testing.T, body []byte) {
	t.Helper()
	var doc struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Attributes []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"attributes"`
	}
	require.NoError(t, json.Unmarshal(body, &doc))
	assert.Equal(t, scim.SchemaUser, doc.ID)
	assert.NotEmpty(t, doc.Name)
	require.NotEmpty(t, doc.Attributes, "schema declares attributes")
	for _, a := range doc.Attributes {
		assert.NotEmpty(t, a.Name, "attribute has a name")
		assert.NotEmpty(t, a.Type, "attribute has a type")
	}
}

func TestDiscoveryRejectsFilter(t *testing.T) {
	h := newHarness(t, patch.Default)
	tests := []string{
		"/ServiceProviderConfig?filter=" + url("id pr"),
		"/ResourceTypes?filter=" + url("name eq \"User\""),
		"/Schemas?filter=" + url("id pr"),
	}
	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			resp := h.do(http.MethodGet, path, "", nil)
			assert.Equal(t, http.StatusForbidden, resp.status, "%s", resp.body)
		})
	}
}

// metaResourceType wraps a §8.7.2 meta-schema in a resource type so a
// discovery document can be decoded and validated against it.
func metaResourceType(name, endpoint string, schema scim.Schema) *scim.ResourceType {
	return &scim.ResourceType{Name: name, Endpoint: endpoint, Schema: schema.WithDefaults()}
}

// assertValidatesAgainst decodes a discovery document against its
// meta-schema and asserts every required top-level attribute is present —
// the structural half of RFC 7643 §7 conformance.
func assertValidatesAgainst(t *testing.T, body []byte, rt *scim.ResourceType) {
	t.Helper()
	res, err := scim.DecodeResource(body, rt)
	require.NoError(t, err, "decode against meta-schema: %s", body)
	for i := range rt.Schema.Attributes {
		attr := &rt.Schema.Attributes[i]
		if !attr.Required {
			continue
		}
		_, ok := res.Get(attr.Name)
		assert.Truef(t, ok, "required meta-schema attribute %q present", attr.Name)
	}
}
