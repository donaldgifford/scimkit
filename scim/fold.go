package scim

import "strings"

// foldName canonicalizes an attribute name or schema URN for
// case-insensitive lookup (RFC 7643 §2.1, RFC 7644 §3.10). Attribute names
// and schema URNs are ASCII by grammar, so ASCII lowering suffices.
func foldName(s string) string { return strings.ToLower(s) }

// FoldName canonicalizes an attribute name or schema URN the same way this
// package folds keys when storing values, so packages resolving paths
// against Resource state (filter evaluation, PATCH targeting) match
// ComplexValue keys exactly.
func FoldName(s string) string { return foldName(s) }
