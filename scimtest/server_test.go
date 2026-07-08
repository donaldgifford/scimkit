package scimtest_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/scimtest"
	"github.com/donaldgifford/scimkit/server"
)

// newUser builds a minimal valid User resource for the mock.
func newUser(t *testing.T, userName string) *scim.Resource {
	t.Helper()
	rt := scim.UserResourceType()
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("userName", userName))
	return res
}

func TestNewServerLifecycle(t *testing.T) {
	srv := scimtest.NewServer(t)
	ctx := context.Background()
	users := srv.Client().Users()

	created, err := users.Create(ctx, newUser(t, "bjensen@example.com"))
	require.NoError(t, err)
	id, ok := created.Get("id")
	require.True(t, ok)

	got, err := users.Get(ctx, id.(string))
	require.NoError(t, err)
	gotName, _ := got.Get("userName")
	assert.Equal(t, "bjensen@example.com", gotName)

	require.NoError(t, users.Delete(ctx, id.(string)))

	// The recorder captured every attempt: create, get, delete.
	methods := make([]string, 0, srv.Recorder().Count())
	for _, r := range srv.Recorder().Requests() {
		methods = append(methods, r.Method)
	}
	assert.Equal(t, []string{http.MethodPost, http.MethodGet, http.MethodDelete}, methods)
}

func TestNewServerStoreAccessor(t *testing.T) {
	tests := []struct {
		name    string
		lookup  string
		wantNil bool
	}{
		{name: "user by declared name", lookup: "User", wantNil: false},
		{name: "group case-insensitive", lookup: "group", wantNil: false},
		{name: "unregistered type", lookup: "Device", wantNil: true},
	}
	srv := scimtest.NewServer(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := srv.Store(tt.lookup)
			if tt.wantNil {
				assert.Nil(t, st)
				return
			}
			require.NotNil(t, st)
		})
	}
}

func TestNewServerStoreSeedThenRead(t *testing.T) {
	srv := scimtest.NewServer(t)
	ctx := context.Background()

	// A resource created straight through the store is visible over HTTP.
	stored, err := srv.Store("User").Create(ctx, newUser(t, "direct@example.com"))
	require.NoError(t, err)
	id, _ := stored.Get("id")

	got, err := srv.Client().Users().Get(ctx, id.(string))
	require.NoError(t, err)
	name, _ := got.Get("userName")
	assert.Equal(t, "direct@example.com", name)
}

func TestNewServerStoreImplementsPatcher(t *testing.T) {
	srv := scimtest.NewServer(t)
	_, ok := srv.Store("Group").(server.Patcher)
	assert.True(t, ok, "memstore-backed store exposes native PATCH")
}

func TestNewServerBearerToken(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{name: "correct token", authHeader: "Bearer s3cret", wantStatus: http.StatusOK},
		{name: "missing token", authHeader: "", wantStatus: http.StatusUnauthorized},
		{name: "wrong token", authHeader: "Bearer nope", wantStatus: http.StatusUnauthorized},
	}
	srv := scimtest.NewServer(t, scimtest.WithBearerToken("s3cret"))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL()+"/Users", http.NoBody)
			require.NoError(t, err)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

func TestNewServerTokenClientSucceeds(t *testing.T) {
	srv := scimtest.NewServer(t, scimtest.WithBearerToken("s3cret"))
	// The cached client carries the token, so authenticated calls work.
	_, err := srv.Client().Users().Create(context.Background(), newUser(t, "authed@example.com"))
	require.NoError(t, err)
}

func TestNewMockDefaultTypes(t *testing.T) {
	m, err := scimtest.NewMock()
	require.NoError(t, err)
	assert.NotNil(t, m.Store("User"))
	assert.NotNil(t, m.Store("Group"))
	assert.NotNil(t, m.Handler())
	assert.NotNil(t, m.Recorder())
}

func TestNewMockCustomResourceType(t *testing.T) {
	// Registering a type replaces the default pair.
	deviceRT := scim.UserResourceType()
	deviceRT.Name = "Device"
	deviceRT.Endpoint = "/Devices"

	m, err := scimtest.NewMock(scimtest.WithResourceType(deviceRT))
	require.NoError(t, err)

	assert.NotNil(t, m.Store("Device"))
	assert.Nil(t, m.Store("User"), "registering a custom type replaces the built-in User+Group")
}

func TestRecorderResetAndBody(t *testing.T) {
	srv := scimtest.NewServer(t)
	_, err := srv.Client().Users().Create(context.Background(), newUser(t, "body@example.com"))
	require.NoError(t, err)

	last, ok := srv.Recorder().Last()
	require.True(t, ok)
	assert.Equal(t, http.MethodPost, last.Method)
	assert.Contains(t, string(last.Body), "body@example.com", "recorder buffered the request body")

	srv.Recorder().Reset()
	assert.Equal(t, 0, srv.Recorder().Count())
	_, ok = srv.Recorder().Last()
	assert.False(t, ok)
}
