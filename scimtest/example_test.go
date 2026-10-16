package scimtest_test

import (
	"context"
	"fmt"
	"net/http/httptest"

	"github.com/donaldgifford/scimkit/client"
	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scimtest"
)

// Point a provisioner at an in-memory SCIM mock. In a test, prefer
// scimtest.NewServer(t, opts...), which wraps the same Mock in an
// httptest.Server and tears it down via t.Cleanup; NewMock is the
// test-free core shown here so the example runs without a *testing.T.
func ExampleNewMock() {
	mock, err := scimtest.NewMock(
		scimtest.WithBearerToken("secret"),
		scimtest.WithSeedJSON([]byte(`{"users": [
			{"schemas": ["urn:ietf:params:scim:schemas:core:2.0:User"],
			 "userName": "bjensen@example.com"}
		]}`)),
	)
	if err != nil {
		fmt.Println("mock:", err)
		return
	}
	ts := httptest.NewServer(mock.Handler())
	defer ts.Close()

	// The code under test: a provisioner doing an Okta-style lookup.
	c, err := client.New(ts.URL, client.WithBearerToken("secret"))
	if err != nil {
		fmt.Println("client:", err)
		return
	}
	res, err := c.Users().Lookup(context.Background(), filter.Eq("userName", "bjensen@example.com"))
	if err != nil {
		fmt.Println("lookup:", err)
		return
	}
	userName, _ := res.Get("userName")
	fmt.Println("found:", userName)

	// Assert on the wire traffic the provisioner produced.
	last, _ := mock.Recorder().Last()
	fmt.Println("requests:", mock.Recorder().Count())
	fmt.Println("last:", last.Method, last.Path)
	// Output:
	// found: bjensen@example.com
	// requests: 1
	// last: GET /Users
}

// Replay a built-in IdP scenario against a target and inspect the report.
// A target that tolerates that provider's quirks passes; one that does not
// fails at the first non-conforming step.
func ExampleSimulate() {
	mock, err := scimtest.NewMock()
	if err != nil {
		fmt.Println("mock:", err)
		return
	}
	ts := httptest.NewServer(mock.Handler())
	defer ts.Close()

	scenario, err := scimtest.LookupScenario(scimtest.ScenarioRFCStrictLifecycle)
	if err != nil {
		fmt.Println("scenario:", err)
		return
	}
	report, err := scimtest.Simulate(context.Background(), ts.URL, "", scenario)
	if err != nil {
		fmt.Println("simulate:", err)
		return
	}
	fmt.Println("scenario:", report.Scenario)
	fmt.Println("passed:", report.Passed)
	fmt.Println("steps:", len(report.Steps))
	// Output:
	// scenario: rfc-strict-lifecycle
	// passed: true
	// steps: 6
}
