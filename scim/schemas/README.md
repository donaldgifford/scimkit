# Vendored RFC 7643 schema documents

These files are the machine-readable SCIM 2.0 schema representations
published in [RFC 7643 §8.7](https://www.rfc-editor.org/rfc/rfc7643.txt),
vendored as the source of truth for scimkit's built-in schemas
(DESIGN-0001 Q2; IMPL-0001 Q3). They are consumed by `go:embed` and the
`internal/schemagen` generator — **do not edit by hand**.

| File | RFC section | Schema id |
| --- | --- | --- |
| `user.json` | §8.7.1 | `urn:ietf:params:scim:schemas:core:2.0:User` |
| `group.json` | §8.7.1 | `urn:ietf:params:scim:schemas:core:2.0:Group` |
| `enterprise-user.json` | §8.7.1 | `urn:ietf:params:scim:schemas:extension:enterprise:2.0:User` |
| `service-provider-config.json` | §8.7.2 | `urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig` |
| `resource-type.json` | §8.7.2 | `urn:ietf:params:scim:schemas:core:2.0:ResourceType` |
| `schema.json` | §8.7.2 | `urn:ietf:params:scim:schemas:core:2.0:Schema` |

## Provenance and extraction method

Source: `https://www.rfc-editor.org/rfc/rfc7643.txt` (immutable),
extracted 2026-07-06. Method:

1. Strip RFC pagination artifacts: form-feed lines, page footers
   (`Hunt, et al. … [Page N]`), and page headers
   (`RFC 7643 … September 2015`).
2. Take each section's JSON array from its first `[` to the balanced
   closing `]` (bracket counting, string-aware).
3. **Rejoin wrapped strings**: the RFC wraps long `description` prose
   across lines to fit 72 columns, which is invalid JSON. Inside string
   literals only, a newline plus following indentation is replaced with a
   single space. No other content transformation is applied.
4. Split each array into one file per schema object; pretty-print with
   2-space indent, key order preserved as published.

## Validation performed

- All six documents parse as JSON and carry the expected schema `id`s.
- Top-level attribute counts match the RFC (User 21, Group 2,
  Enterprise User 6, ServiceProviderConfig 7, ResourceType 6, Schema 4).
- Every attribute object has `name`/`type`/`multiValued`/`description`;
  all `type`/`mutability`/`returned`/`uniqueness` values are within the
  RFC 7643 §7 enumerations.
- Spot-checked by hand against the RFC text (group.json in full;
  rejoined descriptions in user.json and enterprise-user.json).

## Fidelity notes

- Content is **verbatim RFC 7643 as published** — RFC errata are NOT
  applied. Reconciliation with verified errata, plus automated fidelity
  assurance (checksums or an extraction tool), is deferred to IMPL-0001
  phase 7 (Q3 follow-up).
- §8.7.1 schemas include the RFC's `meta` block; the §8.7.2 documents do
  not carry one in the RFC and none was added.
- The RFC's prose style (double space after periods) is preserved inside
  descriptions.
