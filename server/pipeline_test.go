package server_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// widgetType is a custom resource type with an immutable attribute and a
// canonical-valued attribute, to exercise the write-pipeline branches the
// shipped User/Group schemas cannot reach at the top level.
func widgetType() scim.ResourceType {
	return scim.ResourceType{
		Name:     "Widget",
		Endpoint: "/Widgets",
		Schema: scim.Schema{
			ID:   "urn:example:Widget",
			Name: "Widget",
			Attributes: []scim.Attribute{
				{Name: "displayName", Type: scim.TypeString, Required: true},
				{Name: "region", Type: scim.TypeString, Mutability: scim.MutabilityImmutable},
				{Name: "tier", Type: scim.TypeString, CanonicalValues: []string{"gold", "silver", "bronze"}},
				{
					Name: "labels", Type: scim.TypeComplex, MultiValued: true,
					SubAttributes: []scim.Attribute{
						{Name: "text", Type: scim.TypeString, Required: true},
						{Name: "kind", Type: scim.TypeString, CanonicalValues: []string{"primary", "secondary"}},
					},
				},
			},
		}.WithDefaults(),
	}
}

func newWidgetHarness(t *testing.T, profile patch.Profile) *harness {
	t.Helper()
	rt := widgetType()
	h, err := server.New(server.Config{
		BaseURL:          "https://example.com/scim/v2",
		ResourceTypes:    []server.Registration{{Type: rt, Store: memstore.New(rt, memstore.WithProfile(profile))}},
		Profile:          profile,
		MaxFilterResults: 100,
		Logger:           discardLogger(),
	})
	require.NoError(t, err)
	return &harness{t: t, server: newTestServer(t, h)}
}

func (h *harness) createWidget(t *testing.T, body string) string {
	t.Helper()
	resp := h.do(http.MethodPost, "/Widgets", body, nil)
	require.Equal(t, http.StatusCreated, resp.status, "%s", resp.body)
	res, err := scim.DecodeResource(resp.body, ptr(widgetType()))
	require.NoError(t, err)
	id, _ := res.Get("id")
	return id.(string)
}

func TestPutImmutableSemantics(t *testing.T) {
	h := newWidgetHarness(t, patch.Default)
	id := h.createWidget(t, `{"displayName":"W1","region":"us"}`)

	t.Run("matching immutable value succeeds", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/Widgets/"+id, `{"displayName":"W1b","region":"us"}`, nil)
		require.Equal(t, http.StatusOK, resp.status, "%s", resp.body)
	})

	t.Run("changed immutable value is 400 mutability", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/Widgets/"+id, `{"displayName":"W1c","region":"eu"}`, nil)
		require.Equal(t, http.StatusBadRequest, resp.status, "%s", resp.body)
		assert.Equal(t, scim.ScimTypeMutability, h.decodeError(resp.body).ScimType)
	})

	t.Run("omitted immutable value is retained", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/Widgets/"+id, `{"displayName":"W1d"}`, nil)
		require.Equal(t, http.StatusOK, resp.status, "%s", resp.body)
		res, err := scim.DecodeResource(resp.body, ptr(widgetType()))
		require.NoError(t, err)
		region, ok := res.Get("region")
		assert.True(t, ok, "immutable region retained")
		assert.Equal(t, "us", region)
	})
}

func TestCanonicalValueValidation(t *testing.T) {
	tests := []struct {
		name       string
		profile    patch.Profile
		body       string
		wantStatus int
	}{
		{
			name: "strict rejects non-canonical", profile: patch.Strict,
			body: `{"displayName":"W","tier":"platinum"}`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "strict accepts canonical", profile: patch.Strict,
			body: `{"displayName":"W","tier":"gold"}`, wantStatus: http.StatusCreated,
		},
		{
			name: "strict accepts canonical case-insensitively", profile: patch.Strict,
			body: `{"displayName":"W","tier":"GOLD"}`, wantStatus: http.StatusCreated,
		},
		{
			name: "tolerant profile accepts non-canonical", profile: patch.Default,
			body: `{"displayName":"W","tier":"platinum"}`, wantStatus: http.StatusCreated,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newWidgetHarness(t, tt.profile)
			resp := h.do(http.MethodPost, "/Widgets", tt.body, nil)
			require.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			if tt.wantStatus == http.StatusBadRequest {
				assert.Equal(t, scim.ScimTypeInvalidValue, h.decodeError(resp.body).ScimType)
			}
		})
	}
}

func TestComplexElementValidation(t *testing.T) {
	tests := []struct {
		name       string
		profile    patch.Profile
		body       string
		wantStatus int
		wantScim   string
	}{
		{
			name: "valid element passes strict", profile: patch.Strict,
			body:       `{"displayName":"W","labels":[{"text":"a","kind":"primary"}]}`,
			wantStatus: http.StatusCreated,
		},
		{
			name: "missing required sub-attribute is invalidValue", profile: patch.Default,
			body:       `{"displayName":"W","labels":[{"kind":"primary"}]}`,
			wantStatus: http.StatusBadRequest, wantScim: scim.ScimTypeInvalidValue,
		},
		{
			name: "non-canonical sub-value rejected under strict", profile: patch.Strict,
			body:       `{"displayName":"W","labels":[{"text":"a","kind":"tertiary"}]}`,
			wantStatus: http.StatusBadRequest, wantScim: scim.ScimTypeInvalidValue,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newWidgetHarness(t, tt.profile)
			resp := h.do(http.MethodPost, "/Widgets", tt.body, nil)
			require.Equal(t, tt.wantStatus, resp.status, "%s", resp.body)
			if tt.wantScim != "" {
				assert.Equal(t, tt.wantScim, h.decodeError(resp.body).ScimType)
			}
		})
	}
}

func TestConditionalWrites(t *testing.T) {
	h := newHarness(t, patch.Okta)
	id, created := h.createUser("cond@example.com")
	etag := created.header.Get("ETag")

	t.Run("DELETE with stale If-Match is 412", func(t *testing.T) {
		resp := h.do(http.MethodDelete, "/Users/"+id, "", map[string]string{"If-Match": `W/"stale"`})
		assert.Equal(t, http.StatusPreconditionFailed, resp.status)
	})

	t.Run("PATCH with stale If-Match is 412", func(t *testing.T) {
		body := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],` +
			`"Operations":[{"op":"replace","path":"displayName","value":"x"}]}`
		resp := h.do(http.MethodPatch, "/Users/"+id, body, map[string]string{"If-Match": `W/"stale"`})
		assert.Equal(t, http.StatusPreconditionFailed, resp.status)
	})

	t.Run("DELETE with matching If-Match succeeds", func(t *testing.T) {
		resp := h.do(http.MethodDelete, "/Users/"+id, "", map[string]string{"If-Match": etag})
		assert.Equal(t, http.StatusNoContent, resp.status)
	})
}
