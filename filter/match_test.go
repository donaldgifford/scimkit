package filter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/donaldgifford/scimkit/scim"
)

// matchUser builds the reference resource for evaluator tests: work email
// at example.com, home email at jensen.org — the cross-element trap needs
// exactly that split.
func matchUser(t *testing.T) *scim.Resource {
	t.Helper()
	active := true
	u := &scim.User{
		ID:       "2819c223",
		UserName: "bjensen@example.com",
		Name:     &scim.Name{GivenName: "Barbara", FamilyName: "Jensen"},
		Title:    "Tour Guide",
		Active:   &active,
		Emails: []scim.MultiValue{
			{Value: "bjensen@example.com", Type: "work", Primary: true},
			{Value: "babs@jensen.org", Type: "home"},
		},
		X509Certificates: []scim.Certificate{{Value: []byte("der-bytes")}},
		Enterprise:       &scim.EnterpriseUser{EmployeeNumber: "701984"},
	}
	res, err := u.Resource()
	require.NoError(t, err)
	res.SetMeta(scim.Meta{
		ResourceType: "User",
		LastModified: time.Date(2011, 5, 13, 4, 42, 34, 0, time.UTC),
	})
	return res
}

func TestMatch(t *testing.T) {
	res := matchUser(t)
	tests := []struct {
		name   string
		filter string
		want   bool
	}{
		{name: "eq match", filter: `userName eq "bjensen@example.com"`, want: true},
		{name: "eq no match", filter: `userName eq "other"`, want: false},
		{name: "eq folds case for caseExact=false", filter: `userName eq "BJENSEN@EXAMPLE.COM"`, want: true},
		{name: "attribute name folds case", filter: `USERNAME eq "bjensen@example.com"`, want: true},
		{name: "ne", filter: `userName ne "other"`, want: true},
		{name: "co", filter: `userName co "jensen"`, want: true},
		{name: "sw", filter: `userName sw "bjensen"`, want: true},
		{name: "sw folds case", filter: `userName sw "BJ"`, want: true},
		{name: "ew", filter: `userName ew ".com"`, want: true},
		{name: "string ordering", filter: `userName gt "a"`, want: true},
		{name: "boolean eq", filter: `active eq true`, want: true},
		{name: "boolean eq false", filter: `active eq false`, want: false},
		{name: "sub-attribute", filter: `name.givenName eq "Barbara"`, want: true},
		{name: "pr on assigned", filter: `title pr`, want: true},
		{name: "pr on unassigned", filter: `nickName pr`, want: false},
		{name: "pr on complex", filter: `name pr`, want: true},
		{name: "pr on multi-valued", filter: `emails pr`, want: true},
		{name: "pr on unassigned multi-valued", filter: `phoneNumbers pr`, want: false},
		{name: "pr on multi-valued sub-attribute", filter: `emails.type pr`, want: true},
		{
			name:   "existential sub-attribute dot notation",
			filter: `emails.value co "@jensen.org"`,
			want:   true,
		},
		{
			name:   "existential requires some element to match",
			filter: `emails.value co "@nowhere"`,
			want:   false,
		},
		{
			name:   "valuePath single clause",
			filter: `emails[type eq "work"]`,
			want:   true,
		},
		{
			name:   "valuePath correlates within one element",
			filter: `emails[type eq "work" and value co "@example.com"]`,
			want:   true,
		},
		{
			name: "cross-element false positive rejected",
			// type "work" comes from element 0, "@jensen.org" from element
			// 1 — a flatten-then-filter evaluator would wrongly match.
			filter: `emails[type eq "work" and value co "@jensen.org"]`,
			want:   false,
		},
		{
			name:   "valuePath not",
			filter: `emails[not (type eq "work")]`,
			want:   true,
		},
		{
			name:   "valuePath pr",
			filter: `emails[primary pr]`,
			want:   true,
		},
		{
			name:   "valuePath undefined sub-attribute is no-value",
			filter: `emails[nosuch eq "x"]`,
			want:   false,
		},
		{name: "and", filter: `userName pr and active eq true`, want: true},
		{name: "and short", filter: `userName pr and nickName pr`, want: false},
		{name: "or", filter: `nickName pr or title pr`, want: true},
		{name: "not", filter: `not (nickName pr)`, want: true},
		{
			name:   "extension attribute via URN",
			filter: scim.SchemaEnterpriseUser + `:employeeNumber eq "701984"`,
			want:   true,
		},
		{
			name:   "extension URN folds case",
			filter: `URN:IETF:PARAMS:SCIM:SCHEMAS:EXTENSION:ENTERPRISE:2.0:USER:employeeNumber pr`,
			want:   true,
		},
		{
			name:   "core URN prefix accepted",
			filter: scim.SchemaUser + `:userName pr`,
			want:   true,
		},
		{name: "common id caseExact", filter: `id eq "2819C223"`, want: false},
		{name: "common id exact match", filter: `id eq "2819c223"`, want: true},
		{
			name:   "meta dateTime chronological gt",
			filter: `meta.lastModified gt "2011-05-13T04:42:33Z"`,
			want:   true,
		},
		{
			name: "meta dateTime equal across offsets",
			// Same instant written in a +02:00 offset must compare equal.
			filter: `meta.lastModified eq "2011-05-13T06:42:34+02:00"`,
			want:   true,
		},
		{
			name:   "meta dateTime lt is false for earlier literal",
			filter: `meta.lastModified lt "2011-05-13T04:42:33Z"`,
			want:   false,
		},
		{
			name:   "schemas multi-valued existential",
			filter: `schemas eq "` + scim.SchemaEnterpriseUser + `"`,
			want:   true,
		},
		{name: "undefined attribute is no-value", filter: `favoriteColor eq "teal"`, want: false},
		{name: "undefined attribute pr", filter: `favoriteColor pr`, want: false},
		{
			name:   "unknown URN is no-value",
			filter: `urn:example:unknown:2.0:Thing:field eq "x"`,
			want:   false,
		},
		{name: "eq null matches unassigned", filter: `nickName eq null`, want: true},
		{name: "eq null on assigned", filter: `title eq null`, want: false},
		{name: "ne null on assigned", filter: `title ne null`, want: true},
		{name: "ne null on unassigned", filter: `nickName ne null`, want: false},
		{
			name:   "type mismatch is a non-match",
			filter: `userName eq 42`,
			want:   false,
		},
		{
			name:   "binary eq on decoded bytes",
			filter: `x509Certificates.value eq "ZGVyLWJ5dGVz"`,
			want:   true,
		},
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

func TestMatchNumericComparison(t *testing.T) {
	schema := scim.Schema{
		ID:   "urn:example:2.0:Widget",
		Name: "Widget",
		Attributes: []scim.Attribute{
			{Name: "count", Type: scim.TypeInteger},
			{Name: "ratio", Type: scim.TypeDecimal},
			{Name: "big", Type: scim.TypeInteger},
		},
	}.WithDefaults()
	rt := scim.ResourceType{Name: "Widget", Endpoint: "/Widgets", Schema: schema}
	res := scim.NewResource(&rt)
	require.NoError(t, res.Set("count", 42))
	require.NoError(t, res.Set("ratio", 2.5))
	// 2^53+1: the first integer float64 cannot represent. Comparing in
	// float space would collapse it onto 2^53 and match the wrong literal.
	require.NoError(t, res.Set("big", int64(9007199254740993)))

	tests := []struct {
		name   string
		filter string
		want   bool
	}{
		{name: "integer eq", filter: `count eq 42`, want: true},
		{name: "integer gt", filter: `count gt 41`, want: true},
		{name: "integer le", filter: `count le 41`, want: false},
		{name: "integer compared to decimal literal", filter: `count eq 42.0`, want: true},
		{name: "decimal lt", filter: `ratio lt 3`, want: true},
		{name: "decimal ge", filter: `ratio ge 2.5`, want: true},
		{name: "decimal ne", filter: `ratio ne 2.5`, want: false},
		{name: "int64 exact above 2^53", filter: `big eq 9007199254740993`, want: true},
		{name: "int64 adjacent literal above 2^53", filter: `big eq 9007199254740992`, want: false},
		{name: "int64 ordering exact above 2^53", filter: `big gt 9007199254740992`, want: true},
		{name: "int64 ne adjacent above 2^53", filter: `big ne 9007199254740992`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.filter)
			require.NoError(t, err)
			got, err := Match(res, e)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchInvalidFilterErrors(t *testing.T) {
	res := matchUser(t)
	tests := []struct {
		name   string
		filter string
	}{
		{name: "ordering on boolean", filter: `active gt true`},
		{name: "ordering on binary", filter: `x509Certificates.value lt "AA=="`},
		{name: "ordering on boolean inside valuePath", filter: `emails[primary gt true]`},
		{name: "ordering on boolean nested in logic", filter: `title pr and active le false`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.filter)
			require.NoError(t, err)
			_, err = Match(res, e)
			var scimErr *scim.Error
			require.ErrorAs(t, err, &scimErr)
			assert.Equal(t, scim.ScimTypeInvalidFilter, scimErr.ScimType)
		})
	}
}

// TestMatchValuePathOnSingleComplex pins the one-element-list behavior for
// valuePath against a single-valued complex attribute.
func TestMatchValuePathOnSingleComplex(t *testing.T) {
	res := matchUser(t)
	tests := []struct {
		name   string
		filter string
		want   bool
	}{
		{name: "matching predicate", filter: `name[givenName eq "Barbara"]`, want: true},
		{name: "non-matching predicate", filter: `name[givenName eq "Nope"]`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.filter)
			require.NoError(t, err)
			got, err := Match(res, e)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
