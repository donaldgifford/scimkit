package filter

import (
	"testing"

	"github.com/donaldgifford/scimkit/scim"
)

func benchResource(b *testing.B) *scim.Resource {
	b.Helper()
	u := scim.User{
		ID:       "2819c223-7f76-453a-919d-413861904646",
		UserName: "bjensen@example.com",
		Name:     &scim.Name{GivenName: "Barbara", FamilyName: "Jensen"},
		Emails: []scim.MultiValue{
			{Value: "bjensen@example.com", Type: "work", Primary: true},
			{Value: "babs@jensen.org", Type: "home"},
		},
	}
	r, err := u.Resource()
	if err != nil {
		b.Fatal(err)
	}
	return r
}

// BenchmarkMatchSimple measures one equality clause against a User.
func BenchmarkMatchSimple(b *testing.B) {
	r := benchResource(b)
	e, err := Parse(`userName eq "bjensen@example.com"`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		ok, err := Match(r, e)
		if err != nil || !ok {
			b.Fatalf("Match = %v, %v", ok, err)
		}
	}
}

// BenchmarkMatchValuePath measures per-element valuePath correlation.
func BenchmarkMatchValuePath(b *testing.B) {
	r := benchResource(b)
	e, err := Parse(`emails[type eq "home" and value co "jensen.org"]`)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		ok, err := Match(r, e)
		if err != nil || !ok {
			b.Fatalf("Match = %v, %v", ok, err)
		}
	}
}
