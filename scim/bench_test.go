package scim

import "testing"

func benchUser() User {
	active := true
	return User{
		ID:          "2819c223-7f76-453a-919d-413861904646",
		ExternalID:  "701984",
		UserName:    "bjensen@example.com",
		Name:        &Name{GivenName: "Barbara", FamilyName: "Jensen", Formatted: "Ms. Barbara J Jensen, III"},
		DisplayName: "Babs Jensen",
		Active:      &active,
		Emails: []MultiValue{
			{Value: "bjensen@example.com", Type: "work", Primary: true},
			{Value: "babs@jensen.org", Type: "home"},
		},
		Enterprise: &EnterpriseUser{EmployeeNumber: "701984", Department: "Tour Operations"},
	}
}

// BenchmarkResourceGet measures a dotted sub-attribute read, the hot path
// of filter evaluation and PATCH targeting.
func BenchmarkResourceGet(b *testing.B) {
	u := benchUser()
	r, err := u.Resource()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := r.Get("name.givenName"); !ok {
			b.Fatal("name.givenName unassigned")
		}
	}
}

// BenchmarkUserResource measures typed-view to Resource conversion.
func BenchmarkUserResource(b *testing.B) {
	u := benchUser()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := u.Resource(); err != nil {
			b.Fatal(err)
		}
	}
}
