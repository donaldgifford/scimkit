package scim

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func fullUser() *User {
	return &User{
		ID:         "2819c223",
		ExternalID: "701984",
		Meta: Meta{
			ResourceType: "User",
			Created:      time.Date(2010, 1, 23, 4, 56, 22, 0, time.UTC),
			LastModified: time.Date(2011, 5, 13, 4, 42, 34, 0, time.UTC),
			Location:     "https://example.com/v2/Users/2819c223",
			Version:      `W/"3694e05e9dff590"`,
		},
		UserName: "bjensen@example.com",
		Name: &Name{
			Formatted:       "Ms. Barbara J Jensen, III",
			FamilyName:      "Jensen",
			GivenName:       "Barbara",
			MiddleName:      "Jane",
			HonorificPrefix: "Ms.",
			HonorificSuffix: "III",
		},
		DisplayName:       "Babs Jensen",
		NickName:          "Babs",
		ProfileURL:        "https://login.example.com/bjensen",
		Title:             "Tour Guide",
		UserType:          "Employee",
		PreferredLanguage: "en-US",
		Locale:            "en-US",
		Timezone:          "America/Los_Angeles",
		Active:            boolPtr(true),
		Password:          "t1meMa$heen",
		Emails: []MultiValue{
			{Value: "bjensen@example.com", Type: "work", Primary: true},
			{Value: "babs@jensen.org", Type: "home"},
		},
		PhoneNumbers: []MultiValue{{Value: "555-555-5555", Type: "work"}},
		IMs:          []MultiValue{{Value: "someaimhandle", Type: "aim"}},
		Photos:       []MultiValue{{Value: "https://photos.example.com/profilephoto/72930000000Ccne/F", Type: "photo"}},
		Addresses: []Address{
			{
				Formatted:     "100 Universal City Plaza\nHollywood, CA 91608 USA",
				StreetAddress: "100 Universal City Plaza",
				Locality:      "Hollywood",
				Region:        "CA",
				PostalCode:    "91608",
				Country:       "USA",
				Type:          "work",
			},
		},
		Groups: []GroupRef{
			{
				Value:   "e9e30dba",
				Ref:     "https://example.com/v2/Groups/e9e30dba",
				Display: "Tour Guides",
			},
		},
		Entitlements:     []MultiValue{{Value: "swimming"}},
		Roles:            []MultiValue{{Value: "guide", Primary: true}},
		X509Certificates: []Certificate{{Value: []byte{0x30, 0x82, 0x03, 0x75}, Type: "signing"}},
		Enterprise: &EnterpriseUser{
			EmployeeNumber: "701984",
			CostCenter:     "4130",
			Organization:   "Universal Studios",
			Division:       "Theme Park",
			Department:     "Tour Operations",
			Manager: &Manager{
				Value:       "26118915-6090-4610-87e4-49d8ca9f808d",
				Ref:         "../Users/26118915-6090-4610-87e4-49d8ca9f808d",
				DisplayName: "John Smith",
			},
		},
	}
}

// TestUserResourceRoundTrip pins the design's round-trip property:
// User → Resource → JSON → Resource → User must be lossless (modulo
// password, which is returned=never and thus absent from the wire).
func TestUserResourceRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		user *User
	}{
		{name: "full user", user: fullUser()},
		{name: "minimal user", user: &User{UserName: "min"}},
		{name: "active false is preserved", user: &User{UserName: "u", Active: boolPtr(false)}},
		{
			name: "extension without manager",
			user: &User{UserName: "u", Enterprise: &EnterpriseUser{Department: "Ops"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("typed to resource and back", func(t *testing.T) {
				res, err := tt.user.Resource()
				require.NoError(t, err)
				back, err := UserFromResource(res)
				require.NoError(t, err)
				assert.Equal(t, tt.user, back)
			})
			t.Run("through the wire", func(t *testing.T) {
				res, err := tt.user.Resource()
				require.NoError(t, err)
				wire, err := EncodeResource(res, Projection{})
				require.NoError(t, err)
				rt := UserResourceType()
				decoded, err := DecodeResource(wire, &rt)
				require.NoError(t, err)
				back, err := UserFromResource(decoded)
				require.NoError(t, err)

				want := *tt.user
				want.Password = "" // returned=never: absent from the wire by design
				assert.Equal(t, &want, back)
			})
		})
	}
}

func TestUserResourceMapping(t *testing.T) {
	res, err := fullUser().Resource()
	require.NoError(t, err)
	tests := []struct {
		name string
		path string
		want any
	}{
		{name: "schemas gains extension URN", path: "schemas", want: []string{SchemaUser, SchemaEnterpriseUser}},
		{name: "profileUrl is a reference", path: "profileUrl", want: ReferenceValue("https://login.example.com/bjensen")},
		{name: "photo value is a reference", path: "photos", want: MultiComplexValue{{
			"value": ReferenceValue("https://photos.example.com/profilephoto/72930000000Ccne/F"),
			"type":  "photo",
		}}},
		{name: "certificate value is binary", path: "x509Certificates", want: MultiComplexValue{{
			"value": []byte{0x30, 0x82, 0x03, 0x75},
			"type":  "signing",
		}}},
		{name: "manager display name", path: SchemaEnterpriseUser + ":manager.displayName", want: "John Smith"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := res.Get(tt.path)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCertificateBytesAreIndependent pins that mutating a typed view's
// certificate bytes cannot corrupt the source resource, and vice versa.
func TestCertificateBytesAreIndependent(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "typed view mutation does not reach the resource"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &User{X509Certificates: []Certificate{{Value: []byte{1, 2, 3}}}}
			res, err := user.Resource()
			require.NoError(t, err)

			view, err := UserFromResource(res)
			require.NoError(t, err)
			view.X509Certificates[0].Value[0] = 99

			stored, ok := res.Get("x509Certificates")
			require.True(t, ok)
			assert.Equal(t, []byte{1, 2, 3}, stored.(MultiComplexValue)[0]["value"])

			user.X509Certificates[0].Value[0] = 42
			stored, ok = res.Get("x509Certificates")
			require.True(t, ok)
			assert.Equal(t, []byte{1, 2, 3}, stored.(MultiComplexValue)[0]["value"])
		})
	}
}

func TestUserResourceErrors(t *testing.T) {
	tests := []struct {
		name string
		user *User
	}{
		{
			name: "two primary emails",
			user: &User{Emails: []MultiValue{
				{Value: "a", Primary: true},
				{Value: "b", Primary: true},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.user.Resource()
			var scimErr *Error
			require.ErrorAs(t, err, &scimErr)
			assert.Equal(t, ScimTypeInvalidValue, scimErr.ScimType)
		})
	}
}

func TestUserFromResourceRejectsWrongSchema(t *testing.T) {
	tests := []struct {
		name    string
		build   func() *Resource
		wantErr string
	}{
		{
			name: "group resource",
			build: func() *Resource {
				rt := GroupResourceType()
				return NewResource(&rt)
			},
			wantErr: "want",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UserFromResource(tt.build())
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestGroupResourceRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		group *Group
	}{
		{
			name: "full group",
			group: &Group{
				ID:          "e9e30dba",
				ExternalID:  "ext-42",
				Meta:        Meta{ResourceType: "Group", Version: `W/"1"`},
				DisplayName: "Tour Guides",
				Members: []Member{
					{Value: "2819c223", Ref: "https://example.com/v2/Users/2819c223", Type: "User"},
					{Value: "902c246b", Type: "User"},
				},
			},
		},
		{name: "minimal group", group: &Group{DisplayName: "Empty"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tt.group.Resource()
			require.NoError(t, err)
			wire, err := EncodeResource(res, Projection{})
			require.NoError(t, err)
			rt := GroupResourceType()
			decoded, err := DecodeResource(wire, &rt)
			require.NoError(t, err)
			back, err := GroupFromResource(decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.group, back)
		})
	}
}

func TestGroupFromResourceRejectsWrongSchema(t *testing.T) {
	rt := UserResourceType()
	_, err := GroupFromResource(NewResource(&rt))
	require.ErrorContains(t, err, "want")
}

// TestTypedViewsShareResourceType pins that conversions reuse one built-in
// ResourceType per kind and that it matches the public constructor.
func TestTypedViewsShareResourceType(t *testing.T) {
	tests := []struct {
		name    string
		convert func() (*Resource, error)
		want    ResourceType
	}{
		{
			name:    "user",
			convert: func() (*Resource, error) { return (&User{UserName: "b"}).Resource() },
			want:    UserResourceType(),
		},
		{
			name:    "group",
			convert: func() (*Resource, error) { return (&Group{DisplayName: "g"}).Resource() },
			want:    GroupResourceType(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, err := tt.convert()
			require.NoError(t, err)
			second, err := tt.convert()
			require.NoError(t, err)
			assert.Same(t, first.ResourceType(), second.ResourceType())
			assert.Equal(t, tt.want, *first.ResourceType())
		})
	}
}
