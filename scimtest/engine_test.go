package scimtest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustDoc(t *testing.T, s string) any {
	t.Helper()
	var doc any
	require.NoError(t, json.Unmarshal([]byte(s), &doc))
	return doc
}

func TestLookupPath(t *testing.T) {
	doc := mustDoc(t, `{
		"userName": "bjensen",
		"name": {"givenName": "Barbara"},
		"emails": [{"value": "a@x.com"}, {"value": "b@x.com"}],
		"active": false,
		"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User": {"department": "Sales"}
	}`)
	tests := []struct {
		name   string
		path   string
		want   any
		wantOK bool
	}{
		{name: "top-level string", path: "userName", want: "bjensen", wantOK: true},
		{name: "nested complex", path: "name.givenName", want: "Barbara", wantOK: true},
		{name: "array index", path: "emails.1.value", want: "b@x.com", wantOK: true},
		{name: "bool false is present", path: "active", want: false, wantOK: true},
		{
			name:   "urn key with embedded dots",
			path:   "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User.department",
			want:   "Sales",
			wantOK: true,
		},
		{name: "missing top-level", path: "nope", wantOK: false},
		{name: "missing nested", path: "name.familyName", wantOK: false},
		{name: "index out of range", path: "emails.9.value", wantOK: false},
		{name: "index into non-array", path: "userName.0", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := lookupPath(doc, tt.path)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestValueEqual(t *testing.T) {
	tests := []struct {
		name string
		want any
		got  any
		eq   bool
	}{
		{name: "int vs json float", want: 1, got: float64(1), eq: true},
		{name: "string match", want: "x", got: "x", eq: true},
		{name: "string mismatch", want: "x", got: "y", eq: false},
		{name: "bool match", want: false, got: false, eq: true},
		{name: "number mismatch", want: 2, got: float64(3), eq: false},
		{name: "type mismatch", want: "1", got: float64(1), eq: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.eq, valueEqual(tt.want, tt.got))
		})
	}
}

func TestSubstitute(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		vars    map[string]string
		want    string
		wantErr bool
	}{
		{name: "no template passthrough", in: "/Users", want: "/Users"},
		{name: "single var", in: "/Users/{{.uid}}", vars: map[string]string{"uid": "42"}, want: "/Users/42"},
		{name: "in body", in: `{"value":"{{.gid}}"}`, vars: map[string]string{"gid": "g1"}, want: `{"value":"g1"}`},
		{name: "missing var errors", in: "/Users/{{.uid}}", vars: map[string]string{}, wantErr: true},
		{name: "malformed template errors", in: "/Users/{{.uid", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := substitute(tt.in, tt.vars)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyCaptures(t *testing.T) {
	vars := map[string]string{}
	body := []byte(`{"id":"abc-123","meta":{"version":"W/\"1\""}}`)
	err := applyCaptures([]Capture{
		{Var: "uid", Path: "id"},
		{Var: "ver", Path: "meta.version"},
	}, body, vars)
	require.NoError(t, err)
	assert.Equal(t, "abc-123", vars["uid"])
	assert.Equal(t, `W/"1"`, vars["ver"])

	err = applyCaptures([]Capture{{Var: "x", Path: "missing"}}, body, vars)
	require.Error(t, err)

	err = applyCaptures([]Capture{{Var: "x", Path: "id"}}, []byte(`not json`), vars)
	require.Error(t, err)
}

func TestEvaluate(t *testing.T) {
	two := 2
	tests := []struct {
		name      string
		exp       Expectation
		status    int
		header    http.Header
		body      string
		wantFails int
	}{
		{
			name:   "clean pass",
			exp:    Expectation{Status: 200, Body: []BodyAssertion{{Path: "active", Value: false}}},
			status: 200,
			body:   `{"active": false}`,
		},
		{
			name:      "status mismatch",
			exp:       Expectation{Status: 200},
			status:    404,
			wantFails: 1,
		},
		{
			name:      "scimType match",
			exp:       Expectation{Status: 400, ScimType: "invalidValue"},
			status:    400,
			body:      `{"scimType": "invalidValue"}`,
			wantFails: 0,
		},
		{
			name:      "scimType mismatch",
			exp:       Expectation{Status: 400, ScimType: "invalidPath"},
			status:    400,
			body:      `{"scimType": "invalidValue"}`,
			wantFails: 1,
		},
		{
			name:      "len assertion pass",
			exp:       Expectation{Status: 200, Body: []BodyAssertion{{Path: "members", Len: &two}}},
			status:    200,
			body:      `{"members": [{"value":"a"},{"value":"b"}]}`,
			wantFails: 0,
		},
		{
			name:      "len assertion fail",
			exp:       Expectation{Status: 200, Body: []BodyAssertion{{Path: "members", Len: &two}}},
			status:    200,
			body:      `{"members": [{"value":"a"}]}`,
			wantFails: 1,
		},
		{
			name:      "unassigned pass",
			exp:       Expectation{Status: 200, Body: []BodyAssertion{{Path: "members", Unassigned: true}}},
			status:    200,
			body:      `{"displayName": "g"}`,
			wantFails: 0,
		},
		{
			name:      "unassigned honors empty-array equivalence",
			exp:       Expectation{Status: 200, Body: []BodyAssertion{{Path: "members", Unassigned: true}}},
			status:    200,
			body:      `{"members": []}`,
			wantFails: 0,
		},
		{
			name:      "unassigned fails when present",
			exp:       Expectation{Status: 200, Body: []BodyAssertion{{Path: "members", Unassigned: true}}},
			status:    200,
			body:      `{"members": [{"value":"a"}]}`,
			wantFails: 1,
		},
		{
			name:      "header match",
			exp:       Expectation{Status: 200, Headers: map[string]string{"Location": "/Users/1"}},
			status:    200,
			header:    http.Header{"Location": []string{"/Users/1"}},
			wantFails: 0,
		},
		{
			name:      "header mismatch",
			exp:       Expectation{Status: 200, Headers: map[string]string{"Location": "/Users/1"}},
			status:    200,
			header:    http.Header{},
			wantFails: 1,
		},
		{
			name:      "value assertion missing path",
			exp:       Expectation{Status: 200, Body: []BodyAssertion{{Path: "userName", Value: "x"}}},
			status:    200,
			body:      `{}`,
			wantFails: 1,
		},
		{
			name:      "total results numeric",
			exp:       Expectation{Status: 200, Body: []BodyAssertion{{Path: "totalResults", Value: 0}}},
			status:    200,
			body:      `{"totalResults": 0}`,
			wantFails: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := tt.header
			if h == nil {
				h = http.Header{}
			}
			fails := evaluate(tt.exp, tt.status, h, []byte(tt.body))
			assert.Len(t, fails, tt.wantFails, "failures: %v", fails)
		})
	}
}
