package scimtest_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/client"
	"github.com/donaldgifford/scimkit/scimtest"
)

// deviceDoc builds a Device document; the custom type reuses the User
// schema, so each seeded document needs a distinct userName.
func deviceDoc(name string) string {
	return `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"` + name + `"}`
}

func TestSeedCustomResourceTypes(t *testing.T) {
	device := customType("Device", "/Devices")
	d1, d2, u1 := deviceDoc("device-1"), deviceDoc("device-2"), deviceDoc("user-1")
	tests := []struct {
		name        string
		opts        []scimtest.Option
		wantDevices int
		wantUsers   int
		wantErr     string
	}{
		{
			name: "resources key in a seed document",
			opts: []scimtest.Option{
				scimtest.WithResourceType(device),
				scimtest.WithSeedJSON([]byte(`{"users":[` + u1 + `],"resources":{"device":[` + d1 + `,` + d2 + `]}}`)),
			},
			wantDevices: 2,
			wantUsers:   1,
		},
		{
			name: "WithSeedResources option accumulates with WithSeed",
			opts: []scimtest.Option{
				scimtest.WithResourceType(device),
				scimtest.WithSeedResources("Device", json.RawMessage(d1)),
				scimtest.WithSeed(scimtest.Seed{Resources: map[string][]json.RawMessage{"Device": {json.RawMessage(d2)}}}),
			},
			wantDevices: 2,
		},
		{
			name:    "seeding an unregistered type fails construction",
			opts:    []scimtest.Option{scimtest.WithSeedResources("Device", json.RawMessage(d1))},
			wantErr: "no such resource type registered",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr != "" {
				_, err := scimtest.NewMock(tt.opts...)
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			srv := scimtest.NewServer(t, tt.opts...)
			assert.Equal(t, 0, srv.Recorder().Count(), "seed traffic is not recorded")
			ctx := context.Background()
			assert.Equal(t, tt.wantDevices, count(t, srv.Client().Resources("Device").List(ctx, client.Query{})))
			assert.Equal(t, tt.wantUsers, count(t, srv.Client().Users().List(ctx, client.Query{})))
		})
	}
}
