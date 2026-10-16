package patch

import (
	"strconv"
	"testing"

	"github.com/donaldgifford/scimkit/scim"
)

// BenchmarkApplyRemoveMember5000 measures removing one member by
// valuePath from a 5 000-member group — the shape of a large IdP group
// sync.
func BenchmarkApplyRemoveMember5000(b *testing.B) {
	const n = 5000
	g := scim.Group{ID: "e9e30dba", DisplayName: "Tour Guides", Members: make([]scim.Member, n)}
	for i := range g.Members {
		g.Members[i] = scim.Member{Value: "m-" + strconv.Itoa(i), Type: "User"}
	}
	res, err := g.Resource()
	if err != nil {
		b.Fatal(err)
	}
	body := patchBody(`[{"op": "remove", "path": "members[value eq \"m-2500\"]"}]`)
	ops, err := Decode([]byte(body), res.ResourceType(), Strict)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		result, err := Apply(res, ops, Strict)
		if err != nil || !result.Changed {
			b.Fatalf("Apply = %+v, %v", result, err)
		}
	}
}
