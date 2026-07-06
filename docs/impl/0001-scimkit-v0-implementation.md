---
id: IMPL-0001
title: "scimkit v0 implementation"
status: In Progress
author: Donald Gifford
created: 2026-07-03
---
<!-- markdownlint-disable-file MD025 MD041 -->

# IMPL 0001: scimkit v0 implementation

**Status:** In Progress
**Author:** Donald Gifford
**Date:** 2026-07-03

<!--toc:start-->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Conventions for every phase PR](#conventions-for-every-phase-pr)
- [Implementation Phases](#implementation-phases)
  - [Phase 0: Repo prep — ✅ complete](#phase-0-repo-prep---complete)
    - [Tasks (phase 0)](#tasks-phase-0)
    - [Success criteria (phase 0)](#success-criteria-phase-0)
  - [Phase 1: scim core](#phase-1-scim-core)
    - [Tasks (phase 1)](#tasks-phase-1)
    - [Success criteria (phase 1)](#success-criteria-phase-1)
  - [Phase 2: filter](#phase-2-filter)
    - [Tasks (phase 2)](#tasks-phase-2)
    - [Success criteria (phase 2)](#success-criteria-phase-2)
  - [Phase 3: patch](#phase-3-patch)
    - [Tasks (phase 3)](#tasks-phase-3)
    - [Success criteria (phase 3)](#success-criteria-phase-3)
  - [Phase 4: server + server/memstore](#phase-4-server--servermemstore)
    - [Tasks (phase 4)](#tasks-phase-4)
    - [Success criteria (phase 4)](#success-criteria-phase-4)
  - [Phase 5: client](#phase-5-client)
    - [Tasks (phase 5)](#tasks-phase-5)
    - [Success criteria (phase 5)](#success-criteria-phase-5)
  - [Phase 6: scimtest + cmd/scimkit → v0.1.0](#phase-6-scimtest--cmdscimkit--v010)
    - [Tasks (phase 6)](#tasks-phase-6)
    - [Success criteria (phase 6)](#success-criteria-phase-6)
  - [Phase 7: Hardening (post-v0.1.0)](#phase-7-hardening-post-v010)
    - [Tasks (phase 7)](#tasks-phase-7)
    - [Success criteria (phase 7)](#success-criteria-phase-7)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [Open Questions](#open-questions)
  - [Q1 — Release cadence across phases?](#q1--release-cadence-across-phases)
  - [Q2 — Test-only dependency set?](#q2--test-only-dependency-set)
  - [Q3 — Sourcing the RFC 7643 §8.7 JSON?](#q3--sourcing-the-rfc-7643-87-json)
  - [Q4 — Generated code: committed or build-time?](#q4--generated-code-committed-or-build-time)
  - [Q5 — Simulator scenario representation?](#q5--simulator-scenario-representation)
  - [Q6 — Coverage enforcement?](#q6--coverage-enforcement)
  - [Q7 — PR granularity per phase?](#q7--pr-granularity-per-phase)
- [References](#references)
<!--toc:end-->

## Objective

Implement scimkit v0 — the SCIM 2.0 primitives library (server toolkit,
provisioner client, mock server + IdP traffic simulator) — per the
architecture, API sketches, and decided open questions in
[DESIGN-0001](../design/0001-scimkit-library-architecture.md). This document
expands DESIGN-0001's rollout table into phase-by-phase task checklists with
per-phase success criteria.

**Implements:** DESIGN-0001

## Scope

### In Scope

- Phases 0–6 of the DESIGN-0001 rollout, ending at the **v0.1.0** tag:
  `scim` core, `filter`, `patch`, `server` (+ `server/memstore`), `client`,
  `scimtest`, and the `cmd/scimkit` CLI + Docker image.
- Phase 7 hardening tasks (post-v0.1.0): fuzz corpus, external compliance
  job, `scimkit schema gen`, examples, docs site.
- The test assets both halves share: the golden IdP corpus and the RFC
  conformance test files.

### Out of Scope

- Everything in DESIGN-0001's Non-Goals: SCIM 1.1, persistence adapters,
  auth implementations, SSO, `/Bulk`, `/Me`, mock UI.
- The deferred go-directive floor decoupling (DESIGN-0001 Q6 decision).
- Mock spin-off to its own repo (revisit after v0.1.0 usage).

## Conventions for every phase PR

These apply to each phase below; they are not repeated per phase:

- Branch names follow `feat/`, `chore/`, `docs/` prefixes; each PR carries
  exactly one semver label per the release-cadence policy (Open Questions
  Q1).
- `just check` (lint + race tests), `actionlint`, and
  `git-cliff -o CHANGELOG.md` regeneration run before every push — the
  changelog drift check compares committed vs generated.
- **Tests are table-driven, always** (Open Questions Q2 amendment):
  table structure is independent of the assertion library — testify and
  go-cmp are used inside table cases where they fit; where they don't,
  the table remains and assertions fall back to plain stdlib.
- Every exported symbol ships with a doc comment; new packages replace
  their phase-0 placeholder `doc.go` with real package docs.
- `go.mod` diff is reviewed in every PR: the `require` block may contain
  **test-only** dependencies (Open Questions Q2) and nothing else.
- Any deviation from a DESIGN-0001 API sketch is recorded in the PR
  description and, if it changes a decision, back-ported to DESIGN-0001.

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its
tasks are checked off and its success criteria are met. Phase numbering
matches DESIGN-0001's rollout table.

---

### Phase 0: Repo prep — ✅ complete

Landed in [PR #1](https://github.com/donaldgifford/scimkit/pull/1)
(2026-07-03).

#### Tasks (phase 0)

- [x] Fix scaffold template leftovers (CI references, Makefile/justfile,
      unrendered template variables)
- [x] Package skeletons with doc comments (`scim`, `filter`, `patch`,
      `server`, `client`, `scimtest`)
- [x] Retire `internal/.gitkeep`
- [x] Rewrite `CLAUDE.md`/`README.md` for the library shape
- [x] Add Apache-2.0 `LICENSE`, pin trufflehog action, create repo labels
- [x] DESIGN-0001 authored, decided, committed

#### Success criteria (phase 0)

- [x] PR #1 fully green (lint, tests, security, build, license, changelog,
      CodeQL, secret scan)

---

### Phase 1: `scim` core

The foundation everything else imports: schema metadata generated from the
RFC's own JSON, the presence-tracking `Resource`, the JSON codec, the error
type, and the hand-written typed views.

#### Tasks (phase 1)

- [ ] Vendor RFC 7643 §8.7 schema JSON under `scim/schemas/` — User, Group,
      Enterprise User (§8.7.1) and ServiceProviderConfig, ResourceType,
      Schema meta-schemas (§8.7.2) — one file each, provenance header
      comment with RFC section + extraction method (Open Questions Q3)
- [ ] Schema generator (`internal/schemagen`, invoked via `go:generate`):
      vendored JSON → committed `scim/schema_gen.go` with `Schema` /
      `Attribute` values; CI drift check regenerates and fails on diff
      (Open Questions Q4)
- [ ] Metadata types: `AttrType`, `Mutability`, `Returned`, `Uniqueness`,
      `Attribute`, `Schema`, `ResourceType`, `SchemaExtension`; registry
      applies RFC 7643 §2.2 defaults so hand-authored schemas stay terse
- [ ] Case-folded canonical name registry: path → `Attribute` resolution,
      declared-casing emission (wire output never leaks folded names)
- [ ] URN constants: core/extension schema URNs + `api:messages:2.0:*` URNs
- [ ] `Error` type + constructors for all RFC 7644 Table 9 scimTypes plus
      `ErrNotFound`/`ErrPreconditionFailed`; wire encode (status as JSON
      string) and decode (accept string or integer status)
- [ ] `Resource`: presence-tracking container; `Get`/`Set`/`Unassign` in
      standard attribute notation; `Clone`; automatic `schemas` array
      maintenance; `Meta` accessors; `null`/`[]`/absent equivalence
- [ ] Value system: SCIM-typed scalars (`dateTime`, `binary`, `reference`
      distinct from `string`; `integer` vs `decimal`), multi-valued slices,
      complex max-depth-1 enforcement, at-most-one-`primary` helper
- [ ] JSON codec: unmarshal (case-folded keys, URN extension containers,
      silent drop of client-supplied readOnly attrs, schema type checks);
      marshal (declared casing, projection-context parameter, wire
      invariants: capitalized `Resources`/`Operations`, integer pagination
      fields)
- [ ] ID generation: UUIDv4 via `crypto/rand`; reject reserved `bulkId`
      string (RFC 7643 §3.1)
- [ ] Typed views + conversions: `User` (full §4.1 surface incl. `Name`,
      `MultiValue`, addresses, x509, groups read-only), `Group` (§4.2),
      `EnterpriseUser` + `Manager` (§4.3, attached via `User.Enterprise`);
      `ListResponse`, `Meta`, common message types
- [ ] Tests: presence-semantics table; codec round-trip properties
      (`User` → `Resource` → JSON → `Resource` → `User`; schema → wire →
      schema); fuzz target + seed corpus for the codec; example tests for
      the primary flows

#### Success criteria (phase 1)

- Marshalled built-in schemas are semantically equal (order-insensitive
  JSON comparison) to the vendored RFC 7643 §8.7 documents — golden test
- `go generate ./...` is idempotent; CI drift check green
- Round-trip property tests and presence table pass under `-race`
- `go.mod` contains only test dependencies; `scim` imports stdlib only
- Codec fuzz target runs 60s locally without panic; seed corpus committed

---

### Phase 2: `filter`

The shared URN-aware tokenizer plus parse/evaluate/build for the RFC 7644
filter and path grammars — reused by `patch`, `server`, and `client`.

#### Tasks (phase 2)

- [ ] Tokenizer with URN-aware `attrPath` splitting: longest match against
      registered schema URNs, last-colon fallback for unknown URNs; accept
      `$ref` (cross-RFC `nameChar` inconsistency)
- [ ] AST types (`Compare`, `Present`, `And`, `Or`, `Not`, `ValuePath`) +
      `Path`; `String()` renders a re-parseable filter
- [ ] `Parse`: recursive descent with precedence climbing
      (`not` > `and` > `or`), case-insensitive operators/names, strict JSON
      literal `compValue` (incl. `null`), grouping, valuePath; syntax
      errors → `scim.ErrInvalidFilter` with position detail
- [ ] `ParsePath`: RFC 7644 Figure 7 grammar
      (`attrPath / valuePath [subAttr]`)
- [ ] `Lenient` parse option: bare `not` and other documented off-grammar
      IdP shapes — each lenient acceptance covered by a test naming its
      source
- [ ] `Match` evaluator: schema-resolved `caseExact` string comparison,
      chronological `dateTime`, numeric compare, existential multi-valued
      matching, **per-element** valuePath correlation, `pr` on complex
      attributes, ordering ops on boolean/binary → `ErrInvalidFilter`,
      undefined attributes evaluate as no-value (never error)
- [ ] Builder: `Eq`/`Ne`/`Co`/`Sw`/`Ew`/`Gt`/`Ge`/`Lt`/`Le`/`Pr`/`And`/
      `Or`/`Not`/`HasValue` with correct quoting/escaping
- [ ] Property tests: builder → `String()` → `Parse` → equal AST;
      `Parse` → `String()` → `Parse` stable
- [ ] Conformance tests: RFC §3.4.2.2 Figure 2 examples parse to expected
      ASTs; Table 3 operator-semantics matrix; observed Okta/Entra filters
      (`userName eq`, `externalId eq`, extension-URN paths)
- [ ] Fuzz `Parse` + `ParsePath` with seed corpus (RFC examples + IdP
      shapes + hostile inputs)

#### Success criteria (phase 2)

- Every RFC filter example and every corpus IdP filter parses to the
  asserted AST; Table 3 matrix green under `-race`
- Property tests green (round-trip stability both directions)
- Fuzz targets run 60s locally without panic; corpus committed
- Evaluator correctness on per-element valuePath demonstrated by the
  cross-element false-positive test (the naive-flatten trap)

---

### Phase 3: `patch`

The PATCH engine — decode/normalize per compat Profile, apply per the
§3.5.2 decision tables — plus the golden IdP corpus that doubles as
simulator input later.

#### Tasks (phase 3)

- [ ] `PatchOp` message decoding: schema URN validation, `Operations`
      required, per-op shape checks → `ErrInvalidSyntax`/`ErrInvalidPath`
- [ ] Normalization passes, one per `Profile` knob, each independently
      unit-tested: case-insensitive ops; string→bool/int/decimal coercion
      per target attribute; pathless-replace expansion (dotted keys +
      full-URN keys); remove-with-value-array as member selector;
      `{"value": x}` unwrapping
- [ ] `Profile` definitions: `Strict`, `Default`, `Entra`, `Okta` — each
      knob's doc comment cites the field report that motivates it
- [ ] `Apply` — add table: pathless merge, missing target creates, complex
      merge, multi-valued append, single-valued replace, **idempotent
      no-change add** (`Changed=false`)
- [ ] `Apply` — remove table: no path → `ErrNoTarget`; single/multi
      unassign; valuePath-selected removal; required/readOnly →
      `ErrMutability`
- [ ] `Apply` — replace table: resource-level replace, whole-array replace,
      missing-attr → add fallback, complex **partial merge**, valuePath
      replace (all matching), valuePath+subAttr, non-matching valuePath →
      `ErrNoTarget` (or add under `ReplaceNoTargetAsAdd`)
- [ ] `SynthesizeAddValuePath`: manufacture element from eq-only
      conjunction filters, reject anything else
- [ ] Cross-cutting apply behavior: sequential ops on working copy
      (atomicity via `Clone`), `primary` auto-demotion, implicit `schemas`
      maintenance for URN-qualified paths, mutability enforcement
      (immutable add only when unassigned)
- [ ] Golden IdP corpus: `testdata/idp/{entra,okta,onelogin}/*.json`
      authored from documented shapes, each fixture carrying a `_source`
      URL field; shared loader package for tests and (later) the simulator
- [ ] Conformance test file mirroring §3.5.2's structure; fuzz `Decode`
      with corpus seeds

#### Success criteria (phase 3)

- Full §3.5.2 decision-table matrix green under `Strict`
- Every golden fixture decodes and applies under its IdP Profile; every
  non-compliant fixture is rejected under `Strict` with the RFC-correct
  scimType asserted
- No-op group-member add returns `Changed=false` (the meta.version
  no-bump precondition for phase 4)
- Fuzz `Decode` 60s clean; corpus committed

---

### Phase 4: `server` + `server/memstore`

The service-provider toolkit: routing, pipeline, storage contract with
capability fallbacks, query engine, projection, ETags, discovery.

#### Tasks (phase 4)

- [ ] `Config` validation + `New` returning `http.Handler` on Go 1.22
      `ServeMux` patterns: per-type CRUD/PATCH/list, `POST /{type}/.search`
      (same query engine as GET), discovery endpoints, `501` for `/Bulk`
      and `/Me`, `403` for filtered discovery queries
- [ ] Middleware: media-type handling (`application/scim+json`, accept
      `application/json`), panic-safe error mapping (`scim.Error` → wire
      body + status), request-scoped `slog` fields
- [ ] `BearerToken(verifyFn)` helper + configurable auth exemption for
      `/ServiceProviderConfig` (spec SHOULD)
- [ ] Store wiring: capability sniffing (`Patcher`, `SupportsFilter`) with
      startup `slog` capability report per resource type
- [ ] `server/memstore`: case-folded value indexes, native filter
      evaluation via `filter.Match`, `Patcher` with set-semantics members,
      CAS on version — the reference implementation and mock backend
- [ ] Create/PUT pipeline: schema validation (required, canonicalValues
      per strictness), §3.5.1 PUT semantics (readOnly retained, immutable
      match-or-`ErrMutability`), uniqueness delegation (`ErrUniqueness` →
      409), id/meta assignment
- [ ] PATCH endpoint: native `Patcher` path or Get → `patch.Apply` → CAS
      Replace fallback with bounded retry; always `200` + full body (the
      Okta-safe default); `Changed=false` skips version/lastModified bump
- [ ] Query engine: filter push-down with scan-and-`Match` fallback capped
      by `MaxFilterResults` → `ErrTooMany`; sorting (primary-first value,
      missing-last ascending, type-aware compare, `caseExact`); pagination
      (`startIndex<1`→1, negative count→0, `count=0` returns totals only)
- [ ] Projection stage on **all** responses (GET and write responses):
      `attributes`/`excludedAttributes` mutual exclusion, minimum set
      (`id`, `schemas`, `returned=always`), `returned=never` strip
- [ ] ETags: weak ETag from canonical-JSON hash unless store supplies
      `meta.version`; `If-Match` → 412 on mismatch; `If-None-Match` → 304;
      `meta.version` mirrors the header
- [ ] Concurrency: striped per-resource-id locks around the
      read-modify-write fallback
- [ ] Discovery generation from wiring: SPC capability flags reflect
      actual config (patch/filter+maxResults/sort/etag true; bulk/
      changePassword false), `/ResourceTypes` + `/Schemas` from registered
      types, `authenticationSchemes` from configured auth
- [ ] Integration tests over `httptest`: full user + group lifecycles;
      golden Entra/Okta traffic replayed through HTTP under `Default` and
      rejected appropriately under `Strict`; concurrent same-group PATCH
      storm under `-race`
- [ ] Conformance tests: Table 8 status mapping + Table 9 error bodies;
      discovery documents validate against the vendored meta-schemas

#### Success criteria (phase 4)

- Lifecycle integration green; golden IdP traffic green under `Default`
- Concurrent PATCH storm test: no lost member updates, race detector clean
- Error/status conformance files green; generated discovery docs validate
  against RFC meta-schemas
- A toy 5-method Store (no optional interfaces) passes the full lifecycle
  suite via fallbacks — proving the minimal contract is sufficient

---

### Phase 5: `client`

The generic typed provisioner client.

#### Tasks (phase 5)

- [ ] Transport core: base-URL joining, SCIM headers, context plumbing,
      error-body decode to `scim.Error` (string or int `status`)
- [ ] Retry policy: 429 honoring `Retry-After` (seconds and HTTP-date),
      doubling backoff with jitter and `MaxAttempts` cap (Okta's
      protocol); configurable 5xx retry; request context deadlines
      respected
- [ ] Generic `Resources(name)` service: Create/Get/Replace/Patch/Delete
      returning `*scim.Resource`
- [ ] Typed `Users()` / `Groups()` wrappers over the generic service
- [ ] `List`: `iter.Seq2[T, error]` with transparent pagination,
      duplicate/gap tolerance (non-snapshot semantics), `Query` mapping to
      params; opt-in `POST /.search` transport for PII-bearing filters
- [ ] Provisioner helpers: `Lookup` (0-or-1 from filter), `EnsureMember` /
      `RemoveMember` (spec-shaped valuePath PATCHes, already-present/absent
      = success), `Deactivate` (`active:false`)
- [ ] `Capabilities()`: fetch/parse/cache SPC; PATCH→PUT write fallback
      when the target advertises `patch.supported=false` (or returns 501)
- [ ] ETag support: `If-Match` on writes when etags advertised; 412 as
      typed conflict error
- [ ] Integration tests against `server`+`memstore` over `httptest`; retry
      behavior against a scripted flaky handler (429 sequences, Retry-After
      variants, deadline expiry)

#### Success criteria (phase 5)

- Client ↔ server lifecycle integration green, including membership
  helpers and PATCH→PUT fallback against a patch-disabled server config
- Retry tests assert exact attempt counts, backoff growth, and
  Retry-After honoring; context cancellation aborts cleanly
- Iterator verified across ≥3 pages including a mid-iteration page
  mutation (duplicate/gap tolerance)

---

### Phase 6: `scimtest` + `cmd/scimkit` → v0.1.0

The testing story and the shippable artifact.

#### Tasks (phase 6)

- [ ] `scimtest.NewServer(t, opts...)`: resource-type/seed/profile/token
      options, `httptest` lifecycle with `t.Cleanup`, `URL`/`Client()`/
      `Store()` accessors, request **Recorder** for asserting what a
      provisioner sent
- [ ] Seed format (JSON users/groups) + loader shared by
      `scimtest.WithSeed` and the CLI `--seed` flag
- [ ] Scenario engine: step model with request templates, variable
      capture/substitution (created IDs), per-step expectations; payloads
      sourced from the phase-3 golden corpus (Open Questions Q5)
- [ ] Scenario catalog: `rfc-strict-lifecycle`, `entra-user-lifecycle`,
      `entra-group-membership` (one-member-per-PATCH storm),
      `okta-user-lifecycle` (lookup→create→PUT), `okta-group-put`,
      `onelogin-lookup-dance`
- [ ] `Simulate(ctx, target, token, scenario)` + report model with JSON
      and human-readable renderers
- [ ] CLI (stdlib `flag`, subcommand dispatch): `mock` (`--addr`,
      `--seed`, `--profile`, `--latency`, `--fail-rate`), `exercise`
      (`--target`, `--token`, `--scenario`, `--report`), `version`; slog
      default handler, SIGTERM graceful shutdown
- [ ] Chaos middleware for the mock: injected latency + failure rate (for
      consumer retry testing)
- [ ] CI self-check: `exercise` running `rfc-strict-lifecycle` against
      `scimtest.NewServer` (the simulator validates the mock, the mock
      validates the simulator)
- [ ] Container smoke test in CI: build via `docker buildx bake`, run the
      image, drive an Okta-shaped create/lookup/patch flow against it
- [ ] README: replace placeholder snippets with real quickstarts (server,
      client, `go test` mock, docker mock, exercise) backed by `Example`
      tests
- [ ] Final pre-tag API review: godoc read-through of every package,
      resolve TODOs, verify DESIGN-0001 sketches match reality (update the
      design doc where they deliberately don't)
- [ ] Ship it: final PR labeled `minor` → **v0.1.0** tag, archives, image

#### Success criteria (phase 6)

- `docker run ghcr.io/donaldgifford/scimkit` serves a working SCIM
  endpoint; CI smoke test green
- `scimkit exercise --scenario rfc-strict-lifecycle` passes against the
  mock; at least one intentionally-broken server config demonstrably fails
  it (the simulator detects real problems)
- v0.1.0 released: goreleaser archives + checksums + container image;
  `go get github.com/donaldgifford/scimkit@v0.1.0` works from a clean
  module
- README examples compile as `Example` tests

---

### Phase 7: Hardening (post-v0.1.0)

Not release-blocking; sequenced by value.

#### Tasks (phase 7)

- [ ] Scheduled short-run fuzz CI job (cron) over all fuzz targets;
      committed corpus grows with findings
- [ ] `scim2-tester` (Python) container CI job against `scimkit mock` —
      non-blocking; findings triaged into issues
- [ ] `scimkit schema gen`: schema JSON (file or live `/Schemas` URL) →
      typed structs + `Resource` conversions; golden tests prove it
      reproduces the hand-written built-in views
- [ ] `examples/`: minimal custom server (own Store implementation) and
      minimal provisioner; built in CI
- [ ] Docs site via `docz wiki` (MkDocs/TechDocs) publishing `docs/`
- [ ] Revisit deferred decisions: DESIGN-0001 Q6 go-directive floor;
      coverage-floor ratchet; mock spin-off assessment; automated
      fidelity assurance for the vendored RFC schema JSON (checksum
      pinning or extraction tool — Open Questions Q3 follow-up)

#### Success criteria (phase 7)

- `schema gen` output for the three built-in schemas is byte-identical to
  the hand-written typed views (golden test)
- Fuzz cron and scim2-tester jobs wired and reporting; open findings
  triaged, not silently ignored
- Examples compile and run in CI

## File Changes

Key files by phase (all Create unless noted):

| File | Phase | Description |
|------|-------|-------------|
| `scim/schemas/*.json` | 1 | Vendored RFC 7643 §8.7 schema documents |
| `internal/schemagen/` | 1 | go:generate schema-to-Go generator |
| `scim/schema_gen.go` | 1 | Generated built-in schema values (committed) |
| `scim/{resource,value,codec,errors,meta,user,group,enterprise}.go` | 1 | Core model |
| `filter/{token,ast,parse,path,match,build}.go` | 2 | Filter package |
| `patch/{decode,normalize,profile,apply}.go` | 3 | PATCH engine |
| `testdata/idp/{entra,okta,onelogin}/*.json` | 3 | Golden IdP corpus |
| `server/{server,config,store,pipeline,query,projection,etag,discovery,auth}.go` | 4 | Server toolkit |
| `server/memstore/memstore.go` | 4 | In-memory reference store |
| `client/{client,options,retry,service,iter,helpers,capabilities}.go` | 5 | Provisioner client |
| `scimtest/{server,recorder,seed,scenario,simulate,report}.go` | 6 | Test tooling |
| `cmd/scimkit/main.go` | 6 | Modify: placeholder → CLI dispatch |
| `.github/workflows/ci.yml` | 1, 7 | Modify: generate-drift check; fuzz/compliance jobs |

## Testing Plan

Per DESIGN-0001's Testing Strategy; tracked here as cross-phase work:

- [ ] Table-driven unit tests per package, race detector always on
- [ ] RFC conformance test files (PATCH §3.5.2, filter Table 3, error
      Tables 8/9, projection rules) mirroring spec structure
- [ ] Golden IdP corpus asserted under every Profile (phase 3 onward)
- [ ] Fuzz targets + committed corpora: codec (phase 1), filter
      parse/path (phase 2), patch decode (phase 3)
- [ ] Round-trip property tests: schema↔wire, typed↔Resource↔JSON
- [ ] client↔server httptest integration (phase 5); simulator↔mock
      self-check and container smoke test (phase 6)
- [ ] Coverage enforcement per Open Questions Q6

## Dependencies

- Phases are strictly sequential (1 → 2 → 3 → 4 → 5 → 6); phase 7 tasks
  are independent of each other after v0.1.0.
- Within phases, tasks are parallelizable except where a task names
  another's output (e.g. codec before typed views in phase 1).
- No external services required anywhere; the only network-touching CI
  addition is phase 7's scim2-tester container job.
- Test-only Go dependencies land in phase 1 pending Open Questions Q2.

## Open Questions

All seven questions were decided 2026-07-06: option **a** across the board,
with amendments on Q2 (table-driven always) and Q3 (hand validation first).
The options are preserved below for the record; each question carries a
**Decision** line.

### Q1 — Release cadence across phases?

DESIGN-0001 says both "each phase releasable" and "tag v0.1.0 at phase 6";
the PR labels force a choice per merge:

- **a. Phases 1–5 merge with `dont-release`; the phase 6 PR carries
  `minor` and cuts v0.1.0 (recommended)** — matches DESIGN-0001's stated
  milestone, keeps the first public tag a complete, coherent artifact
  (library + mock), and avoids implying stability for half-built surface.
  Early adopters can still pin pseudo-versions from main.
- **b. Release every phase** (`minor` each: v0.1.0 = scim core alone, mock
  lands around v0.6.0) — real tags for early importers and exercises the
  release pipeline continuously, but scatters "v0.1.0" from the design's
  meaning and publishes tags whose packages half-exist.
- **c. Patch-tag phases 1–5** (v0.0.x pre-releases, `minor` at phase 6) —
  middle ground; v0.0.x tags carry an unusual "nothing works yet" signal
  in Go tooling and still cost a release per phase.

**Decision: a.**

### Q2 — Test-only dependency set?

DESIGN-0001 Q8 allows test-only deps, naming "go-cmp and/or testify":

- **a. Both: `stretchr/testify` for assertion/require flow,
  `google/go-cmp` for deep diffs (recommended)** — the repo's golangci
  config already runs `testifylint` (the fleet convention anticipates
  testify), and go-cmp's diff output on nested `Resource`/AST comparisons
  is materially better than testify's. Two test deps, zero consumer
  impact.
- **b. go-cmp only** — one dependency, excellent diffs; assertion
  boilerplate (`if !cmp.Equal(…) { t.Errorf(cmp.Diff(…)) }`) everywhere
  testify would be one line.
- **c. testify only** — one dependency, fleet-familiar; nested-struct
  failure output is where it's weakest, and this library is nothing but
  nested structs.

**Decision: a, with a standing amendment: tests are table-driven,
always.** Table-driven structure is the house style and is independent of
the assertion library — testify (and go-cmp for deep diffs) are used
*inside* table cases where they fit, and where they don't, the test is
still written as a table with plain stdlib assertions. This is recorded in
[Conventions for every phase PR](#conventions-for-every-phase-pr).

### Q3 — Sourcing the RFC 7643 §8.7 JSON?

- **a. Hand-extract once from the RFC text into `scim/schemas/*.json`,
  with a provenance header and a checksum-pinning test (recommended)** —
  one-time, auditable, no network in any build step; the pinned checksums
  make accidental edits loud. The RFC is immutable, so "stale vendored
  copy" is not a real risk.
- **b. Extraction tool that downloads and slices the RFC text during
  `go:generate`** — provenance by construction, but network-dependent
  generation, brittle text slicing, and it re-derives an immutable
  artifact forever.
- **c. Transcribe schemas directly as Go values, skip JSON embedding** —
  simplest mechanically, but reverses the DESIGN-0001 Q2 decision (JSON as
  source of truth) and loses the byte-comparable golden reference for
  `/Schemas` output.

**Decision: a, staged.** Phase 1 lands hand-extracted, hand-validated
JSON (careful review against the RFC text, provenance headers). The
*automated* assurance layer — checksum pinning, or a verification/
extraction tool — is explicitly deferred; how to guarantee fidelity
long-term is revisited in phase 7 alongside the other deferred decisions.

### Q4 — Generated code: committed or build-time?

- **a. Commit `scim/schema_gen.go`; CI job runs `go generate` and fails on
  diff (recommended)** — consumers `go get` working code with no generator
  tooling; the drift check gives the same guarantee the changelog check
  already does. Standard Go library practice.
- **b. Generate at build time only (output gitignored)** — no generated
  code in review diffs, but consumers and `go get` break: published Go
  modules must contain all compilable source.
- **c. Commit without a drift check** — works until someone edits the
  generated file or the vendored JSON and nothing notices.

**Decision: a.**

### Q5 — Simulator scenario representation?

- **a. Hybrid: Go scenario definitions (sequencing, ID capture,
  assertions) referencing embedded JSON payload files shared with the
  golden corpus (recommended)** — payloads stay data (one corpus, cited
  sources, reusable by tests and simulator per DESIGN-0001), while flow
  logic stays in Go where variable capture and conditional assertions are
  free. No DSL to design in v0.
- **b. Fully data-driven scenario DSL** (steps, captures, and expectations
  all in JSON/YAML) — externally contributable and a natural fit for the
  future mock-with-UI product, but it means designing a
  template/capture/assertion mini-language now, which is real scope; can
  be extracted from (a) later when the spin-off needs it.
- **c. Pure Go scenarios, payloads inline** — fastest to write, but forks
  the payload corpus away from the phase-3 golden fixtures, which
  DESIGN-0001 explicitly wanted unified.

**Decision: a.**

### Q6 — Coverage enforcement?

The scaffold's CI originally called a (nonexistent) `just coverage-gate`;
`.codecov.yml` is currently informational (60% target, 40% threshold):

- **a. Reinstate a real `just coverage-gate` from phase 1: per-package
  floor of 80% for library packages, `cmd/` and generated files exempt
  (recommended)** — coverage discipline is cheapest from the first
  package, and "production grade" is the pitch; per-package floors stop
  one giant package from hiding an untested one. Codecov stays as trend
  UI.
- **b. Codecov project status only** — less CI plumbing, but soft-fail
  culture and repo-wide averaging is exactly how PATCH edge cases go
  untested.
- **c. Defer gating to phase 7** — fastest early velocity; retrofitting a
  floor after five packages exist means one painful catch-up PR.

**Decision: a.**

### Q7 — PR granularity per phase?

- **a. One PR per phase by default, with pre-agreed split points where a
  phase is large — phase 1 (schemas+codec | typed views) and phase 4
  (store+pipeline | query+discovery) are the two that warrant it
  (recommended)** — keeps each merge a coherent, testable unit while
  capping review size where it would hurt.
- **b. Strictly one PR per phase** — simplest bookkeeping; phases 1 and 4
  become 3k+ line reviews, which is where bugs slip through.
- **c. Many small PRs (per task cluster) with a tracking issue per
  phase** — smallest diffs, but long-lived half-built packages on main
  and lots of `dont-release` churn.

**Decision: a.**

## References

- [DESIGN-0001 — scimkit library architecture](../design/0001-scimkit-library-architecture.md)
  (normative for all API shapes referenced here)
- [RFC 7643](https://datatracker.ietf.org/doc/html/rfc7643) /
  [RFC 7644 (inline errata)](https://www.ietf.org/rfc/inline-errata/rfc7644.html)
- [PR #1 — phase 0](https://github.com/donaldgifford/scimkit/pull/1)
- IdP field-report links: see DESIGN-0001 References
