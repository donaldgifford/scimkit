// Package patch decodes SCIM PATCH (RFC 7644 §3.5.2) request bodies into
// normalized operations and applies them to scim.Resource values with full
// spec semantics: sequential atomic application, add/remove/replace
// decision tables, and no-op change detection.
//
// # Decode
//
// [Decode] parses a PatchOp message body into [Op] values: envelope
// validation (schema URN, non-empty Operations), per-op shape checks,
// and filter.ParsePath path parsing. Op values keep json.Number so
// integer literals survive intact until the target attribute's schema
// type is known. Errors are *scim.Error values with the Table 9
// scimType a server can return directly.
//
// # Profiles
//
// A [Profile] bundles the compatibility knobs deciding how far the
// package bends the strict grammar for documented IdP behavior, each
// implemented as an independent normalization pass inside Decode or a
// branch inside Apply. [Strict] is exact RFC 7644; [Default] accepts
// shape deviations with exactly one possible meaning (title-cased ops,
// string-encoded primitives with {"value": x} wrapping, flattened
// pathless-replace keys, remove-with-value member selectors) while
// keeping RFC error semantics; [Entra] adds Microsoft Entra ID's
// semantic deviations (creating valuePath targets that do not exist
// yet); [Okta] pins vendor intent for Okta traffic.
//
// # Apply
//
// [Apply] executes ops sequentially against a clone of the input
// resource — the caller's resource is never mutated, and the first
// failing op aborts the whole PatchOp per §3.5.2 atomicity. The
// decision tables: add merges (multi-valued targets append with
// value-identity deduplication), remove unassigns (the presence model
// makes removing the last element unassign the attribute), replace
// overwrites whole attributes with the spec's missing-attr add
// fallback and complex partial merge. Mutability is enforced here, not
// in Resource.Set: readOnly targets never write, immutable targets
// write only while unassigned. A newly set primary demotes the previous
// one (RFC 7643 §2.4).
//
// [Result].Changed reports whether anything actually changed, so
// servers skip meta.version bumps on idempotent PatchOps — an IdP
// re-adding an existing group member is the canonical case.
package patch
