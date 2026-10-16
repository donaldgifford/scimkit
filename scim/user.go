package scim

import (
	"fmt"
	"slices"
)

// MultiValue is the standard multi-valued element shape (RFC 7643 §2.4)
// used by emails, phoneNumbers, ims, photos, entitlements, and roles. For
// photos, Value carries the reference URI as a string.
type MultiValue struct {
	Value   string
	Display string
	Type    string
	Primary bool
}

// Name is the User name complex attribute (RFC 7643 §4.1.1).
type Name struct {
	Formatted       string
	FamilyName      string
	GivenName       string
	MiddleName      string
	HonorificPrefix string
	HonorificSuffix string
}

// Address is one element of the User addresses attribute (RFC 7643 §4.1.2).
type Address struct {
	Formatted     string
	StreetAddress string
	Locality      string
	Region        string
	PostalCode    string
	Country       string
	Type          string
}

// GroupRef is one element of the read-only User groups attribute. Ref maps
// the $ref sub-attribute.
type GroupRef struct {
	Value   string
	Ref     string
	Display string
	Type    string
}

// Certificate is one element of x509Certificates; Value is the raw DER
// bytes (base64 on the wire).
type Certificate struct {
	Value   []byte
	Display string
	Type    string
	Primary bool
}

// User is the typed view of the core User resource (RFC 7643 §4.1). Zero
// fields are unassigned; pointer fields distinguish absent from zero where
// that matters (Active false vs unassigned). The Enterprise field attaches
// the urn:ietf:params:scim:schemas:extension:enterprise:2.0:User extension.
type User struct {
	ID                string
	ExternalID        string
	Meta              Meta
	UserName          string
	Name              *Name
	DisplayName       string
	NickName          string
	ProfileURL        string
	Title             string
	UserType          string
	PreferredLanguage string
	Locale            string
	Timezone          string
	Active            *bool
	Password          string
	Emails            []MultiValue
	PhoneNumbers      []MultiValue
	IMs               []MultiValue
	Photos            []MultiValue
	Addresses         []Address
	Groups            []GroupRef
	Entitlements      []MultiValue
	Roles             []MultiValue
	X509Certificates  []Certificate
	Enterprise        *EnterpriseUser
}

// Resource converts the typed view to a canonical Resource backed by the
// built-in User resource type. It fails on values the schema rejects, such
// as two elements of one attribute marked primary.
func (u *User) Resource() (*Resource, error) {
	rt := UserResourceType()
	r := NewResource(&rt)
	r.SetMeta(u.Meta)
	fields := []struct {
		path  string
		value any
	}{
		{"id", stringValue(u.ID)},
		{"externalId", stringValue(u.ExternalID)},
		{"userName", stringValue(u.UserName)},
		{"name", u.Name.complexValue()},
		{"displayName", stringValue(u.DisplayName)},
		{"nickName", stringValue(u.NickName)},
		{"profileUrl", stringValue(u.ProfileURL)},
		{"title", stringValue(u.Title)},
		{"userType", stringValue(u.UserType)},
		{"preferredLanguage", stringValue(u.PreferredLanguage)},
		{"locale", stringValue(u.Locale)},
		{"timezone", stringValue(u.Timezone)},
		{"active", boolPointerValue(u.Active)},
		{"password", stringValue(u.Password)},
		{"emails", multiValueElements(u.Emails)},
		{"phoneNumbers", multiValueElements(u.PhoneNumbers)},
		{"ims", multiValueElements(u.IMs)},
		{"photos", multiValueElements(u.Photos)},
		{"addresses", addressElements(u.Addresses)},
		{"groups", groupRefElements(u.Groups)},
		{"entitlements", multiValueElements(u.Entitlements)},
		{"roles", multiValueElements(u.Roles)},
		{"x509Certificates", certificateElements(u.X509Certificates)},
		{SchemaEnterpriseUser, u.Enterprise.containerValue()},
	}
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		if err := r.Set(f.path, f.value); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// UserFromResource converts a canonical Resource into the typed view. The
// resource's core schema must be the User schema.
func UserFromResource(r *Resource) (*User, error) {
	if foldName(r.ResourceType().Schema.ID) != foldName(SchemaUser) {
		return nil, fmt.Errorf("resource has core schema %q, want %q", r.ResourceType().Schema.ID, SchemaUser)
	}
	u := &User{
		ID:                getString(r, "id"),
		ExternalID:        getString(r, "externalId"),
		Meta:              r.Meta(),
		UserName:          getString(r, "userName"),
		Name:              nameFromResource(r),
		DisplayName:       getString(r, "displayName"),
		NickName:          getString(r, "nickName"),
		ProfileURL:        getString(r, "profileUrl"),
		Title:             getString(r, "title"),
		UserType:          getString(r, "userType"),
		PreferredLanguage: getString(r, "preferredLanguage"),
		Locale:            getString(r, "locale"),
		Timezone:          getString(r, "timezone"),
		Active:            getBoolPointer(r, "active"),
		Password:          getString(r, "password"),
		Emails:            multiValuesAt(r, "emails"),
		PhoneNumbers:      multiValuesAt(r, "phoneNumbers"),
		IMs:               multiValuesAt(r, "ims"),
		Photos:            multiValuesAt(r, "photos"),
		Addresses:         addressesAt(r),
		Groups:            groupRefsAt(r),
		Entitlements:      multiValuesAt(r, "entitlements"),
		Roles:             multiValuesAt(r, "roles"),
		X509Certificates:  certificatesAt(r),
		Enterprise:        enterpriseFromResource(r),
	}
	return u, nil
}

// stringValue maps Go zero-string to unassigned.
func stringValue(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolPointerValue(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

func (n *Name) complexValue() any {
	if n == nil {
		return nil
	}
	cv := make(ComplexValue)
	putString(cv, "formatted", n.Formatted)
	putString(cv, "familyname", n.FamilyName)
	putString(cv, "givenname", n.GivenName)
	putString(cv, "middlename", n.MiddleName)
	putString(cv, "honorificprefix", n.HonorificPrefix)
	putString(cv, "honorificsuffix", n.HonorificSuffix)
	if len(cv) == 0 {
		return nil
	}
	return cv
}

func multiValueElements(vals []MultiValue) any {
	if len(vals) == 0 {
		return nil
	}
	out := make(MultiComplexValue, 0, len(vals))
	for _, v := range vals {
		cv := make(ComplexValue)
		putString(cv, "value", v.Value)
		putString(cv, "display", v.Display)
		putString(cv, "type", v.Type)
		if v.Primary {
			cv["primary"] = true
		}
		out = append(out, cv)
	}
	return out
}

func addressElements(vals []Address) any {
	if len(vals) == 0 {
		return nil
	}
	out := make(MultiComplexValue, 0, len(vals))
	for _, a := range vals {
		cv := make(ComplexValue)
		putString(cv, "formatted", a.Formatted)
		putString(cv, "streetaddress", a.StreetAddress)
		putString(cv, "locality", a.Locality)
		putString(cv, "region", a.Region)
		putString(cv, "postalcode", a.PostalCode)
		putString(cv, "country", a.Country)
		putString(cv, "type", a.Type)
		out = append(out, cv)
	}
	return out
}

func groupRefElements(vals []GroupRef) any {
	if len(vals) == 0 {
		return nil
	}
	out := make(MultiComplexValue, 0, len(vals))
	for _, g := range vals {
		cv := make(ComplexValue)
		putString(cv, "value", g.Value)
		putString(cv, "$ref", g.Ref)
		putString(cv, "display", g.Display)
		putString(cv, "type", g.Type)
		out = append(out, cv)
	}
	return out
}

func certificateElements(vals []Certificate) any {
	if len(vals) == 0 {
		return nil
	}
	out := make(MultiComplexValue, 0, len(vals))
	for _, c := range vals {
		cv := make(ComplexValue)
		if len(c.Value) > 0 {
			cv["value"] = c.Value
		}
		putString(cv, "display", c.Display)
		putString(cv, "type", c.Type)
		if c.Primary {
			cv["primary"] = true
		}
		out = append(out, cv)
	}
	return out
}

// putString stores a non-empty string under an already-folded key.
func putString(cv ComplexValue, foldedKey, s string) {
	if s != "" {
		cv[foldedKey] = s
	}
}

func nameFromResource(r *Resource) *Name {
	v, ok := r.Get("name")
	if !ok {
		return nil
	}
	cv, ok := v.(ComplexValue)
	if !ok {
		return nil
	}
	return &Name{
		Formatted:       stringOf(cv["formatted"]),
		FamilyName:      stringOf(cv["familyname"]),
		GivenName:       stringOf(cv["givenname"]),
		MiddleName:      stringOf(cv["middlename"]),
		HonorificPrefix: stringOf(cv["honorificprefix"]),
		HonorificSuffix: stringOf(cv["honorificsuffix"]),
	}
}

func multiValuesAt(r *Resource, path string) []MultiValue {
	mcv := multiComplexAt(r, path)
	if mcv == nil {
		return nil
	}
	out := make([]MultiValue, len(mcv))
	for i, cv := range mcv {
		out[i] = MultiValue{
			Value:   stringOf(cv["value"]),
			Display: stringOf(cv["display"]),
			Type:    stringOf(cv["type"]),
			Primary: boolOf(cv["primary"]),
		}
	}
	return out
}

func addressesAt(r *Resource) []Address {
	mcv := multiComplexAt(r, "addresses")
	if mcv == nil {
		return nil
	}
	out := make([]Address, len(mcv))
	for i, cv := range mcv {
		out[i] = Address{
			Formatted:     stringOf(cv["formatted"]),
			StreetAddress: stringOf(cv["streetaddress"]),
			Locality:      stringOf(cv["locality"]),
			Region:        stringOf(cv["region"]),
			PostalCode:    stringOf(cv["postalcode"]),
			Country:       stringOf(cv["country"]),
			Type:          stringOf(cv["type"]),
		}
	}
	return out
}

func groupRefsAt(r *Resource) []GroupRef {
	mcv := multiComplexAt(r, "groups")
	if mcv == nil {
		return nil
	}
	out := make([]GroupRef, len(mcv))
	for i, cv := range mcv {
		out[i] = GroupRef{
			Value:   stringOf(cv["value"]),
			Ref:     stringOf(cv["$ref"]),
			Display: stringOf(cv["display"]),
			Type:    stringOf(cv["type"]),
		}
	}
	return out
}

func certificatesAt(r *Resource) []Certificate {
	mcv := multiComplexAt(r, "x509Certificates")
	if mcv == nil {
		return nil
	}
	out := make([]Certificate, len(mcv))
	for i, cv := range mcv {
		var der []byte
		if b, ok := cv["value"].([]byte); ok {
			// Clone so mutating the typed view cannot corrupt the source
			// resource.
			der = slices.Clone(b)
		}
		out[i] = Certificate{
			Value:   der,
			Display: stringOf(cv["display"]),
			Type:    stringOf(cv["type"]),
			Primary: boolOf(cv["primary"]),
		}
	}
	return out
}

func multiComplexAt(r *Resource, path string) MultiComplexValue {
	v, ok := r.Get(path)
	if !ok {
		return nil
	}
	mcv, ok := v.(MultiComplexValue)
	if !ok {
		return nil
	}
	return mcv
}

func getString(r *Resource, path string) string {
	v, ok := r.Get(path)
	if !ok {
		return ""
	}
	return stringOf(v)
}

func getBoolPointer(r *Resource, path string) *bool {
	v, ok := r.Get(path)
	if !ok {
		return nil
	}
	b, ok := v.(bool)
	if !ok {
		return nil
	}
	return &b
}

// stringOf converts the string-shaped stored forms (string and reference)
// to a Go string; anything else is treated as unassigned.
func stringOf(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case ReferenceValue:
		return string(s)
	default:
		return ""
	}
}

func boolOf(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
