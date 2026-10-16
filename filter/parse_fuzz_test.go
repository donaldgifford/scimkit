package filter

import (
	"reflect"
	"testing"
)

// FuzzParse drives hostile input through the filter parser. Rejecting
// input is fine; panicking is not, and anything that parses must satisfy
// the round-trip property: String() reparses to an equal AST.
func FuzzParse(f *testing.F) {
	seeds := []string{
		// RFC 7644 §3.4.2.2 Figure 2
		`userName eq "bjensen"`,
		`name.familyName co "O'Malley"`,
		`userName sw "J"`,
		`urn:ietf:params:scim:schemas:core:2.0:User:userName sw "J"`,
		`title pr`,
		`meta.lastModified gt "2011-05-13T04:42:34Z"`,
		`title pr and userType eq "Employee"`,
		`title pr or userType eq "Intern"`,
		`schemas eq "urn:ietf:params:scim:api:messages:2.0:PatchOp"`,
		`userType eq "Employee" and (emails co "example.com" or emails.value co "example.org")`,
		`userType ne "Employee" and not (emails co "example.com" or emails.value co "example.org")`,
		`userType eq "Employee" and emails[type eq "work" and value co "@example.com"]`,
		`emails[type eq "work" and value co "@example.com"] or ims[type eq "xmpp" and value co "@foo.com"]`,
		// IdP shapes
		`externalId eq "00u1abcd"`,
		`urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber eq "701984"`,
		`members[$ref eq "https://example.com/v2/Users/1"]`,
		`not userName eq "x"`,
		// literals and hostile input
		`active eq true`,
		`title eq null`,
		`count gt 3`,
		`ratio le 2.5`,
		`n eq -1.5e3`,
		`a eq "colon:in:string"`,
		`a eq "\u0000\uffff"`,
		`a eq "unterminated`,
		`(((((a pr`,
		`emails[emails[type pr]]`,
		`a eq TRUE`,
		`not not not (a pr)`,
		``,
		`   `,
		`urn:`,
		`:`,
		`a`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		e, err := Parse(input)
		if err != nil {
			return
		}
		rendered := e.String()
		back, err := Parse(rendered)
		if err != nil {
			t.Fatalf("Parse(%q).String() = %q failed to reparse: %v", input, rendered, err)
		}
		if !reflect.DeepEqual(e, back) {
			t.Fatalf("round trip changed AST for %q via %q", input, rendered)
		}
	})
}

// FuzzParsePath drives hostile input through the PATCH path parser with
// the same no-panic and round-trip guarantees.
func FuzzParsePath(f *testing.F) {
	seeds := []string{
		`members`,
		`name.givenName`,
		`emails[type eq "work"]`,
		`emails[type eq "work"].value`,
		`members[value eq "2819c223"]`,
		`addresses[type eq "work" and locality eq "Hollywood"].streetAddress`,
		`urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:manager.displayName`,
		`$ref`,
		`emails[]`,
		`emails[type pr].`,
		`a.b.c`,
		`[type pr]`,
		``,
		`urn:x:`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		p, err := ParsePath(input)
		if err != nil {
			return
		}
		rendered := p.String()
		back, err := ParsePath(rendered)
		if err != nil {
			t.Fatalf("ParsePath(%q).String() = %q failed to reparse: %v", input, rendered, err)
		}
		if !reflect.DeepEqual(p, back) {
			t.Fatalf("round trip changed path for %q via %q", input, rendered)
		}
	})
}
