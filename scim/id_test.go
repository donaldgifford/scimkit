package scim

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var uuidV4Pattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func TestNewID(t *testing.T) {
	tests := []struct {
		name  string
		check func(t *testing.T, ids []string)
	}{
		{
			name: "well-formed version 4 UUIDs",
			check: func(t *testing.T, ids []string) {
				t.Helper()
				for _, id := range ids {
					assert.Regexp(t, uuidV4Pattern, id)
				}
			},
		},
		{
			name: "no duplicates",
			check: func(t *testing.T, ids []string) {
				t.Helper()
				seen := make(map[string]bool, len(ids))
				for _, id := range ids {
					assert.False(t, seen[id], "duplicate id %s", id)
					seen[id] = true
				}
			},
		},
		{
			name: "valid per ValidateID",
			check: func(t *testing.T, ids []string) {
				t.Helper()
				for _, id := range ids {
					assert.NoError(t, ValidateID(id))
				}
			},
		},
	}
	ids := make([]string, 1000)
	for i := range ids {
		id, err := NewID()
		require.NoError(t, err)
		ids[i] = id
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, ids)
		})
	}
}

func TestValidateID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "uuid", id: "2819c223-7f76-453a-919d-413861904646"},
		{name: "opaque string", id: "user-42"},
		{name: "empty", id: "", wantErr: true},
		{name: "reserved bulkId", id: "bulkId", wantErr: true},
		{name: "reserved bulkId folded", id: "BULKID", wantErr: true},
		{name: "bulkId as substring is fine", id: "bulkId-2", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateID(tt.id)
			if tt.wantErr {
				var scimErr *Error
				require.ErrorAs(t, err, &scimErr)
				assert.Equal(t, ScimTypeInvalidValue, scimErr.ScimType)
				return
			}
			require.NoError(t, err)
		})
	}
}
