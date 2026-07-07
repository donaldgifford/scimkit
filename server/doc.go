// Package server is a net/http toolkit for building SCIM 2.0 service
// providers (RFC 7644). Consumers implement the minimal five-method
// Store contract per resource type and hand the wiring to New, which
// returns an http.Handler ready to mount under a base URL.
//
// # Wiring
//
// New validates a Config — a base URL, one Registration per resource
// type, a compatibility Profile, an optional Auth layer, and the filter
// result cap — and builds the router on Go 1.22 ServeMux method
// patterns. Each registered type gets CRUD, PATCH, list, and
// POST /.search endpoints; the discovery documents
// (ServiceProviderConfig, ResourceTypes, Schemas) are generated from the
// wiring once and cached, so they never disagree with the running
// capabilities.
//
// # Request pipeline
//
// Every request passes through recover → logging → auth → media-type
// negotiation before routing. Write requests are decoded against the
// resource type (readOnly attributes dropped per RFC 7644 §3.3),
// validated for required attributes and — under the strict profile —
// canonical values, then handed to the store. Responses carry a
// Location on create, an ETag on every resource body, and the SCIM
// media type; errors marshal to the RFC 7644 §3.12 wire form with the
// Table 9 scimType preserved.
//
// # Capability fallbacks
//
// Optional Store capabilities are sniffed once at wiring time, each with
// a documented fallback:
//
//   - Patcher: native O(delta) PATCH. Without it, PATCH runs
//     Get → patch.Apply → conditional Replace under a striped
//     per-resource lock, retrying on a version conflict.
//   - FilterUnsupported: a store that cannot evaluate an expression
//     natively makes the server scan and filter.Match in memory, capped
//     by Config.MaxFilterResults so an unindexed filter fails loudly
//     with tooMany rather than scanning without bound.
//
// ETags come from the store's meta.version when present, else a weak tag
// hashed from the canonical resource JSON. PATCH always answers 200 with
// the full body (never 204, the Okta-safe default), and a no-op PatchOp
// leaves version and lastModified untouched.
package server
