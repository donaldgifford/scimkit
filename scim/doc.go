// Package scim provides the core SCIM 2.0 (RFC 7643) data model for
// building service providers and provisioning clients.
//
// # Resource model
//
// [Resource] is the canonical representation: a schema-aware,
// case-insensitive, presence-tracking attribute container in which JSON
// null, empty arrays, and absent keys are one unassigned state (RFC 7643
// §2.5). Values are stored in SCIM-typed forms — dateTime as [time.Time],
// binary as []byte, reference as [ReferenceValue], integer as int64,
// decimal as float64, complex as [ComplexValue] — with the schema deciding
// interpretation at parse time. Paths use standard attribute notation
// ("userName", "name.givenName", URN-prefixed extension attributes) and the
// schemas attribute is derived from content, so it never drifts.
//
// # Codec
//
// [DecodeResource] parses wire bodies tolerantly: keys fold case, top-level
// URN keys become extension containers, unknown attributes are ignored, and
// the [DropReadOnly] option discards client-supplied readOnly data per
// RFC 7644 §3.3. [EncodeResource] emits schema-declared casing in a
// deterministic layout and applies a [Projection]
// (attributes/excludedAttributes plus the schema's returned rules) as a
// serialization stage. [ListResponse] carries the remaining wire
// invariants: a capitalized Resources key and integer pagination fields.
//
// # Typed views
//
// [User], [Group], and [EnterpriseUser] (attached via User.Enterprise) are
// hand-written views of the core schemas with two-way Resource conversions.
// Consumers with custom schemas work with Resource directly.
//
// # Schema metadata
//
// [Schema], [Attribute], and [ResourceType] model RFC 7643 §7 exactly; the
// built-in accessors ([UserSchema], [GroupSchema], [EnterpriseUserSchema],
// [UserResourceType], [GroupResourceType], and the discovery meta-schemas)
// are generated from the RFC 7643 §8.7 JSON vendored under schemas/ and
// return fresh values on every call.
//
// [Error] is the RFC 7644 §3.12 protocol error used by both halves of the
// library, and [NewID] generates service-provider resource ids.
//
// The package depends only on the standard library and anchors the
// architecture described in docs/design/0001-scimkit-library-architecture.md.
package scim
