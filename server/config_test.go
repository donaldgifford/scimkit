package server_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

func TestNewConfigValidation(t *testing.T) {
	userReg := server.Registration{
		Type:  scim.UserResourceType(),
		Store: memstore.New(scim.UserResourceType()),
	}
	tests := []struct {
		name    string
		cfg     server.Config
		wantErr string
	}{
		{
			name: "valid config",
			cfg: server.Config{
				BaseURL:          "https://example.com/scim/v2",
				ResourceTypes:    []server.Registration{userReg},
				MaxFilterResults: 100,
			},
		},
		{
			name: "missing base url",
			cfg: server.Config{
				ResourceTypes:    []server.Registration{userReg},
				MaxFilterResults: 100,
			},
			wantErr: "BaseURL is required",
		},
		{
			name: "no resource types",
			cfg: server.Config{
				BaseURL:          "https://example.com",
				MaxFilterResults: 100,
			},
			wantErr: "at least one resource type",
		},
		{
			name: "non-positive max filter results",
			cfg: server.Config{
				BaseURL:       "https://example.com",
				ResourceTypes: []server.Registration{userReg},
			},
			wantErr: "MaxFilterResults must be positive",
		},
		{
			name: "nil store",
			cfg: server.Config{
				BaseURL:          "https://example.com",
				ResourceTypes:    []server.Registration{{Type: scim.UserResourceType()}},
				MaxFilterResults: 100,
			},
			wantErr: "has no store",
		},
		{
			name: "duplicate endpoint",
			cfg: server.Config{
				BaseURL:          "https://example.com",
				ResourceTypes:    []server.Registration{userReg, userReg},
				MaxFilterResults: 100,
			},
			wantErr: "duplicate endpoint",
		},
		{
			name: "max page size below default page size",
			cfg: server.Config{
				BaseURL:          "https://example.com",
				ResourceTypes:    []server.Registration{userReg},
				MaxFilterResults: 100,
				DefaultPageSize:  50,
				MaxPageSize:      10,
			},
			wantErr: "MaxPageSize (10) must be at least DefaultPageSize (50)",
		},
		{
			name: "max page size equal to default page size is fine",
			cfg: server.Config{
				BaseURL:          "https://example.com",
				ResourceTypes:    []server.Registration{userReg},
				MaxFilterResults: 100,
				DefaultPageSize:  50,
				MaxPageSize:      50,
			},
		},
		{
			name: "large default page size lifts the max page size default",
			cfg: server.Config{
				BaseURL:          "https://example.com",
				ResourceTypes:    []server.Registration{userReg},
				MaxFilterResults: 100,
				DefaultPageSize:  500,
			},
		},
		{
			name: "negative max body bytes",
			cfg: server.Config{
				BaseURL:          "https://example.com",
				ResourceTypes:    []server.Registration{userReg},
				MaxFilterResults: 100,
				MaxBodyBytes:     -1,
			},
			wantErr: "MaxBodyBytes must not be negative",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.New(tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
