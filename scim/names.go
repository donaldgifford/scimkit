package scim

// Declared-casing names of the attributes the typed views and codec address
// directly. Lookup folds case, so these exist for emission and readability,
// not for matching.
const (
	nameID          = "id"
	nameExternalID  = "externalId"
	nameDisplayName = "displayName"
	nameName        = "name"
)

// Built-in resource type names (RFC 7643 §4).
const (
	typeNameUser  = "User"
	typeNameGroup = "Group"
)
