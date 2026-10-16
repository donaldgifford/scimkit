package scim_test

import (
	"fmt"

	"github.com/donaldgifford/scimkit/scim"
)

// Decoding a client-supplied User body: keys match case-insensitively,
// the enterprise extension lands under its URN container, and readOnly
// attributes (like a client-chosen id) are silently dropped.
func ExampleDecodeResource() {
	body := []byte(`{
		"schemas": ["urn:ietf:params:scim:schemas:core:2.0:User"],
		"id": "client-chosen-id",
		"USERNAME": "bjensen@example.com",
		"name": {"givenName": "Barbara", "familyName": "Jensen"},
		"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User": {
			"employeeNumber": "701984"
		}
	}`)

	rt := scim.UserResourceType()
	r, err := scim.DecodeResource(body, &rt, scim.DropReadOnly())
	if err != nil {
		fmt.Println("decode:", err)
		return
	}

	userName, _ := r.Get("userName")
	given, _ := r.Get("name.givenName")
	employee, _ := r.Get("urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber")
	_, idAssigned := r.Get("id")
	fmt.Println("userName:", userName)
	fmt.Println("name.givenName:", given)
	fmt.Println("employeeNumber:", employee)
	fmt.Println("client-supplied id kept:", idAssigned)
	// Output:
	// userName: bjensen@example.com
	// name.givenName: Barbara
	// employeeNumber: 701984
	// client-supplied id kept: false
}

// Building a resource programmatically: Set validates against the schema,
// unassignment removes values, and the schemas attribute tracks extension
// data automatically.
func ExampleResource_Set() {
	rt := scim.UserResourceType()
	r := scim.NewResource(&rt)

	_ = r.Set("userName", "bjensen@example.com")
	_ = r.Set("urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:department", "Tour Operations")
	fmt.Println(r.Schemas())

	_ = r.Unassign("urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:department")
	fmt.Println(r.Schemas())

	err := r.Set("userName", 42)
	fmt.Println(err)
	// Output:
	// [urn:ietf:params:scim:schemas:core:2.0:User urn:ietf:params:scim:schemas:extension:enterprise:2.0:User]
	// [urn:ietf:params:scim:schemas:core:2.0:User]
	// scim: status 400 invalidValue: path "userName": expected string value, got int
}

// Serving a response: projection is a serialization stage, so the resource
// keeps every attribute while the encoder applies attributes/
// excludedAttributes and the schema's returned rules (password is
// returned=never and can never appear).
func ExampleEncodeResource() {
	user := &scim.User{
		UserName:    "bjensen@example.com",
		DisplayName: "Babs Jensen",
		Password:    "t1meMa$heen",
	}
	r, err := user.Resource()
	if err != nil {
		fmt.Println("convert:", err)
		return
	}

	wire, err := scim.EncodeResource(r, scim.Projection{
		Attributes: []string{"userName"},
	})
	if err != nil {
		fmt.Println("encode:", err)
		return
	}
	fmt.Println(string(wire))
	// Output:
	// {"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"bjensen@example.com"}
}

// The typed views convert both ways; unassigned attributes stay unassigned
// through the round trip.
func ExampleUserFromResource() {
	body := []byte(`{
		"schemas": ["urn:ietf:params:scim:schemas:core:2.0:User"],
		"id": "2819c223",
		"userName": "bjensen@example.com",
		"active": false
	}`)

	rt := scim.UserResourceType()
	r, err := scim.DecodeResource(body, &rt)
	if err != nil {
		fmt.Println("decode:", err)
		return
	}
	u, err := scim.UserFromResource(r)
	if err != nil {
		fmt.Println("convert:", err)
		return
	}

	fmt.Println("id:", u.ID)
	fmt.Println("userName:", u.UserName)
	fmt.Println("active assigned:", u.Active != nil, "value:", *u.Active)
	fmt.Println("name assigned:", u.Name != nil)
	// Output:
	// id: 2819c223
	// userName: bjensen@example.com
	// active assigned: true value: false
	// name assigned: false
}
