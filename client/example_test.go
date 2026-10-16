package client_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/donaldgifford/scimkit/client"
	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// A custom provisioner: look a user up by userName before creating it
// (the Okta/OneLogin flow), add it to a group idempotently, and iterate a
// filtered list with transparent pagination. The target here is a
// scimkit server over httptest; point it at any SCIM 2.0 endpoint.
func ExampleNew() {
	ts := httptest.NewServer(mustServer())
	defer ts.Close()

	ctx := context.Background()
	c, err := client.New(ts.URL, client.WithBearerToken("secret"))
	if err != nil {
		fmt.Println("client:", err)
		return
	}

	// Lookup is 0-or-1: nil means not found, not an error.
	u, err := c.Users().Lookup(ctx, filter.Eq("userName", "bjensen@example.com"))
	if err != nil {
		fmt.Println("lookup:", err)
		return
	}
	fmt.Println("found before create:", u != nil)

	// Typed views convert to the canonical Resource the client sends.
	user := scim.User{UserName: "bjensen@example.com", DisplayName: "Barbara Jensen"}
	res, _ := user.Resource()
	created, err := c.Users().Create(ctx, res)
	if err != nil {
		fmt.Println("create:", err)
		return
	}
	userID, _ := created.Get("id")

	group := scim.Group{DisplayName: "Tour Guides"}
	gres, _ := group.Resource()
	g, err := c.Groups().Create(ctx, gres)
	if err != nil {
		fmt.Println("create group:", err)
		return
	}
	groupID, _ := g.Get("id")

	// EnsureMember is idempotent: already-present is success.
	for range 2 {
		if _, err := c.Groups().EnsureMember(ctx, groupID.(string), userID.(string)); err != nil {
			fmt.Println("ensure member:", err)
			return
		}
	}

	// List follows pagination for you; the loop yields one error at most.
	for r, err := range c.Users().List(ctx, client.Query{Filter: filter.Sw("userName", "b")}) {
		if err != nil {
			fmt.Println("list:", err)
			return
		}
		name, _ := r.Get("userName")
		fmt.Println("listed:", name)
	}

	// Output:
	// found before create: false
	// listed: bjensen@example.com
}

// mustServer wires a minimal scimkit service provider for the example.
func mustServer() http.Handler {
	h, err := server.New(server.Config{
		BaseURL: "https://app.example.com/scim/v2",
		ResourceTypes: []server.Registration{
			{Type: scim.UserResourceType(), Store: memstore.New(scim.UserResourceType())},
			{Type: scim.GroupResourceType(), Store: memstore.New(scim.GroupResourceType())},
		},
		MaxFilterResults: 200,
	})
	if err != nil {
		panic(err)
	}
	return h
}
