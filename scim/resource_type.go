package scim

// UserResourceType returns the built-in User resource type: the RFC 7643
// §4.1 User schema served at /Users with the Enterprise User extension
// attached as optional (mirroring the RFC 7643 §6 example). Each call
// returns a fresh value that is safe to modify.
func UserResourceType() ResourceType {
	return ResourceType{
		Name:        typeNameUser,
		Description: "User Account",
		Endpoint:    "/Users",
		Schema:      UserSchema(),
		Extensions: []SchemaExtension{
			{Schema: EnterpriseUserSchema(), Required: false},
		},
	}
}

// GroupResourceType returns the built-in Group resource type: the RFC 7643
// §4.2 Group schema served at /Groups. Each call returns a fresh value that
// is safe to modify.
func GroupResourceType() ResourceType {
	return ResourceType{
		Name:        typeNameGroup,
		Description: typeNameGroup,
		Endpoint:    "/Groups",
		Schema:      GroupSchema(),
	}
}
