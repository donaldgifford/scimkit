# Changelog

All notable changes to this project are documented here. The format is
based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
this project adheres to [Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-10-16

### Features

- *(server)* Harden the request boundary
- *(server)* StaticBearerToken and ResponseWriter unwrapping
- *(scimtest)* Bound every simulator step and stop following redirects
- *(scimtest)* [**breaking**] Append custom resource types, seed any type, cap the recorder
- *(scim)* Error.Is/IsStatus, Resource.Lookup, exported Coerce, nano dateTime, bare-URN projection
- *(filter)* Reject nested value filters, bound nesting depth, add MatchElement
- *(patch)* Immutable match-or-error, MaxOperations, LenientPaths, per-element matching

### Bug Fixes

- *(client)* Gate retries on method idempotency; add WithTokenSource
- *(client)* Single-flight capability fetch, stalled-pagination guard, visible 401 probe
- *(memstore)* Deterministic list order and weak version compare
- *(cmd)* Server timeouts, flag validation, and a SCIMKIT_TOKEN fallback

### Refactor

- *(server)* Adopt errors.AsType, slices.SortStableFunc, strings.SplitSeq

### Documentation

- INV-0001 post-release review, IMPL-0002 follow-on, status transitions
- *(readme)* Note the Go 1.26 floor and the signed release image
- *(inv)* Record that the Renovate presets already apply semver labels

### Performance

- *(memstore)* List and member-remove benchmarks
- *(scim)* Resource.CloneShared and allocation-free re-coercion of stored scalars

### Miscellaneous Tasks

- *(toolchain)* Go 1.27.2 with a 1.26 floor, golangci-lint 2.14, explicit tool pins
- *(repo)* Remove the Makefile and scrub phase-0 scaffold leftovers
- Pin actions by SHA, test the Go floor, sign the release image

## [0.1.0] - 2026-10-16

### Features

- *(scim)* Vendor RFC 7643 §8.7 schema JSON
- *(scim)* Schema metadata types, wire codec, and URN constants
- *(scim)* Generate built-in schemas from vendored RFC JSON
- *(scim)* Protocol error type with Table 9 constructors
- *(scim)* Typed value system with schema-aware coercion
- *(scim)* Case-folded attribute path resolution
- *(scim)* Presence-tracking Resource container
- *(scim)* JSON codec with projection-aware encoding
- *(scim)* Resource id generation and validation
- *(scim)* Typed User/Group/EnterpriseUser views and ListResponse
- *(scim)* Export FoldName and CommonAttributes hooks
- *(filter)* Grammar tokenizer with exact attrPath splitting
- *(filter)* AST types with precedence-aware String rendering
- *(filter)* Recursive-descent filter parser
- *(filter)* ParsePath for the RFC 7644 Figure 7 grammar
- *(filter)* Lenient parse option for off-grammar IdP shapes
- *(filter)* Schema-aware Match evaluator
- *(filter)* Expression builder for the client direction
- *(patch)* Decode PatchOp messages with per-op shape checks
- *(patch)* Profile-gated normalization passes
- *(patch)* Built-in Strict, Default, Entra, and Okta profiles
- *(patch)* Apply engine implementing the §3.5.2 decision tables
- *(patch)* Synthesize valuePath elements under the Entra knobs
- *(patch)* Golden IdP corpus, §3.5.2 conformance suite, Decode fuzzing
- *(server)* Store contract, sort engine, and memstore reference implementation
- *(server)* HTTP service-provider handler, pipeline, and discovery
- *(patch)* Add EncodeOps for rendering ops to a PatchOp wire body
- *(client)* Generic typed SCIM provisioner client
- *(scimtest)* Embeddable mock server with request recorder
- *(scimtest)* JSON seed format and pipeline-replay loader
- *(scimtest)* Scenario engine with templating and JSON-path assertions
- *(scimtest)* Built-in scenario catalog from the golden corpus
- *(scimtest)* Simulate runner and dual-format report model
- *(scimtest)* Seedable chaos middleware for the mock
- *(cmd)* Scimkit CLI with mock, exercise, and version subcommands

### Bug Fixes

- *(patch)* Enforce immutable sub-attributes inside valuePath elements
- *(scim)* Correct schemaExtensions to multiValued in ResourceType meta-schema

### Refactor

- *(scim)* Apply review fixes from phase 1 style pass
- *(filter)* Apply review fixes from phase 2 style pass

### Documentation

- Add DESIGN-0001 scimkit library architecture
- Add IMPL-0001 v0 implementation plan
- *(design)* Clarify scim stdlib-only wording per architect review
- README quickstarts, DESIGN-0001 reconciliation, IMPL-0001 phase 6 checklist

### Testing

- *(scim)* Codec fuzz targets and example tests
- *(filter)* Round-trip property tests; fix right-nested rendering
- *(filter)* RFC conformance suite and observed IdP filters
- *(filter)* Fuzz targets for Parse and ParsePath
- *(patch)* Pin cross-cutting apply behavior
- *(server)* Integration, conformance, and golden IdP traffic over HTTP
- *(client)* Integration, retry, pagination, and fallback coverage
- README-backed Example tests for server, client, and scimtest

### Miscellaneous Tasks

- Fix scaffold template leftovers
- Phase 0 repo prep for library layout
- Add Apache-2.0 LICENSE and pin trufflehog action
- *(ci)* Per-package coverage gate and scim package docs
- Simulator self-check and container smoke jobs; publish image on release

