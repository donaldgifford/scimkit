package scimtest_test

import (
	"context"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/client"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/scimtest"
)

const seedDoc = `{
  "users": [
    {"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"seed1@example.com"},
    {"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"seed2@example.com"}
  ],
  "groups": [
    {"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"displayName":"Seeded"}
  ]
}`

func count(t *testing.T, it iter.Seq2[*scim.Resource, error]) int {
	t.Helper()
	n := 0
	for _, err := range it {
		require.NoError(t, err)
		n++
	}
	return n
}

func TestWithSeedJSON(t *testing.T) {
	srv := scimtest.NewServer(t, scimtest.WithSeedJSON([]byte(seedDoc)))
	ctx := context.Background()

	// Seeding runs before the recorder is installed, so it never counts as
	// provisioner traffic — checked before any client call adds to the tally.
	assert.Equal(t, 0, srv.Recorder().Count(), "seed traffic is not recorded")

	assert.Equal(t, 2, count(t, srv.Client().Users().List(ctx, client.Query{})), "two users seeded")
	assert.Equal(t, 1, count(t, srv.Client().Groups().List(ctx, client.Query{})), "one group seeded")
}

func TestWithSeedStruct(t *testing.T) {
	seed, err := scimtest.DecodeSeed(strings.NewReader(seedDoc))
	require.NoError(t, err)

	srv := scimtest.NewServer(t, scimtest.WithSeed(seed))
	ctx := context.Background()
	assert.Equal(t, 2, count(t, srv.Client().Users().List(ctx, client.Query{})))
	assert.Equal(t, 1, count(t, srv.Client().Groups().List(ctx, client.Query{})))
}

func TestLoadSeedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seed.json")
	require.NoError(t, os.WriteFile(path, []byte(seedDoc), 0o600))

	seed, err := scimtest.LoadSeedFile(path)
	require.NoError(t, err)
	assert.Len(t, seed.Users, 2)
	assert.Len(t, seed.Groups, 1)

	srv := scimtest.NewServer(t, scimtest.WithSeed(seed))
	assert.Equal(t, 2, count(t, srv.Client().Users().List(context.Background(), client.Query{})))
}

func TestSeedErrors(t *testing.T) {
	tests := []struct {
		name    string
		opt     scimtest.Option
		wantErr string
	}{
		{
			name:    "malformed json",
			opt:     scimtest.WithSeedJSON([]byte(`{"users": [`)),
			wantErr: "decode seed",
		},
		{
			name:    "invalid resource rejected by pipeline",
			opt:     scimtest.WithSeedJSON([]byte(`{"users":[{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"]}]}`)),
			wantErr: "seed User",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scimtest.NewMock(tt.opt)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestLoadSeedFileMissing(t *testing.T) {
	_, err := scimtest.LoadSeedFile(filepath.Join(t.TempDir(), "nope.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open seed file")
}
