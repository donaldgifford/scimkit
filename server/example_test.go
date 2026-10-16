package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
	"github.com/donaldgifford/scimkit/server"
	"github.com/donaldgifford/scimkit/server/memstore"
)

// A complete SCIM service provider: register the built-in User and Group
// types against the in-memory reference store, mount the handler under a
// prefix, and it answers CRUD, PATCH, filtered list, and discovery. Swap
// memstore for your own server.Store to back it with a real database.
func ExampleNew() {
	handler, err := server.New(server.Config{
		BaseURL: "https://app.example.com/scim/v2",
		ResourceTypes: []server.Registration{
			{Type: scim.UserResourceType(), Store: memstore.New(scim.UserResourceType())},
			{Type: scim.GroupResourceType(), Store: memstore.New(scim.GroupResourceType())},
		},
		Profile:          patch.Default, // tolerant input, strict output
		MaxFilterResults: 200,
	})
	if err != nil {
		fmt.Println("server:", err)
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/scim/v2/", http.StripPrefix("/scim/v2", handler))

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Create a user the way an IdP would.
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"bjensen@example.com"}`
	status, created := do(ts.Client(), http.MethodPost, ts.URL+"/scim/v2/Users", body)
	fmt.Println("create:", status, created["userName"])

	// Read it back by id.
	status, fetched := do(ts.Client(), http.MethodGet, ts.URL+"/scim/v2/Users/"+created["id"].(string), "")
	fmt.Println("get:", status, fetched["meta"].(map[string]any)["resourceType"])

	// Output:
	// create: 201 bjensen@example.com
	// get: 200 User
}

// do sends one SCIM request and decodes the JSON body.
func do(hc *http.Client, method, url, body string) (int, map[string]any) {
	req, _ := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/scim+json")
	resp, err := hc.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}
