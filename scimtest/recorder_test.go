package scimtest_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scimtest"
)

func TestRecorderLimit(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		requests  int
		wantCount int
		wantFirst string // userName in the oldest retained request body
	}{
		{name: "unbounded by default", limit: 0, requests: 5, wantCount: 5, wantFirst: "u0"},
		{name: "limit keeps the most recent", limit: 3, requests: 5, wantCount: 3, wantFirst: "u2"},
		{name: "limit larger than traffic", limit: 10, requests: 2, wantCount: 2, wantFirst: "u0"},
		{name: "negative limit means unbounded", limit: -1, requests: 4, wantCount: 4, wantFirst: "u0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := scimtest.NewServer(t, scimtest.WithRecorderLimit(tt.limit))
			ctx := context.Background()
			for i := range tt.requests {
				_, err := srv.Client().Users().Create(ctx, newUser(t, fmt.Sprintf("u%d@example.com", i)))
				require.NoError(t, err)
			}
			reqs := srv.Recorder().Requests()
			require.Len(t, reqs, tt.wantCount)
			assert.Equal(t, tt.wantCount, srv.Recorder().Count())
			assert.Contains(t, string(reqs[0].Body), tt.wantFirst+"@example.com", "oldest retained request")
			last, ok := srv.Recorder().Last()
			require.True(t, ok)
			assert.Contains(t, string(last.Body), fmt.Sprintf("u%d@example.com", tt.requests-1))
		})
	}
}
