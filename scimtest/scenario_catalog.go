package scimtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/donaldgifford/scimkit/internal/idpcorpus"
)

// Built-in scenario names. Each names a sequence LookupScenario resolves.
const (
	ScenarioRFCStrictLifecycle   = "rfc-strict-lifecycle"
	ScenarioEntraUserLifecycle   = "entra-user-lifecycle"
	ScenarioEntraGroupMembership = "entra-group-membership"
	ScenarioOktaUserLifecycle    = "okta-user-lifecycle"
	ScenarioOktaGroupPUT         = "okta-group-put"
	ScenarioOneLoginLookupDance  = "onelogin-lookup-dance"
)

const (
	usersEndpoint  = "/Users"
	groupsEndpoint = "/Groups"
	userSchema     = "urn:ietf:params:scim:schemas:core:2.0:User"
	groupSchema    = "urn:ietf:params:scim:schemas:core:2.0:Group"
	patchSchema    = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
)

// ScenarioNames lists the built-in scenarios in catalog order.
func ScenarioNames() []string {
	return []string{
		ScenarioRFCStrictLifecycle,
		ScenarioEntraUserLifecycle,
		ScenarioEntraGroupMembership,
		ScenarioOktaUserLifecycle,
		ScenarioOktaGroupPUT,
		ScenarioOneLoginLookupDance,
	}
}

// LookupScenario resolves a built-in scenario by name. IdP scenarios embed
// the golden corpus's real request bodies, so a target that tolerates that
// provider's quirks passes and one that does not fails with a useful diff.
func LookupScenario(name string) (Scenario, error) {
	fx, err := loadFixtures()
	if err != nil {
		return Scenario{}, err
	}
	switch name {
	case ScenarioRFCStrictLifecycle:
		return rfcStrictLifecycle()
	case ScenarioEntraUserLifecycle:
		return entraUserLifecycle(fx)
	case ScenarioEntraGroupMembership:
		return entraGroupMembership()
	case ScenarioOktaUserLifecycle:
		return oktaUserLifecycle(fx)
	case ScenarioOktaGroupPUT:
		return oktaGroupPUT(fx)
	case ScenarioOneLoginLookupDance:
		return oneLoginLookupDance(fx)
	default:
		return Scenario{}, fmt.Errorf("scimtest: unknown scenario %q (known: %v)", name, ScenarioNames())
	}
}

// fixtureSet indexes the golden corpus by "provider/name".
type fixtureSet map[string]json.RawMessage

func loadFixtures() (fixtureSet, error) {
	all, err := idpcorpus.All()
	if err != nil {
		return nil, fmt.Errorf("scimtest: load corpus: %w", err)
	}
	fx := make(fixtureSet, len(all))
	for i := range all {
		fx[all[i].Provider+"/"+all[i].Name] = all[i].Request
	}
	return fx, nil
}

// scenarioBuilder accumulates steps, deferring the first corpus-lookup
// error until build.
type scenarioBuilder struct {
	fx    fixtureSet
	steps []Step
	err   error
}

//nolint:gocritic // Step is passed by value so builders read as fluent literals
func (b *scenarioBuilder) step(s Step) *scenarioBuilder {
	b.steps = append(b.steps, s)
	return b
}

// corpus returns a fixture's PatchOp body, recording an error if the
// fixture is absent (a programming error — the corpus is compiled in).
func (b *scenarioBuilder) corpus(key string) json.RawMessage {
	if b.err != nil {
		return nil
	}
	body, ok := b.fx[key]
	if !ok {
		b.err = fmt.Errorf("scimtest: corpus fixture %q not found", key)
	}
	return body
}

func (b *scenarioBuilder) build(name, desc string) (Scenario, error) {
	if b.err != nil {
		return Scenario{}, b.err
	}
	sc := Scenario{Name: name, Description: desc, Steps: b.steps}
	if err := validateScenario(sc); err != nil {
		return Scenario{}, err
	}
	return sc, nil
}

// validateScenario rejects a malformed scenario so a bad expectation is a
// build error, never a silent step "failure".
func validateScenario(sc Scenario) error {
	if len(sc.Steps) == 0 {
		return fmt.Errorf("scimtest: scenario %q has no steps", sc.Name)
	}
	for i := range sc.Steps {
		s := &sc.Steps[i]
		if s.Method == "" {
			return fmt.Errorf("scimtest: scenario %q step %d: missing method", sc.Name, i)
		}
		if s.Expect.Status == 0 {
			return fmt.Errorf("scimtest: scenario %q step %q: expectation has no status", sc.Name, s.Name)
		}
	}
	return nil
}

// lenOf returns a pointer for a Len assertion.
func lenOf(n int) *int { return &n }

// userFilterPath builds a filtered GET path selecting a user by userName.
func userFilterPath(userName string) string {
	q := url.Values{}
	q.Set("filter", fmt.Sprintf("userName eq %q", userName))
	return usersEndpoint + "?" + q.Encode()
}

// captureID captures the created resource id as {{.uid}} or {{.gid}}.
func captureID(varName string) []Capture { return []Capture{{Var: varName, Path: "id"}} }

// rfcStrictLifecycle is the RFC-compliant baseline: it uses only shapes a
// strict server must accept, so it validates the mock itself.
func rfcStrictLifecycle() (Scenario, error) {
	create := jsonf(`{"schemas":[%q],"userName":"rfc.user@example.com","active":true}`, userSchema)
	activePatch := jsonf(`{"schemas":[%q],"Operations":[{"op":"replace","path":"active","value":false}]}`, patchSchema)
	var b scenarioBuilder
	b.step(Step{
		Name: "create user", Method: http.MethodPost, Path: usersEndpoint, Body: create,
		Capture: captureID("uid"),
		Expect:  Expectation{Status: http.StatusCreated, Body: assertUserName("rfc.user@example.com")},
	}).step(Step{
		Name: "get user", Method: http.MethodGet, Path: "/Users/{{.uid}}",
		Expect: Expectation{Status: http.StatusOK, Body: assertUserName("rfc.user@example.com")},
	}).step(Step{
		Name: "deactivate via RFC patch", Method: http.MethodPatch, Path: "/Users/{{.uid}}", Body: activePatch,
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "active", Value: false}}},
	}).step(Step{
		Name: "lookup finds one", Method: http.MethodGet, Path: userFilterPath("rfc.user@example.com"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "totalResults", Value: 1}}},
	}).step(Step{
		Name: "delete user", Method: http.MethodDelete, Path: "/Users/{{.uid}}",
		Expect: Expectation{Status: http.StatusNoContent},
	}).step(Step{
		Name: "get after delete is 404", Method: http.MethodGet, Path: "/Users/{{.uid}}",
		Expect: Expectation{Status: http.StatusNotFound},
	})
	return b.build(ScenarioRFCStrictLifecycle,
		"RFC-compliant user lifecycle: create, read, deactivate, lookup, delete")
}

// entraUserLifecycle replays Entra ID's documented PATCH shapes; it passes
// against a server running the Entra profile.
func entraUserLifecycle(fx fixtureSet) (Scenario, error) {
	create := jsonf(`{"schemas":[%q],"userName":"bjensen@example.com",`+
		`"name":{"givenName":"Barb","familyName":"Jensen"},"active":true}`, userSchema)
	b := scenarioBuilder{fx: fx}
	b.step(Step{
		Name: "create user", Method: http.MethodPost, Path: usersEndpoint, Body: create,
		Capture: captureID("uid"),
		Expect:  Expectation{Status: http.StatusCreated, Body: []BodyAssertion{{Path: "active", Value: true}}},
	}).step(Step{
		Name:   "deactivate (wrapped-string replace)",
		Method: http.MethodPatch, Path: "/Users/{{.uid}}", Body: b.corpus("entra/replace-active-wrapped-string"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "active", Value: false}}},
	}).step(Step{
		Name:   "profile update (pathless flattened keys)",
		Method: http.MethodPatch, Path: "/Users/{{.uid}}", Body: b.corpus("entra/pathless-replace-flattened-keys"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{
			{Path: "displayName", Value: "Babs Jensen"},
			{Path: "name.givenName", Value: "Barbara"},
			{Path: "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User.department", Value: "Tour Operations"},
		}},
	}).step(Step{
		Name:   "set manager (value object)",
		Method: http.MethodPatch, Path: "/Users/{{.uid}}", Body: b.corpus("entra/add-manager-value-object"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{
			{
				Path:  "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User.manager.value",
				Value: "26118915-6090-4610-87e4-49d8ca9f808d",
			},
		}},
	}).step(Step{
		Name:   "add address (valuePath synthesis)",
		Method: http.MethodPatch, Path: "/Users/{{.uid}}", Body: b.corpus("entra/add-valuepath-synthesizes-element"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "addresses", Len: lenOf(1)}}},
	}).step(Step{
		Name: "confirm", Method: http.MethodGet, Path: "/Users/{{.uid}}",
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "active", Value: false}}},
	})
	return b.build(ScenarioEntraUserLifecycle,
		"Entra ID user lifecycle: wrapped-string deactivate, flattened-key profile edit, manager, address")
}

// entraGroupMembership is the one-member-per-PATCH storm real IdPs send
// instead of a single multi-member add. It is RFC-shaped and passes on any
// profile.
func entraGroupMembership() (Scenario, error) {
	var b scenarioBuilder
	b.step(Step{
		Name: "create group", Method: http.MethodPost, Path: groupsEndpoint,
		Body:    jsonf(`{"schemas":[%q],"displayName":"Engineering"}`, groupSchema),
		Capture: captureID("gid"),
		Expect:  Expectation{Status: http.StatusCreated},
	})
	for i := 1; i <= 3; i++ {
		v := fmt.Sprintf("u%d", i)
		b.step(Step{
			Name: "create " + v, Method: http.MethodPost, Path: usersEndpoint,
			Body:    jsonf(`{"schemas":[%q],"userName":%q}`, userSchema, v+"@example.com"),
			Capture: []Capture{{Var: v, Path: "id"}},
			Expect:  Expectation{Status: http.StatusCreated},
		})
	}
	for i := 1; i <= 3; i++ {
		b.step(Step{
			Name:   fmt.Sprintf("add member %d", i),
			Method: http.MethodPatch, Path: "/Groups/{{.gid}}",
			Body: jsonf(`{"schemas":[%q],"Operations":[{"op":"add","path":"members","value":[{"value":"{{.u%d}}"}]}]}`,
				patchSchema, i),
			Expect: Expectation{Status: http.StatusOK},
		})
	}
	b.step(Step{
		Name: "confirm three members", Method: http.MethodGet, Path: "/Groups/{{.gid}}",
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "members", Len: lenOf(3)}}},
	})
	return b.build(ScenarioEntraGroupMembership,
		"Group membership built one member per PATCH, the way IdPs actually provision it")
}

// oktaUserLifecycle is Okta's lookup-before-create, then PUT-based update,
// then pathless-replace deactivate.
func oktaUserLifecycle(fx fixtureSet) (Scenario, error) {
	const uname = "okta.user@example.com"
	create := jsonf(`{"schemas":[%q],"userName":%q,"active":true}`, userSchema, uname)
	put := jsonf(`{"schemas":[%q],"userName":%q,"displayName":"Okta User","active":true}`, userSchema, uname)
	b := scenarioBuilder{fx: fx}
	b.step(Step{
		Name: "lookup before create", Method: http.MethodGet, Path: userFilterPath(uname),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "totalResults", Value: 0}}},
	}).step(Step{
		Name: "create user", Method: http.MethodPost, Path: usersEndpoint, Body: create,
		Capture: captureID("uid"),
		Expect:  Expectation{Status: http.StatusCreated},
	}).step(Step{
		Name: "full replace (PUT)", Method: http.MethodPut, Path: "/Users/{{.uid}}", Body: put,
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "displayName", Value: "Okta User"}}},
	}).step(Step{
		Name:   "deactivate (pathless plain-key replace)",
		Method: http.MethodPatch, Path: "/Users/{{.uid}}", Body: b.corpus("okta/pathless-replace-plain-keys"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "active", Value: false}}},
	}).step(Step{
		Name: "lookup after create", Method: http.MethodGet, Path: userFilterPath(uname),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "totalResults", Value: 1}}},
	})
	return b.build(ScenarioOktaUserLifecycle,
		"Okta user lifecycle: lookup, create, PUT replace, pathless-replace deactivate")
}

// oktaGroupPUT makes Okta's destructive-PUT behavior visible: a PUT that
// omits members wipes them (RFC §3.5.1), which this scenario documents
// rather than papers over.
func oktaGroupPUT(fx fixtureSet) (Scenario, error) {
	create := jsonf(`{"schemas":[%q],"displayName":"Tour Guides","members":[`+
		`{"value":"2819c223-7f76-453a-919d-413861904646"},`+
		`{"value":"902c246b-6245-4190-8e05-00816be7344a"}]}`, groupSchema)
	put := jsonf(`{"schemas":[%q],"displayName":"Tour Guides"}`, groupSchema)
	b := scenarioBuilder{fx: fx}
	b.step(Step{
		Name: "create group with two members", Method: http.MethodPost, Path: groupsEndpoint, Body: create,
		Capture: captureID("gid"),
		Expect:  Expectation{Status: http.StatusCreated, Body: []BodyAssertion{{Path: "members", Len: lenOf(2)}}},
	}).step(Step{
		Name:   "remove one member by value",
		Method: http.MethodPatch, Path: "/Groups/{{.gid}}", Body: b.corpus("okta/remove-member-by-value"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "members", Len: lenOf(1)}}},
	}).step(Step{
		Name: "PUT without members wipes them", Method: http.MethodPut, Path: "/Groups/{{.gid}}", Body: put,
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "members", Unassigned: true}}},
	}).step(Step{
		Name: "confirm members gone", Method: http.MethodGet, Path: "/Groups/{{.gid}}",
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "members", Unassigned: true}}},
	})
	return b.build(ScenarioOktaGroupPUT,
		"Okta group PUT: remove a member, then a members-less PUT that wipes the rest (RFC 3.5.1)")
}

// oneLoginLookupDance is OneLogin's correlate-by-filter flow across a
// username rename, plus its duplicate-group behavior (memstore does not
// constrain displayName, so both creates succeed).
func oneLoginLookupDance(fx fixtureSet) (Scenario, error) {
	create := jsonf(`{"schemas":[%q],"userName":"bjensen@example.com","emails":[`+
		`{"value":"bjensen@example.com","type":"work"},`+
		`{"value":"babs@home.example.com","type":"home"}]}`, userSchema)
	sales := jsonf(`{"schemas":[%q],"displayName":"Sales"}`, groupSchema)
	b := scenarioBuilder{fx: fx}
	b.step(Step{
		Name: "lookup before create", Method: http.MethodGet, Path: userFilterPath("bjensen@example.com"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "totalResults", Value: 0}}},
	}).step(Step{
		Name: "create user", Method: http.MethodPost, Path: usersEndpoint, Body: create,
		Capture: captureID("uid"),
		Expect:  Expectation{Status: http.StatusCreated, Body: []BodyAssertion{{Path: "emails", Len: lenOf(2)}}},
	}).step(Step{
		Name:   "rename username",
		Method: http.MethodPatch, Path: "/Users/{{.uid}}", Body: b.corpus("onelogin/replace-username"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{
			{Path: "userName", Value: "barbara.jensen@example.com"},
		}},
	}).step(Step{
		Name:   "remove home email by filter",
		Method: http.MethodPatch, Path: "/Users/{{.uid}}", Body: b.corpus("onelogin/remove-email-by-filter"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "emails", Len: lenOf(1)}}},
	}).step(Step{
		Name: "lookup after rename correlates", Method: http.MethodGet, Path: userFilterPath("barbara.jensen@example.com"),
		Expect: Expectation{Status: http.StatusOK, Body: []BodyAssertion{{Path: "totalResults", Value: 1}}},
	}).step(Step{
		Name: "create Sales group", Method: http.MethodPost, Path: groupsEndpoint, Body: sales,
		Expect: Expectation{Status: http.StatusCreated},
	}).step(Step{
		Name: "create Sales group again (duplicate allowed)", Method: http.MethodPost, Path: groupsEndpoint, Body: sales,
		Expect: Expectation{Status: http.StatusCreated},
	})
	return b.build(ScenarioOneLoginLookupDance,
		"OneLogin correlate-by-filter across a username rename, plus duplicate-group creation")
}

// assertUserName is a common single-field body assertion.
func assertUserName(name string) []BodyAssertion {
	return []BodyAssertion{{Path: "userName", Value: name}}
}

// jsonf formats a JSON body. The result is raw request bytes; any "{{...}}"
// survives for Simulate's template pass.
func jsonf(format string, args ...any) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(format, args...))
}
