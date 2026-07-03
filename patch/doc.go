// Package patch decodes SCIM PATCH (RFC 7644 §3.5.2) request bodies into
// normalized operations and applies them to scim.Resource values with full
// spec semantics: sequential atomic application, add/remove/replace
// decision tables, and no-op change detection.
//
// Normalization is governed by a Profile, which tolerates the documented
// deviations of real identity providers (Entra, Okta) without loosening
// the strict-RFC core.
package patch
