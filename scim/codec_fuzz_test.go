package scim

import (
	"bytes"
	"testing"
)

// FuzzDecodeResource drives hostile input through the decoder and pins the
// canonical-form property: anything that decodes must encode, re-decode,
// and re-encode to byte-identical output.
func FuzzDecodeResource(f *testing.F) {
	seeds := []string{
		testUserBody,
		`{}`,
		`{"userName": "b"}`,
		`{"userName": null}`,
		`{"emails": []}`,
		`{"emails": [{"value": "a@b.c", "primary": true}]}`,
		`{"name": {"givenName": "Barbara"}}`,
		`{"USERNAME": "folded", "NAME": {"GIVENNAME": "B"}}`,
		`{"active": true, "id": "x", "externalId": "y"}`,
		`{"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User": {"employeeNumber": "7"}}`,
		`{"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User": {"manager": {"displayName": "M"}}}`,
		`{"urn:unknown:extension": {"a": 1}}`,
		`{"meta": {"created": "2010-01-23T04:56:22Z", "version": "W/\"1\""}}`,
		`{"schemas": ["urn:bogus"], "favoriteColor": "teal"}`,
		`{"x509Certificates": [{"value": "MIID"}]}`,
		`{"photos": [{"value": "https://example.com/p.png"}]}`,
		`{"userName": 42}`,
		`{"emails": [{"primary": "yes"}]}`,
		`{"userName": "\u0000\uffff"}`,
		`[1,2,3]`,
		`{`,
		``,
		`{"emails": [{"value": "a", "primary": true}, {"value": "b", "primary": true}]}`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	rt := UserResourceType()
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := DecodeResource(data, &rt)
		if err != nil {
			return // rejecting hostile input is fine; panicking is not
		}
		wire, err := EncodeResource(r, Projection{})
		if err != nil {
			t.Fatalf("EncodeResource(DecodeResource(%q)) = %v, want nil error", data, err)
		}
		second, err := DecodeResource(wire, &rt)
		if err != nil {
			t.Fatalf("DecodeResource(canonical %q) = %v, want nil error", wire, err)
		}
		wire2, err := EncodeResource(second, Projection{})
		if err != nil {
			t.Fatalf("EncodeResource(second decode of %q) = %v, want nil error", wire, err)
		}
		if !bytes.Equal(wire, wire2) {
			t.Fatalf("canonical form not idempotent:\nfirst  = %s\nsecond = %s", wire, wire2)
		}
	})
}

// FuzzListResponseUnmarshal pins the ListResponse parser against hostile
// bodies: parse errors are fine, panics are not, and successful parses must
// round-trip through marshal.
func FuzzListResponseUnmarshal(f *testing.F) {
	seeds := []string{
		`{"totalResults":2,"startIndex":1,"itemsPerPage":2,"Resources":[{"id":"a"}]}`,
		`{"totalresults":1,"resources":[{}]}`,
		`{"totalResults":"5"}`,
		`{"totalResults":1.5}`,
		`{"Resources":{}}`,
		`{}`,
		`[]`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var lr ListResponse
		if err := lr.UnmarshalJSON(data); err != nil {
			return
		}
		if _, err := lr.MarshalJSON(); err != nil {
			t.Fatalf("MarshalJSON after successful unmarshal of %q = %v, want nil error", data, err)
		}
	})
}
