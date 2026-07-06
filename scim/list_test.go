package scim

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListResponseMarshal pins the wire invariants: capitalized Resources
// key, integer pagination fields, Resources always present.
func TestListResponseMarshal(t *testing.T) {
	tests := []struct {
		name string
		in   ListResponse
		want string
	}{
		{
			name: "paged",
			in: ListResponse{
				TotalResults: 100,
				StartIndex:   1,
				ItemsPerPage: 2,
				Resources:    []json.RawMessage{[]byte(`{"id":"a"}`), []byte(`{"id":"b"}`)},
			},
			want: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],` +
				`"totalResults":100,"startIndex":1,"itemsPerPage":2,` +
				`"Resources":[{"id":"a"},{"id":"b"}]}`,
		},
		{
			name: "empty result keeps Resources as an empty array",
			in:   ListResponse{TotalResults: 0},
			want: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],` +
				`"totalResults":0,"Resources":[]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestListResponseUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    ListResponse
		wantErr bool
	}{
		{
			name: "spec casing",
			body: `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],
				"totalResults":2,"startIndex":1,"itemsPerPage":2,
				"Resources":[{"id":"a"},{"id":"b"}]}`,
			want: ListResponse{
				TotalResults: 2,
				StartIndex:   1,
				ItemsPerPage: 2,
				Resources:    []json.RawMessage{[]byte(`{"id":"a"}`), []byte(`{"id":"b"}`)},
			},
		},
		{
			name: "lax lowercase resources key",
			body: `{"totalresults":1,"resources":[{"id":"a"}]}`,
			want: ListResponse{TotalResults: 1, Resources: []json.RawMessage{[]byte(`{"id":"a"}`)}},
		},
		{
			name: "string-wrapped totals tolerated",
			body: `{"totalResults":"5"}`,
			want: ListResponse{TotalResults: 5},
		},
		{name: "not an object", body: `[]`, wantErr: true},
		{name: "non-numeric total", body: `{"totalResults":true}`, wantErr: true},
		{name: "fractional total", body: `{"totalResults":1.5}`, wantErr: true},
		{name: "resources not an array", body: `{"Resources":{"id":"a"}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ListResponse
			err := json.Unmarshal([]byte(tt.body), &got)
			if tt.wantErr {
				var scimErr *Error
				require.ErrorAs(t, err, &scimErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListResponseRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   ListResponse
	}{
		{
			name: "paged",
			in: ListResponse{
				TotalResults: 7,
				StartIndex:   3,
				ItemsPerPage: 1,
				Resources:    []json.RawMessage{[]byte(`{"id":"a"}`)},
			},
		},
		{name: "empty", in: ListResponse{Resources: []json.RawMessage{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.in)
			require.NoError(t, err)
			var back ListResponse
			require.NoError(t, json.Unmarshal(data, &back))
			assert.Equal(t, tt.in, back)
		})
	}
}
