package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

// TestRFCFigure2Examples parses every example filter from RFC 7644
// §3.4.2.2 Figure 2 to its expected AST, in the RFC's order.
func TestRFCFigure2Examples(t *testing.T) {
	coreUser := "urn:ietf:params:scim:schemas:core:2.0:User"
	lastModified := Path{Attr: "meta", SubAttr: "lastModified"}
	stamp := "2011-05-13T04:42:34Z"
	workEmails := ValuePath{
		Path: Path{Attr: "emails"},
		Filter: AndExpr{
			L: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "work"},
			R: Compare{Path: Path{Attr: "value"}, Op: OpContains, Value: "@example.com"},
		},
	}
	emailsOrValue := OrExpr{
		L: Compare{Path: Path{Attr: "emails"}, Op: OpContains, Value: "example.com"},
		R: Compare{Path: Path{Attr: "emails", SubAttr: "value"}, Op: OpContains, Value: "example.org"},
	}
	tests := []struct {
		name  string
		input string
		want  Expr
	}{
		{
			name:  "userName eq",
			input: `userName eq "bjensen"`,
			want:  Compare{Path: Path{Attr: "userName"}, Op: OpEqual, Value: "bjensen"},
		},
		{
			name:  "familyName co",
			input: `name.familyName co "O'Malley"`,
			want:  Compare{Path: Path{Attr: "name", SubAttr: "familyName"}, Op: OpContains, Value: "O'Malley"},
		},
		{
			name:  "userName sw",
			input: `userName sw "J"`,
			want:  Compare{Path: Path{Attr: "userName"}, Op: OpStartsWith, Value: "J"},
		},
		{
			name:  "fully qualified userName sw",
			input: coreUser + `:userName sw "J"`,
			want:  Compare{Path: Path{URN: coreUser, Attr: "userName"}, Op: OpStartsWith, Value: "J"},
		},
		{
			name:  "title pr",
			input: `title pr`,
			want:  Present{Path: Path{Attr: "title"}},
		},
		{
			name:  "lastModified gt",
			input: `meta.lastModified gt "` + stamp + `"`,
			want:  Compare{Path: lastModified, Op: OpGreaterThan, Value: stamp},
		},
		{
			name:  "lastModified ge",
			input: `meta.lastModified ge "` + stamp + `"`,
			want:  Compare{Path: lastModified, Op: OpGreaterThanOrEqual, Value: stamp},
		},
		{
			name:  "lastModified lt",
			input: `meta.lastModified lt "` + stamp + `"`,
			want:  Compare{Path: lastModified, Op: OpLessThan, Value: stamp},
		},
		{
			name:  "lastModified le",
			input: `meta.lastModified le "` + stamp + `"`,
			want:  Compare{Path: lastModified, Op: OpLessThanOrEqual, Value: stamp},
		},
		{
			name:  "title pr and userType eq",
			input: `title pr and userType eq "Employee"`,
			want: AndExpr{
				L: Present{Path: Path{Attr: "title"}},
				R: Compare{Path: Path{Attr: "userType"}, Op: OpEqual, Value: "Employee"},
			},
		},
		{
			name:  "title pr or userType eq",
			input: `title pr or userType eq "Intern"`,
			want: OrExpr{
				L: Present{Path: Path{Attr: "title"}},
				R: Compare{Path: Path{Attr: "userType"}, Op: OpEqual, Value: "Intern"},
			},
		},
		{
			name:  "schemas eq",
			input: `schemas eq "urn:ietf:params:scim:api:messages:2.0:PatchOp"`,
			want: Compare{
				Path:  Path{Attr: "schemas"},
				Op:    OpEqual,
				Value: "urn:ietf:params:scim:api:messages:2.0:PatchOp",
			},
		},
		{
			name:  "grouped or under and",
			input: `userType eq "Employee" and (emails co "example.com" or emails.value co "example.org")`,
			want: AndExpr{
				L: Compare{Path: Path{Attr: "userType"}, Op: OpEqual, Value: "Employee"},
				R: emailsOrValue,
			},
		},
		{
			name:  "ne with not group",
			input: `userType ne "Employee" and not (emails co "example.com" or emails.value co "example.org")`,
			want: AndExpr{
				L: Compare{Path: Path{Attr: "userType"}, Op: OpNotEqual, Value: "Employee"},
				R: NotExpr{E: emailsOrValue},
			},
		},
		{
			name:  "grouped sub-attribute compare",
			input: `userType eq "Employee" and (emails.type eq "work")`,
			want: AndExpr{
				L: Compare{Path: Path{Attr: "userType"}, Op: OpEqual, Value: "Employee"},
				R: Compare{Path: Path{Attr: "emails", SubAttr: "type"}, Op: OpEqual, Value: "work"},
			},
		},
		{
			name:  "valuePath conjunction",
			input: `userType eq "Employee" and emails[type eq "work" and value co "@example.com"]`,
			want: AndExpr{
				L: Compare{Path: Path{Attr: "userType"}, Op: OpEqual, Value: "Employee"},
				R: workEmails,
			},
		},
		{
			name:  "two valuePaths",
			input: `emails[type eq "work" and value co "@example.com"] or ims[type eq "xmpp" and value co "@foo.com"]`,
			want: OrExpr{
				L: workEmails,
				R: ValuePath{
					Path: Path{Attr: "ims"},
					Filter: AndExpr{
						L: Compare{Path: Path{Attr: "type"}, Op: OpEqual, Value: "xmpp"},
						R: Compare{Path: Path{Attr: "value"}, Op: OpContains, Value: "@foo.com"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestTable3OperatorMatrix exercises every RFC 7644 Table 3 operator
// against representative attribute types through Match.
func TestTable3OperatorMatrix(t *testing.T) {
	res := matchUser(t)
	tests := []struct {
		name   string
		filter string
		want   bool
	}{
		// eq — equal (case-insensitive for caseExact=false strings)
		{name: "eq string", filter: `userName eq "BJENSEN@example.com"`, want: true},
		{name: "eq boolean", filter: `active eq true`, want: true},
		{name: "eq dateTime", filter: `meta.lastModified eq "2011-05-13T04:42:34Z"`, want: true},
		// ne — not equal
		{name: "ne string", filter: `userName ne "someone@else.com"`, want: true},
		{name: "ne same value", filter: `userName ne "bjensen@example.com"`, want: false},
		// co — contains
		{name: "co match", filter: `title co "our"`, want: true},
		{name: "co no match", filter: `title co "zzz"`, want: false},
		// sw — starts with
		{name: "sw match", filter: `title sw "Tour"`, want: true},
		{name: "sw no match", filter: `title sw "Guide"`, want: false},
		// ew — ends with
		{name: "ew match", filter: `title ew "Guide"`, want: true},
		{name: "ew no match", filter: `title ew "Tour"`, want: false},
		// pr — present, has a non-empty value
		{name: "pr assigned", filter: `userName pr`, want: true},
		{name: "pr unassigned", filter: `nickName pr`, want: false},
		// gt/ge/lt/le — ordering on strings and dateTimes
		{name: "gt string", filter: `title gt "Toor"`, want: true},
		{name: "ge string equal", filter: `title ge "tour guide"`, want: true},
		{name: "lt dateTime", filter: `meta.lastModified lt "2020-01-01T00:00:00Z"`, want: true},
		{name: "le dateTime equal", filter: `meta.lastModified le "2011-05-13T04:42:34Z"`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.filter)
			require.NoError(t, err)
			got, err := Match(res, e)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got, "filter: %s", tt.filter)
		})
	}
}

// TestObservedIdPFilters covers the filter shapes Okta, Entra ID, and
// OneLogin actually send during provisioning: correlation lookups by
// userName and externalId, and extension-URN qualified paths.
func TestObservedIdPFilters(t *testing.T) {
	res := matchUser(t)
	require.NoError(t, res.Set("externalId", "00u1abcd"))

	tests := []struct {
		name string
		// source names the IdP behavior the shape comes from.
		source string
		filter string
		want   bool
	}{
		{
			name:   "okta userName correlation",
			source: "Okta SCIM: GET /Users?filter=userName eq ... on every provision",
			filter: `userName eq "bjensen@example.com"`,
			want:   true,
		},
		{
			name:   "entra externalId correlation",
			source: "Entra ID: externalId eq ... matching to objectId",
			filter: `externalId eq "00u1abcd"`,
			want:   true,
		},
		{
			name:   "entra externalId no match",
			source: "Entra ID correlation miss must return an empty list, not an error",
			filter: `externalId eq "different"`,
			want:   false,
		},
		{
			name:   "extension URN qualified path",
			source: "Entra ID filters on enterprise extension attributes with full URN",
			filter: scim.SchemaEnterpriseUser + `:employeeNumber eq "701984"`,
			want:   true,
		},
		{
			name:   "okta group displayName",
			source: "Okta SCIM: GET /Groups?filter=displayName eq ...",
			filter: `displayName eq "nonexistent"`,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.filter)
			require.NoError(t, err, "source: %s", tt.source)
			got, err := Match(res, e)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got, "filter: %s (source: %s)", tt.filter, tt.source)
		})
	}
}
