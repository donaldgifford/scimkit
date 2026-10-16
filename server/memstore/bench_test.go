package memstore

import (
	"context"
	"fmt"
	"testing"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
)

// BenchmarkListUnfiltered measures one page of an unfiltered, unsorted
// list over a populated store — the Okta "walk every user" sweep.
func BenchmarkListUnfiltered(b *testing.B) {
	const users = 10_000
	s := New(scim.UserResourceType())
	for i := range users {
		res := scim.NewResource(&s.rt)
		if err := res.Set("userName", fmt.Sprintf("user-%05d@example.com", i)); err != nil {
			b.Fatal(err)
		}
		if _, err := s.Create(context.Background(), res); err != nil {
			b.Fatal(err)
		}
	}
	q := server.Query{StartIndex: 1, Count: 100}
	b.ReportAllocs()
	for b.Loop() {
		page, err := s.List(context.Background(), q)
		if err != nil {
			b.Fatal(err)
		}
		if page.TotalResults != users {
			b.Fatalf("total %d, want %d", page.TotalResults, users)
		}
	}
}

// BenchmarkPatchMemberRemove measures removing one member from a
// 5 000-member group through the native Patcher. Each iteration removes
// a different member; once the cycle has drained the group it is reset
// outside the timer so every measured op works against the full set.
func BenchmarkPatchMemberRemove(b *testing.B) {
	const members = 5_000
	rt := scim.GroupResourceType()
	s := New(rt, WithProfile(patch.Default))
	full := make([]any, members)
	for i := range members {
		full[i] = map[string]any{"value": fmt.Sprintf("u-%d", i)}
	}
	seed := scim.NewResource(&s.rt)
	if err := seed.Set("displayName", "Everyone"); err != nil {
		b.Fatal(err)
	}
	if err := seed.Set("members", full); err != nil {
		b.Fatal(err)
	}
	created, err := s.Create(context.Background(), seed)
	if err != nil {
		b.Fatal(err)
	}
	idv, _ := created.Get("id")
	id := idv.(string)

	ops := make([][]patch.Op, members)
	for i := range members {
		body := fmt.Sprintf(`{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],`+
			`"Operations":[{"op":"remove","path":"members[value eq \"u-%d\"]"}]}`, i)
		decoded, err := patch.Decode([]byte(body), &rt, patch.Default)
		if err != nil {
			b.Fatal(err)
		}
		ops[i] = decoded
	}
	reset := func() {
		b.StopTimer()
		if _, err := s.Replace(context.Background(), id, seed); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}

	b.ReportAllocs()
	i := 0
	for b.Loop() {
		if i == members {
			reset()
			i = 0
		}
		if _, err := s.Patch(context.Background(), id, ops[i]); err != nil {
			b.Fatal(err)
		}
		i++
	}
}
