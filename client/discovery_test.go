package client_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/client"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// deviceType is a custom resource type only reachable through discovery.
func deviceType() scim.ResourceType {
	return scim.ResourceType{
		Name:     "Device",
		Endpoint: "/Devices",
		Schema: scim.Schema{
			ID:   "urn:example:Device",
			Name: "Device",
			Attributes: []scim.Attribute{
				{Name: "serial", Type: scim.TypeString, Required: true, Uniqueness: scim.UniquenessServer},
				{Name: "label", Type: scim.TypeString},
			},
		}.WithDefaults(),
	}
}

func newDiscoveryHarness(t *testing.T) (*client.Client, *atomic.Int32) {
	t.Helper()
	rt := deviceType()
	handler, err := server.New(server.Config{
		BaseURL:          "https://example.com/scim/v2",
		ResourceTypes:    []server.Registration{{Type: rt, Store: memstore.New(rt)}},
		Profile:          patch.Default,
		MaxFilterResults: 100,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(t, err)

	var count atomic.Int32
	counted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ResourceTypes/Device" || r.URL.Path == "/Schemas/urn:example:Device" {
			count.Add(1)
		}
		handler.ServeHTTP(w, r)
	})
	ts := httptest.NewServer(counted)
	t.Cleanup(ts.Close)
	c, err := client.New(ts.URL, client.WithHTTPClient(ts.Client()))
	require.NoError(t, err)
	return c, &count
}

func TestClientCustomTypeViaDiscovery(t *testing.T) {
	c, discoveryHits := newDiscoveryHarness(t)
	ctx := context.Background()
	devices := c.Resources("Device")

	rt := deviceType()
	dev := scim.NewResource(&rt)
	require.NoError(t, dev.Set("serial", "SN-123"))
	require.NoError(t, dev.Set("label", "Laptop"))

	created, err := devices.Create(ctx, dev)
	require.NoError(t, err, "custom type resolved from /ResourceTypes + /Schemas")
	id, _ := created.Get("id")
	require.NotEmpty(t, id)

	got, err := devices.Get(ctx, id.(string))
	require.NoError(t, err)
	serial, _ := got.Get("serial")
	assert.Equal(t, "SN-123", serial)

	hitsAfterTwoCalls := discoveryHits.Load()
	// Reuse the type again — discovery must be cached on the Client.
	_, err = c.Resources("Device").Get(ctx, id.(string))
	require.NoError(t, err)
	assert.Equal(t, hitsAfterTwoCalls, discoveryHits.Load(), "resource type is cached, not refetched")
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{name: "absolute url", baseURL: "https://example.com/scim/v2"},
		{name: "trailing slash trimmed", baseURL: "https://example.com/scim/v2/"},
		{name: "relative url rejected", baseURL: "/scim/v2", wantErr: true},
		{name: "missing scheme rejected", baseURL: "example.com/scim", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.New(tt.baseURL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
