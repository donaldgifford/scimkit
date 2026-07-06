package scim

import "strings"

// foldName canonicalizes an attribute name or schema URN for
// case-insensitive lookup (RFC 7643 §2.1, RFC 7644 §3.10). Attribute names
// and schema URNs are ASCII by grammar, so ASCII lowering suffices.
func foldName(s string) string { return strings.ToLower(s) }
