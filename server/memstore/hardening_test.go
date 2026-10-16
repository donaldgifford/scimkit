package memstore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

func listNames(t *testing.T, s *Store, q server.Query) []string {
	t.Helper()
	page, err := s.List(context.Background(), q)
	require.NoError(t, err)
	names := make([]string, 0, len(page.Resources))
	for _, res := range page.Resources {
		v, _ := res.Get("userName")
		names = append(names, v.(string))
	}
	return names
}

func TestListInsertionOrder(t *testing.T) {
	type step struct {
		create string // userName to create, or ""
		remove string // userName to delete, or ""
	}
	tests := []struct {
		name  string
		steps []step
		q     server.Query
		want  []string
	}{
		{
			name:  "plain creates list in insertion order",
			steps: []step{{create: "c@x"}, {create: "a@x"}, {create: "b@x"}},
			q:     server.Query{StartIndex: 1, Count: 10},
			want:  []string{"c@x", "a@x", "b@x"},
		},
		{
			name: "interleaved deletes keep the survivors' order",
			steps: []step{
				{create: "one@x"},
				{create: "two@x"},
				{create: "three@x"},
				{remove: "two@x"},
				{create: "four@x"},
				{remove: "one@x"},
				{create: "five@x"},
			},
			q:    server.Query{StartIndex: 1, Count: 10},
			want: []string{"three@x", "four@x", "five@x"},
		},
		{
			name: "re-creating a deleted name goes to the end",
			steps: []step{
				{create: "a@x"}, {create: "b@x"}, {remove: "a@x"}, {create: "a@x"},
			},
			q:    server.Query{StartIndex: 1, Count: 10},
			want: []string{"b@x", "a@x"},
		},
		{
			name: "paging walks the same order",
			steps: []step{
				{create: "p1@x"}, {create: "p2@x"}, {create: "p3@x"}, {create: "p4@x"}, {remove: "p2@x"},
			},
			q:    server.Query{StartIndex: 2, Count: 2},
			want: []string{"p3@x", "p4@x"},
		},
		{
			name: "non-indexed filter scan preserves insertion order",
			steps: []step{
				{create: "zed@x"}, {create: "amy@x"}, {create: "bob@x"},
			},
			q:    server.Query{Filter: mustFilter(t, `userName co "@x"`), StartIndex: 1, Count: 10},
			want: []string{"zed@x", "amy@x", "bob@x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(scim.UserResourceType())
			ids := map[string]string{}
			for _, st := range tt.steps {
				if st.create != "" {
					created, err := s.Create(context.Background(), newUser(t, &s.rt, st.create))
					require.NoError(t, err)
					id, _ := created.Get("id")
					ids[st.create] = id.(string)
				}
				if st.remove != "" {
					require.NoError(t, s.Delete(context.Background(), ids[st.remove]))
				}
			}
			first := listNames(t, s, tt.q)
			assert.Equal(t, tt.want, first)
			for range 5 {
				assert.Equal(t, first, listNames(t, s, tt.q), "order is stable across calls")
			}
		})
	}
}

func TestListIndexHitsInInsertionOrder(t *testing.T) {
	s := New(scim.UserResourceType(), WithIndex("title"))
	for _, name := range []string{"d@x", "c@x", "b@x", "a@x"} {
		res := newUser(t, &s.rt, name)
		require.NoError(t, res.Set("title", "Engineer"))
		_, err := s.Create(context.Background(), res)
		require.NoError(t, err)
	}
	q := server.Query{Filter: mustFilter(t, `title eq "engineer"`), StartIndex: 1, Count: 10}
	first := listNames(t, s, q)
	assert.Equal(t, []string{"d@x", "c@x", "b@x", "a@x"}, first, "index bucket is replayed in insertion order")
	for range 5 {
		assert.Equal(t, first, listNames(t, s, q))
	}
}

func TestReplaceWeakVersionCompare(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		wantStatus int // 0 means success
	}{
		{name: "exact weak form matches", token: `W/"1"`},
		{name: "strong-quoted form matches", token: `"1"`},
		{name: "bare opaque form matches", token: "1"},
		{name: "surrounding whitespace is ignored", token: ` W/"1" `},
		{name: "empty token skips the check", token: ""},
		{name: "different version fails", token: `W/"2"`, wantStatus: 412},
		{name: "different bare version fails", token: "2", wantStatus: 412},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ids := seedUsers(t, "v@example.com")
			next := newUser(t, &s.rt, "v2@example.com")
			next.SetMeta(scim.Meta{Version: tt.token})
			replaced, err := s.Replace(context.Background(), ids[0], next)
			if tt.wantStatus != 0 {
				var scimErr *scim.Error
				require.ErrorAs(t, err, &scimErr)
				assert.Equal(t, tt.wantStatus, scimErr.Status)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, `W/"2"`, replaced.Meta().Version, "store re-stamps its own form after the swap")
		})
	}
}

func TestOpaqueVersion(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: `W/"7"`, want: "7"},
		{in: `"7"`, want: "7"},
		{in: "7", want: "7"},
		{in: "  W/\"7\"\t", want: "7"},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, opaqueVersion(tt.in))
		})
	}
}
