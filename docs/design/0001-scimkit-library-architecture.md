---
id: DESIGN-0001
title: "scimkit library architecture"
status: In Review
author: Donald Gifford
created: 2026-07-02
---
<!-- markdownlint-disable-file MD025 MD041 -->

# DESIGN 0001: scimkit library architecture

**Status:** In Review
**Author:** Donald Gifford
**Date:** 2026-07-02

<!--toc:start-->
- [Overview](#overview)
- [Goals and Non-Goals](#goals-and-non-goals)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
- [Background](#background)
  - [Ecosystem (why this library should exist)](#ecosystem-why-this-library-should-exist)
  - [Protocol constraints that shape the architecture](#protocol-constraints-that-shape-the-architecture)
- [Detailed Design](#detailed-design)
  - [Package layout](#package-layout)
  - [Core resource model (scim)](#core-resource-model-scim)
  - [filter — parse, evaluate, build](#filter--parse-evaluate-build)
  - [patch — decode, normalize, apply](#patch--decode-normalize-apply)
  - [server — service-provider toolkit](#server--service-provider-toolkit)
  - [client — generic provisioner client](#client--generic-provisioner-client)
  - [scimtest + cmd/scimkit — the testing story](#scimtest--cmdscimkit--the-testing-story)
- [API / Interface Changes](#api--interface-changes)
- [Data Model](#data-model)
- [Testing Strategy](#testing-strategy)
- [Migration / Rollout Plan](#migration--rollout-plan)
- [Open Questions](#open-questions)
  - [Q1 — Where does the core package live?](#q1--where-does-the-core-package-live)
  - [Q2 — Core resource model shape?](#q2--core-resource-model-shape)
  - [Q3 — Storage contract shape?](#q3--storage-contract-shape)
  - [Q4 — Default strictness posture?](#q4--default-strictness-posture)
  - [Q5 — v0 protocol feature scope?](#q5--v0-protocol-feature-scope)
  - [Q6 — Go version floor for the library?](#q6--go-version-floor-for-the-library)
  - [Q7 — Mock binary shape?](#q7--mock-binary-shape)
  - [Q8 — Dependency policy details?](#q8--dependency-policy-details)
- [References](#references)
<!--toc:end-->

## Overview

`scimkit` is a Go library of production-grade SCIM 2.0 (RFC 7643/7644)
primitives for building **both** SCIM service providers (servers that receive
provisioning traffic from IdPs like Okta, Entra ID, and OneLogin) and SCIM
clients (custom provisioners that push identity data to downstream apps). The
repo also ships a `scimkit` CLI — a mock SCIM server plus an IdP traffic
simulator — as a distroless Docker image, so integrations can be exercised
locally and in CI without a live IdP.

This document defines the package architecture, the core resource model, the
public API shape, and the rollout plan for v0.

## Goals and Non-Goals

### Goals

- **Primitives, not a framework.** Each layer is independently usable: the
  filter parser without the server, the PATCH engine without a store, the
  typed models without HTTP. Consumers compose what they need.
- **Both sides of the protocol.** A server toolkit (handlers, storage
  contract, discovery generation) and a generic typed client (CRUD, list
  iteration, retry/backoff, capability discovery).
- **Spec-correct core, explicit compat edges.** Strict RFC semantics live in
  the core; the documented deviations of real IdPs (Entra's capitalized ops
  and string booleans, Okta's `userName eq` lookups, value-array member
  removal, pathless dotted-key replaces) are handled by a *named, opt-out*
  normalization layer — never by silently loosening the core.
- **stdlib-only runtime.** No third-party runtime dependencies, so services
  building on scimkit inherit zero transitive baggage.
- **Fill the ecosystem gaps** identified in research: a PATCH *application*
  engine (elimity only parses), filter *evaluation* (scim2/filter-parser only
  parses), a maintained generic client (none exists), typed models with
  extension support, and Go-native test tooling.
- **First-class testing story.** An embeddable in-memory mock server
  (`scimtest`) for `go test`, and a standalone mock + IdP simulator binary
  for local/CI use.
- **Release hygiene.** Semver tags from day one via the existing PR-label
  release flow; documented compatibility policy pre-1.0.

### Non-Goals

- **SCIM 1.1** — 2.0 only, per project decision.
- **Being an identity store.** No SQL/Mongo/LDAP persistence adapters in
  core; the `server.Store` contract plus an in-memory reference
  implementation is the boundary. Adapters can live in separate repos later.
- **Authentication implementations.** Auth is a middleware seam with a
  shipped bearer-token helper; OAuth token issuance, mTLS setup, etc. are the
  consumer's business (RFC 7644 §2 declares auth out of scope).
- **SSO protocols.** No SAML/OIDC/SSO anything.
- **Bulk (`/Bulk`) and `/Me` in v0.** Deferred; `ServiceProviderConfig`
  advertises them honestly as unsupported (see Open Questions Q5).
- **A UI for the mock.** The mock may grow into its own project with a UI
  later; v0 is headless.

## Background

### Ecosystem (why this library should exist)

Research across the Go SCIM ecosystem (July 2026) found:

| Library | Role | State |
|---|---|---|
| `elimity-com/scim` | server | De-facto standard (~241★) but no tagged releases; PATCH is parsed, **never applied**; filters validated, **never evaluated**; `map[string]interface{}` models; no sort/bulk/ETag; no client. |
| `scim2/filter-parser` | filter AST | Healthy, small, correct — but parse-only. No evaluator, no builder. |
| `imulab/go-scim` | server toolkit | The only lib that implements PATCH-apply and filter-eval, but dormant since 2020 and built on a heavy property-tree/Navigator model users found painful. |
| `cybozu-go/scim` | client+server | The only generic typed client attempt; **archived 2023**. |
| `scim2/test-suite` | compliance | "Initial draft", 14 commits. The Go-native testing slot is open. |

The gaps scimkit targets: PATCH application, filter evaluation, a maintained
generic client, typed models + extensions, an IdP-quirk compat layer as a
named feature, embeddable Go test tooling, and semver releases.

### Protocol constraints that shape the architecture

These RFC realities drive the design below (citations in [References](#references)):

1. **Presence is tri-state-equivalent.** `null`, `[]`, and *absent* are the
   same state (RFC 7643 §2.5), but PUT/PATCH semantics require knowing
   whether a client *sent* an attribute. Plain Go structs with zero values
   cannot represent this — the core model needs presence tracking.
2. **Names are case-insensitive everywhere; values follow `caseExact`.**
   JSON keys, filter paths, PATCH paths, `attributes` params, and URN
   prefixes all fold case (RFC 7644 §3.10), while output must emit the
   schema's declared casing (`Resources`, `Operations` are capitalized on
   the wire). This demands a canonical-name registry, not ad-hoc folding.
3. **One URN-aware path tokenizer, shared.** `attrPath = [URI ":"] ATTRNAME`
   is ambiguous — URNs contain colons and dots
   (`…enterprise:2.0:User:manager.displayName`). Filters, PATCH paths,
   `attributes`/`excludedAttributes`, and `sortBy` must share one tokenizer
   that prefers longest-registered-URN match with a last-colon fallback.
4. **PATCH is a decision table, not a verb.** §3.5.2's add/remove/replace
   tables (add-on-single-valued replaces; no-op adds must not bump
   `meta.version`; replace-on-missing becomes add; replace-with-non-matching
   valuePath is `400 noTarget`; complex replace is a *partial merge*) plus
   request atomicity require a real engine applied to a working copy.
5. **valuePath correlation is per-element.** `emails[type eq "work" and
   value co "@x"]` must match within a single array element — naive
   flatten-then-filter evaluation is wrong.
6. **Projection is a serialization stage.** `attributes`/`excludedAttributes`
   apply to GET *and* to POST/PUT/PATCH responses; `returned=always` beats
   exclusion; `returned=never` strips unconditionally while the attribute may
   still be filterable. Projection belongs in the response encoder, not the
   query layer.
7. **Discovery documents are generated, never hand-written.**
   `ServiceProviderConfig`, `/ResourceTypes`, and `/Schemas` must reflect the
   server's actual wiring, and query params on those endpoints are ignored
   (filter → 403).
8. **Group membership must be O(delta).** Entra sends one member per PATCH,
   never reads members back (`excludedAttributes=members`), retries
   indefinitely, and issues concurrent PATCHes. Read-modify-write of the full
   member array is both a scale and a correctness failure; idempotent
   member add/remove primitives are required.
9. **IdP deviations are mandatory table stakes.** Capitalized ops, string
   booleans, pathless replaces with dotted/URN keys, remove-with-value-array
   member selection, `userName eq` pre-create lookups, 204-PATCH breaking
   Okta, integer-typed pagination fields. Every production SCIM server
   handles these or fails in the field.

## Detailed Design

### Package layout

```text
github.com/donaldgifford/scimkit/
├── scim/            # core: schemas, attribute metadata, Resource, typed views, errors, URNs
├── filter/          # RFC 7644 filter + path parsing (AST), evaluation, filter builder
├── patch/           # PATCH op decoding/normalization + application engine + compat policies
├── server/          # net/http service-provider toolkit: routing, Store contract, discovery
├── client/          # generic typed SCIM client for provisioners
├── scimtest/        # embeddable mock server + IdP traffic simulator for go test
├── cmd/scimkit/     # CLI: mock server + simulator subcommands (the Docker image)
├── internal/        # non-exported helpers (case folding tables, json utils)
└── docs/            # docz-managed documentation (this file)
```

Dependency direction is strictly one-way:

```text
scim  ←  filter  ←  patch  ←  server  ←  scimtest  ←  cmd/scimkit
   ↖────  client  ─────────────────────↗
```

`scim` imports only the stdlib. `client` needs `scim` + `filter` (builder)
only. Nothing imports `server` except `scimtest` and `cmd`. This keeps each
primitive independently consumable (Goal 1) and makes an eventual mock
spin-off a clean cut along the `scimtest`/`cmd` seam.

### Core resource model (`scim`)

The pivotal decision: a **hybrid model** — a dynamic, schema-aware canonical
representation at the core, with typed views layered on top (Open Questions
Q2).

**Why not typed structs alone:** presence tracking (constraint 1),
case-insensitive access (constraint 2), URN-keyed extension containers, and a
PATCH engine that must address arbitrary paths (`members[value eq "x"].display`)
all require a dynamic structure. **Why not dynamic alone:** `imulab/go-scim`
proved that making users manipulate a generic tree is an adoption killer (its
own `facade` package exists as an apology). scimkit does both: the engine
operates on `scim.Resource`; humans mostly touch `scim.User` / `scim.Group`.

```go
package scim

// Resource is the canonical representation: a schema-aware, case-insensitive,
// presence-tracking attribute container. All protocol machinery (filter
// evaluation, PATCH application, projection) operates on Resources.
type Resource struct { /* resourceType ref + ordered case-folded attr map */ }

func NewResource(rt *ResourceType) *Resource
func (r *Resource) Get(path string) (any, bool)      // path in standard attr notation
func (r *Resource) Set(path string, v any) error     // schema-validated write
func (r *Resource) Unassign(path string) error       // null / [] / absent equivalence
func (r *Resource) Schemas() []string                // maintained automatically
func (r *Resource) Meta() Meta
func (r *Resource) Clone() *Resource                 // working copies for atomic PATCH
```

Scalar values preserve the *SCIM* type, not just the JSON type: `dateTime`
and `binary` are distinct from `string`; `integer` from `decimal`. Internally
values are stored as `string`, `bool`, `int64`, `float64`, `time.Time`,
`[]byte`, `ReferenceValue`, complex `map`, and multi-valued slices — with the
schema deciding interpretation at parse time.

**Typed views** are hand-written for v0 — three stable types whose
ergonomics (pointer optionality, `Name`/`MultiValue` shapes, extension
attach points, Go naming like `ID`) *are* the product and are easier to nail
by hand than to encode in a generator first:

```go
type User struct {
    ID         string
    ExternalID string
    UserName   string
    Name       *Name
    Active     *bool          // pointer: absent vs false matters
    Emails     []MultiValue
    // ... full RFC 7643 §4.1 surface
    Enterprise *EnterpriseUser // urn:…:extension:enterprise:2.0:User
}

func (u *User) Resource() (*Resource, error)
func UserFromResource(r *Resource) (*User, error)
```

Consumers with custom schemas work with `Resource` directly, implement the
same two-way conversion by hand, or — post-v0.1.0 — generate it: a planned
`scimkit schema gen` subcommand consumes schema JSON (a file, or a live
`/Schemas` endpoint) and emits typed structs plus `Resource` conversions for
custom resource types, using the hand-written built-ins as its golden test
fixtures (see Q2 decision).

**Schema metadata** models RFC 7643 §7 exactly — all seven characteristics
with §2.2 defaults applied by the registry, so hand-authored schemas stay
terse:

```go
type Attribute struct {
    Name            string
    Type            AttrType   // String, Boolean, Decimal, Integer, DateTime, Binary, Reference, Complex
    MultiValued     bool
    Required        bool
    CaseExact       bool
    Mutability      Mutability // ReadOnly, ReadWrite, Immutable, WriteOnly
    Returned        Returned   // Always, Never, Default, Request
    Uniqueness      Uniqueness // None, Server, Global
    CanonicalValues []string
    ReferenceTypes  []string
    SubAttributes   []Attribute // max depth 1 except Schema meta-schema
    Description     string
}
```

Built-in: `UserSchema()`, `GroupSchema()`, `EnterpriseUserSchema()`, the
discovery meta-schemas, and `UserResourceType()` / `GroupResourceType()`.
These are not hand-transcribed: RFC 7643 §8.7 ships full machine-readable
JSON representations of every core schema, which are embedded (`go:embed`)
as the source of truth; a `go:generate` step emits the Go values from them,
and a round-trip test pins `/Schemas` output to the RFC text. The internal
representation round-trips to the `/Schemas` wire format so discovery is
generated, never duplicated (constraint 7).

**JSON codec** (in `scim`, not `encoding/json` tags): unmarshalling folds
case, treats top-level URN keys as extension containers, silently drops
client-supplied `readOnly` attributes (`meta`, `id` per §3.1) rather than
erroring, and coerces types per the active compat profile. Marshalling emits
declared casing and applies a projection context (attributes/excluded +
`returned` rules) supplied by the caller.

**Errors** are one first-class type used by both halves of the library:

```go
type Error struct {
    Status   int    // HTTP status
    ScimType string // "" or one of the RFC 7644 Table 9 values
    Detail   string
}

func (e *Error) Error() string
// Constructors for every scimType: ErrInvalidFilter, ErrTooMany, ErrUniqueness,
// ErrMutability, ErrInvalidSyntax, ErrInvalidPath, ErrNoTarget, ErrInvalidValue,
// ErrInvalidVers, ErrSensitive — plus ErrNotFound, ErrPreconditionFailed (412).
```

The server serializes it to the `urn:…:api:messages:2.0:Error` body (with
`status` as a JSON string, per spec examples); the client parses error bodies
back into it (accepting string *or* integer `status`), so
`errors.As(err, &scimErr)` works identically on both sides.

### `filter` — parse, evaluate, build

```go
func Parse(s string) (Expr, error)         // full RFC 7644 §3.4.2.2 grammar
func ParsePath(s string) (Path, error)     // PATCH path grammar (Figure 7)

type Expr interface{ isExpr() }
// Implementations: Compare{Path, Op, Value}, Present{Path},
// And{L, R}, Or{L, R}, Not{E}, ValuePath{Path, Filter}

type Path struct {
    URN       string // "" for core-schema paths
    Attr      string
    SubAttr   string
    ValueFilter Expr // non-nil for members[value eq "x"] style paths
}
```

- Hand-written recursive-descent parser with precedence climbing
  (`not` > `and` > `or`); the RFC's ABNF alone is ambiguous, precedence comes
  from §3.4.2.2 prose.
- The URN-aware tokenizer (constraint 3) lives here and is reused by `patch`,
  `server` (attributes/sortBy params), and `client`. Longest match against
  registered schema URNs, last-colon fallback for unknown URNs.
- Operators and attribute names fold case; `compValue` is a strict JSON
  literal; a `Lenient` parse option accepts the bare-`not` and other
  off-grammar shapes some IdPs emit.
- **Evaluation**: `Match(res *scim.Resource, e Expr) (bool, error)` resolves
  each path against the schema for `caseExact` semantics, applies existential
  multi-valued matching, correlates valuePath predicates per-element
  (constraint 5), and returns `ErrInvalidFilter` for ordering operators on
  boolean/binary attributes. This is the reusable core of both the server's
  in-memory fallback and the mock.
- **Builder** for the client direction, with correct quoting/escaping:

  ```go
  filter.Eq("userName", "bjensen")                  // userName eq "bjensen"
  filter.And(filter.Eq("emails.type", "work"), …)
  f.String()                                        // safe to place in a query
  ```

### `patch` — decode, normalize, apply

Two stages, deliberately separated:

```go
// Decode parses a PatchOp message body into normalized operations.
// Normalization is where IdP-quirk tolerance lives (per the Profile):
//   - case-insensitive "op" values ("Replace" → replace)
//   - string booleans/numbers coerced per target attribute type ("False" → false)
//   - pathless replace with flattened dotted keys and URN keys expanded
//   - remove with op-level value array treated as a member selector
//   - {"value": x} object unwrapping
func Decode(body []byte, rt *scim.ResourceType, p Profile) ([]Op, error)

type Op struct {
    Kind  Kind        // Add, Remove, Replace
    Path  *filter.Path // nil for pathless add/replace; required for remove
    Value any
}

// Apply executes ops sequentially against a working copy (atomicity per
// §3.5.2) and reports whether anything changed (no-op adds MUST NOT bump
// meta.version / lastModified).
func Apply(res *scim.Resource, ops []Op, p Profile) (Result, error)

type Result struct {
    Resource *scim.Resource
    Changed  bool
}
```

`Apply` implements the full §3.5.2 decision tables: add-merges, add-on-
single-valued-replaces, idempotent adds, remove-unassigns, replace-on-missing-
becomes-add, complex-replace-as-partial-merge, valuePath targeting, `primary`
auto-demotion (§2.4), mutability enforcement (immutable add allowed only when
unassigned), and implicit `schemas` maintenance for URN-qualified paths.

**`Profile`** bundles the compat knobs and is shared with `server` and
`scimtest`:

```go
type Profile struct {
    CaseInsensitiveOps      bool
    CoerceStringPrimitives  bool
    ExpandPathlessReplace   bool
    RemoveValueAsSelector   bool
    ReplaceNoTargetAsAdd    bool // strict RFC: 400 noTarget
    SynthesizeAddValuePath  bool // eq-only filter → manufacture element
}

var (
    Strict  Profile // everything false: exact RFC 7644 semantics
    Default Profile // tolerant input, strict output ("Postel mode")
    Entra   Profile // Default + Entra-documented deviations
    Okta    Profile
)
```

### `server` — service-provider toolkit

`server.New` returns an `http.Handler` (built on the Go 1.22+ `ServeMux`
patterns) that the consumer mounts wherever they like:

```go
srv, err := server.New(server.Config{
    BaseURL: "https://app.example.com/scim/v2", // for meta.location generation
    ResourceTypes: []server.Registration{
        {Type: scim.UserResourceType(), Store: userStore},
        {Type: scim.GroupResourceType(), Store: groupStore},
    },
    Profile:        patch.Default,
    Auth:           server.BearerToken(verifyFn), // middleware seam; nil = bring your own
    MaxFilterResults: 200,                        // → SPC filter.maxResults, tooMany
})
mux.Handle("/scim/v2/", http.StripPrefix("/scim/v2", srv))
```

**Endpoints (v0):** per-type CRUD + PATCH, `GET /{type}` with
filter/sort/pagination/projection, `POST /{type}/.search` (same execution
path as GET — one query engine, two transports), `/ServiceProviderConfig`,
`/ResourceTypes`, `/Schemas` (generated from wiring; query params ignored,
filter → 403). Unsupported optional features answer `501` and are advertised
as unsupported in SPC automatically.

**The storage contract** is small; capabilities are optional interfaces the
library sniffs with type assertions, each with a documented fallback:

```go
// Store is the minimum a consumer implements. Resources passed in are
// already validated, normalized, and have server-assigned id/meta.
type Store interface {
    Create(ctx context.Context, res *scim.Resource) (*scim.Resource, error)
    Get(ctx context.Context, id string) (*scim.Resource, error)
    List(ctx context.Context, q Query) (Page, error)
    Replace(ctx context.Context, id string, res *scim.Resource) (*scim.Resource, error)
    Delete(ctx context.Context, id string) error
}

type Query struct {
    Filter    filter.Expr // nil = unfiltered
    SortBy    *filter.Path
    Ascending bool
    StartIndex, Count int  // 1-based; Count 0 = count-only query
}

type Page struct {
    Resources    []*scim.Resource
    TotalResults int
}

// Optional capabilities:
type Patcher interface { // O(delta) native patch — REQUIRED for group scale
    Patch(ctx context.Context, id string, ops []patch.Op) (*scim.Resource, error)
}
type FilterUnsupported interface{ SupportsFilter(f filter.Expr) bool }
```

Fallback behavior, explicitly documented:

- **No `Patcher`:** server does `Get` → `patch.Apply` → CAS `Replace`
  (compare-and-swap on version). Correct, but O(resource size) — the docs
  steer group stores toward `Patcher` (constraint 8).
- **Filter push-down:** `Query.Filter` is always passed to `List`. A store
  that cannot evaluate an expression reports it via `SupportsFilter`; the
  server then falls back to scan-and-`filter.Match` in memory. The fallback
  is capped by `MaxFilterResults` (→ `400 tooMany`) so it degrades loudly,
  not silently. The in-memory store supports everything natively.
- **ETags:** weak ETags computed by the library as a hash of the canonical
  JSON form unless the store's resources carry `meta.version`. `If-Match`
  mismatch → `412`; no-op PATCH (`Changed == false`) leaves version and
  `lastModified` untouched.
- **Concurrency:** per-resource-id serialization inside the server (striped
  mutexes) for the read-modify-write fallback path, since Entra sends
  concurrent PATCHes to the same group.

**Request pipeline:** auth middleware → media-type handling
(`application/scim+json`, accepting `application/json`) → decode + normalize
(Profile) → schema validation (required attrs, mutability, canonical values
per strictness) → uniqueness pre-check delegation (store returns
`ErrUniqueness` → `409`) → store call → projection (attributes/excluded +
`returned` rules) → response with `Location`, `ETag`, correct status codes
(PATCH always answers `200` + body, never `204` — the Okta-safe default).

### `client` — generic provisioner client

```go
c, err := client.New("https://target.example.com/scim/v2",
    client.WithBearerToken(token),
    client.WithHTTPClient(hc),          // default: sane timeouts
    client.WithRetry(client.RetryConfig{ // honors 429 + Retry-After,
        MaxAttempts: 10,                 // doubling backoff (Okta's protocol)
    }),
)

// Typed sugar over a generic core:
u, err := c.Users().Create(ctx, user)
u, err = c.Users().Lookup(ctx, filter.Eq("userName", "bjensen")) // 0-or-1 helper
for u, err := range c.Users().List(ctx, client.Query{Filter: f}) { … } // iter.Seq2,
                                                                       // transparent pagination
err = c.Users().Patch(ctx, id, ops)      // with PUT fallback if 501 (configurable)
res, err := c.Resources("Device").Get(ctx, id) // custom resource types → *scim.Resource

caps, err := c.Capabilities(ctx) // parsed ServiceProviderConfig, cached
```

Provisioner-specific niceties baked in, because they're what every custom
provisioner reimplements:

- **Lookup-then-create** helper matching the Okta/OneLogin flow
  (`filter=userName eq "…"`, empty ListResponse ≠ error).
- **Idempotent group membership**: `Groups().EnsureMember(ctx, gid, uid)` /
  `RemoveMember` emit spec-shaped valuePath PATCHes and treat
  already-present/already-absent as success.
- **Soft-delete convention**: `Users().Deactivate(ctx, id)` PATCHes
  `active: false` (the Okta deletion model) as an alternative to `Delete`.
- **ETag support**: `If-Match` on writes when the server advertises etags;
  `412` surfaces as a typed conflict error for retry logic.
- List iteration tolerates the RFC's non-snapshot pagination (duplicates and
  gaps possible across pages) and never assumes result-set stability.

### `scimtest` + `cmd/scimkit` — the testing story

**`scimtest`** (importable, `go test`-first):

```go
ts := scimtest.NewServer(t,
    scimtest.WithResourceTypes(scim.UserResourceType(), scim.GroupResourceType()),
    scimtest.WithSeed(users, groups),
    scimtest.WithProfile(patch.Strict), // strict by default: tests should catch sloppiness
)
defer ts.Close()
// ts.URL, ts.Client() — point your provisioner at it, assert on ts.Store() after
```

Backed by the real `server` package + the in-memory store — the mock *is*
the library, so mock behavior and library behavior can't drift.

**IdP traffic simulator** — the piece nothing in any ecosystem provides: a
scenario runner that replays golden request shapes (including the
non-compliant ones) against a target SCIM server and produces a report:

```go
report := scimtest.Simulate(ctx, targetURL, token, scimtest.ScenarioEntraUserLifecycle)
// scenarios: Entra (capitalized ops, string bools, pathless replace, one-member-
// per-PATCH storms), Okta (userName-eq lookup→create, PUT full replace,
// deactivate-not-delete), OneLogin (lookup dance, duplicate group creates), RFC-strict.
```

**`cmd/scimkit`** wraps both as subcommands (single CLI keeps the existing
build/goreleaser/docker plumbing intact — Open Questions Q7):

```text
scimkit mock      --addr :8080 --seed seed.json --profile strict|default|entra|okta
                  --latency 50ms --fail-rate 0.01        # chaos knobs for retry testing
scimkit exercise  --target URL --token T --scenario entra-user-lifecycle --report json
scimkit version
```

The distroless image runs `scimkit mock` by default; state is in-memory
(fits the read-only rootfs), seedable via a mounted JSON file. The future
"bigger piece" (UI, persistence, multi-tenant virtual servers) grows behind
these subcommands until it justifies a spin-off along the `scimtest`/`cmd`
seam.

## API / Interface Changes

All new surface — summarized here for review; sketches above are normative:

| Package | Key exports |
|---|---|
| `scim` | `Resource`, `User`/`Group`/`EnterpriseUser` + conversions, `Schema`/`Attribute`/`ResourceType` + built-ins, `Error` + constructors, URN constants, `Meta`, `MultiValue`, `ListResponse` |
| `filter` | `Parse`, `ParsePath`, `Expr` AST, `Match`, builder (`Eq`, `And`, `Not`, …) |
| `patch` | `Decode`, `Apply`, `Op`, `Result`, `Profile` (`Strict`/`Default`/`Entra`/`Okta`) |
| `server` | `New`, `Config`, `Registration`, `Store`, `Patcher`, `Query`, `Page`, `BearerToken`, `memstore.New` (in-memory reference store) |
| `client` | `New`, options, `Users()`/`Groups()`/`Resources()`, `Query`, `Capabilities`, retry config |
| `scimtest` | `NewServer`, options, `Simulate`, scenario catalog |
| `cmd/scimkit` | `mock`, `exercise`, `version` subcommands (flag-based, stdlib `flag`) |

Pre-1.0 compatibility policy: minor versions may break the API (standard Go
pre-1.0 semantics); breaking PRs must carry the `minor` label and a
`CHANGELOG`-visible note. The existing `cmd/scimkit` placeholder `main.go` is
replaced by the CLI described above; ldflags version wiring is kept.

## Data Model

No storage schema is imposed — `server.Store` is the boundary — but the
contract implies requirements adapters must meet, documented on the
interface:

- **Case-folded indexing** for filterable non-`caseExact` attributes
  (`userName eq "BJENSEN"` must match `bjensen`); `id`/`externalId` are
  `caseExact` and index verbatim.
- **`externalId` round-tripping** per requesting client — it is the
  IdP correlation key (`externalId eq "…"` must be efficient).
- **ID discipline**: server-generated, stable, never reassigned; the library
  provides the generator (UUIDv4 via `crypto/rand`) and rejects the reserved
  string `bulkId` (RFC 7643 §3.1).
- **`meta` ownership**: `created`/`lastModified`/`version`/`location` are
  library-managed; stores persist them opaquely. Client-supplied `meta` is
  dropped at decode time, never an error.
- **Group members**: stores implementing `Patcher` should treat member
  add/remove as set operations (idempotent, O(delta), no version bump on
  no-ops).

Wire-format invariants owned by the codec: `Resources`/`Operations` key
casing, extension attributes nested under full-URN containers, integer JSON
types for `totalResults`/`itemsPerPage`/`startIndex`, error `status` as
string.

## Testing Strategy

- **Table-driven unit tests** per package (repo convention), race detector on
  (`just test`).
- **Golden IdP corpus**: `testdata/idp/{entra,okta,onelogin}/*.json` —
  captured real-world request shapes (capitalized ops, string booleans,
  pathless replaces, member-removal variants) asserted through
  `patch.Decode`/`Apply` under each Profile. This corpus doubles as the
  simulator's scenario source, so tests and simulator can't drift.
- **RFC conformance tables as tests**: the §3.5.2 PATCH decision table, the
  Table 9 error mapping, filter operator semantics (Table 3), and projection
  rules each get an exhaustive test file mirroring the spec's structure.
- **Fuzzing** (native `go test -fuzz`): `filter.Parse`, `filter.ParsePath`,
  `patch.Decode`, and the JSON codec — parsers of hostile input are the
  attack surface.
- **Round-trip properties**: schema → `/Schemas` wire format → schema;
  `User` → `Resource` → JSON → `Resource` → `User`.
- **Integration**: `client` ↔ `server` over `httptest` exercising full
  lifecycles (no build tag needed — in-memory); the simulator's RFC-strict
  scenario runs against `scimtest.NewServer` in CI as a self-check.
- **External validation** (later, non-blocking CI job): run
  `python-scim/scim2-tester` against `scimkit mock` in a container to get an
  independent compliance read.

## Migration / Rollout Plan

Phased PRs, each releasable under the existing PR-label flow (`minor` until
v1). Later phases depend on earlier ones; within a phase, work is
parallelizable.

| Phase | Deliverable | Notes |
|---|---|---|
| 0 | Repo prep | Package dirs, rewrite `CLAUDE.md`/`README` for library shape (removing Forgejo/binary leftovers), retire `internal/.gitkeep`, keep docker plumbing aimed at the CLI |
| 1 | `scim` core | Schemas + registry (Go values generated from embedded RFC 7643 §8.7 JSON), `Resource`, codec, errors, hand-written typed `User`/`Group`/`EnterpriseUser` |
| 2 | `filter` | Parser + evaluator + builder, fuzz targets |
| 3 | `patch` | Decode/normalize/apply, Profiles, golden IdP corpus |
| 4 | `server` | Router, Store + memstore, discovery generation, projection, ETags |
| 5 | `client` | Typed client, retries, iterators, provisioner helpers |
| 6 | `scimtest` + `cmd/scimkit` | Mock server, simulator, CLI, Docker image → **tag v0.1.0** |
| 7 | Hardening | Fuzz corpus growth, scim2-tester CI job, `scimkit schema gen` (typed views for custom schemas), docs site, examples/ |

No migration concerns — greenfield. The only consumer-visible commitment
starting at v0.1.0 is the pre-1.0 compatibility policy above.

## Open Questions

All eight questions were decided 2026-07-02: option **a** across the board,
with Q2 amended to add schema-sourced code generation. The options are
preserved below for the record; each question carries a **Decision** line.

### Q1 — Where does the core package live?

The module is `github.com/donaldgifford/scimkit`. Core types (Resource,
schemas, errors) need a home:

- **a. `scim` subpackage (recommended)** — imports read naturally
  (`scim.User`, `scim.Error`, `filter.Parse`, `server.New`); root holds only
  `doc.go`. Mirrors how consumers speak ("a scim User"), keeps the root
  clean, and `scimkit` never appears as an awkward identifier in code.
- **b. Root package `scimkit`** — flatter (`scimkit.User`), one fewer import
  path, common for smaller libs; but `scimkit.` is a long prefix, and
  subpackages (`filter`, `server`) would import the root, inverting the usual
  root-imports-nothing convention.
- **c. `pkg/` prefix** (`scimkit/pkg/scim`) — familiar from k8s-adjacent
  projects; adds a meaningless path segment for a library whose entire point
  is its public API.

**Decision: a.**

### Q2 — Core resource model shape?

- **a. Hybrid: dynamic schema-aware `Resource` core + hand-written typed
  views (recommended)** — the engine gets the presence-tracking,
  case-insensitive, extension-aware structure the RFC demands; users get
  `scim.User` ergonomics; custom schemas fall back to `Resource`. Cost: two
  representations to keep converging (mitigated by round-trip property
  tests).
- **b. Typed structs only** (pointers for presence, struct tags for schema) —
  most ergonomic for the 90% case, but PATCH paths, dynamic filters,
  case-insensitive addressing, and custom resource types all fight
  reflection; this is how libraries end up with a half-broken PATCH engine.
- **c. Dynamic only** (imulab-style property tree) — maximal spec fidelity,
  proven adoption killer; every consumer rebuilds their own typed layer.

**Decision: a, amended with generation where it pays.** The follow-up
question was whether a canonical source could generate the typed structs.
Answer, in three parts:

1. **Schema metadata: yes, generate it.** RFC 7643 §8.7 contains complete
   machine-readable JSON representations of the User, Group, and Enterprise
   User schemas (§8.7.1) and the service-provider meta-schemas (§8.7.2).
   These get embedded via `go:embed` as the source of truth, with a
   `go:generate` step emitting the built-in `Schema`/`Attribute` Go values
   and a round-trip test pinning `/Schemas` output to the RFC text.
2. **Built-in typed views: hand-written in v0.** No reusable Go generator
   exists (cybozu-go/scim's internal codegen is archived; nothing else in
   the ecosystem), and the ergonomic decisions a generator would have to
   encode — pointer optionality, `Name`/`MultiValue` shapes, the enterprise
   extension attach point, Go naming — are exactly what we want to design
   deliberately for three stable types.
3. **Custom-schema typed views: generate, post-v0.1.0.** A `scimkit schema
   gen` subcommand (phase 7) consumes schema JSON — a file or a live
   `/Schemas` endpoint — and generates typed structs + `Resource`
   conversions, with the hand-written built-ins as its golden fixtures.
   That's where codegen pays: every consumer's custom schemas, not our
   three built-ins.

### Q3 — Storage contract shape?

- **a. Minimal 5-method `Store` + optional capability interfaces with
  documented fallbacks (recommended)** — trivially implementable
  (elimity-refugee friendly), scales up via `Patcher`/filter push-down when
  the consumer cares. Risk: capability sniffing via type assertion is
  implicit; mitigated by startup logging of detected capabilities.
- **b. Fat interface** (Store must implement patch, filter, sort, count) —
  everything explicit, no sniffing; but every toy implementation starts by
  stubbing six methods with "not supported", and most never graduate.
- **c. Generic per-resource-type handlers** (`server.Register[T User](…)`) —
  type-safe end-to-end, but couples the server to the typed views, making
  custom/dynamic resource types second-class — backwards, given Q2a makes
  `Resource` the canonical form.

**Decision: a.**

### Q4 — Default strictness posture?

- **a. Server defaults to `patch.Default` ("Postel mode": tolerant input /
  strict output); mock + scimtest default to `Strict` (recommended)** —
  production servers exist to talk to real IdPs, and the quirks are
  documented IdP behavior, not exotica; a strict default means every Entra
  integration breaks in the field with opaque 400s. Tests defaulting strict
  keeps sloppiness visible during development. Named profiles (`Entra`,
  `Okta`) tighten *which* deviations are accepted.
- **b. Strict everywhere, compat strictly opt-in** — purist and predictable;
  in practice re-creates the elimity experience where every adopter
  independently rediscovers the Microsoft known-issues page in production.
- **c. No default — `Profile` is a required constructor arg** — forces a
  conscious choice, but it's ceremony for the 95% who want `Default`, and
  the "right" answer still has to be documented as… a recommended default.

**Decision: a.**

### Q5 — v0 protocol feature scope?

- **a. v0 = CRUD, PATCH, filter, pagination, sorting, projection,
  `/.search`, ETags, discovery; defer `/Bulk` and `/Me` (recommended)** —
  covers everything Okta/Entra/OneLogin actually use (neither Okta nor Entra
  send Bulk; `/Me` is an end-user-facing flow irrelevant to provisioning).
  Bulk's bulkId cross-reference/circular-resolution machinery is the single
  most complex feature in the spec — poor return on v0 effort. SPC
  advertises both honestly as unsupported.
- **b. Include `/Bulk` in v0** — completeness up front and it exercises the
  engine hard; delays everything else by weeks for a feature the target IdPs
  don't emit.
- **c. Okta/Entra critical path only** (CRUD, PATCH, `eq` filter,
  pagination) — fastest to ship, but sorting/`.search`/ETags are cheap on
  top of the Q3a architecture and their absence blocks the "production
  grade" claim.

**Decision: a.**

### Q6 — Go version floor for the library?

Repo convention says `go.mod` matches `mise.toml` (currently 1.26.4), but a
library's `go` directive is a hard floor for every consumer.

- **a. Decouple: `go 1.25` directive (oldest supported Go release, rolling),
  toolchain pinned separately via `mise.toml`/`toolchain` (recommended)** —
  consumers on N-1 can adopt; we still develop/CI on 1.26.4. Needs a small
  CLAUDE.md convention update and a Renovate rule so the directive isn't
  auto-bumped.
- **b. Keep the convention: `go 1.26.4` everywhere** — zero process
  divergence from the fleet, but anyone not on the newest point release
  can't import scimkit, which is hostile for a library meant for others to
  build on.

**Decision: b, deferring a.** Start simple: `go.mod` matches `mise.toml`
per the fleet convention. Decoupling to an N-1 floor (the `go 1.25`
directive, a CI matrix job on oldstable, and a Renovate rule protecting the
directive) is deferred until external consumers need it — loosening a floor
later is backward-compatible, so nothing is lost by waiting.

### Q7 — Mock binary shape?

- **a. Single `scimkit` CLI with `mock` / `exercise` subcommands
  (recommended)** — keeps the existing cmd/goreleaser/docker-bake plumbing
  and image name untouched, leaves room for future subcommands (`validate`,
  `schema gen`), and a later spin-off is a clean cut at the `scimtest`/`cmd`
  seam regardless.
- **b. Dedicated `cmd/scimmock` binary** — sharper identity for the
  mock-as-product story and a marginally easier eventual extraction, at the
  cost of renaming the image/archives/plumbing now and again at spin-off.
- **c. Both** — `scimkit` for library-adjacent tooling and `scimmock` for
  the mock; two release artifacts to maintain before either has users.

**Decision: a.**

### Q8 — Dependency policy details?

"Stdlib and lean" is decided; the edges need definition:

- **a. Zero runtime deps, test-only deps allowed (`go-cmp` and/or
  `testify`), `golang.org/x/*` treated as forbidden at runtime too
  (recommended)** — consumers inherit nothing (test deps don't propagate to
  builds); we keep ergonomic assertions. The lint config already anticipates
  testify.
- **b. Zero deps including tests** — maximal purity; hand-rolled diff output
  in table tests is a real productivity tax for no consumer-facing benefit.
- **c. Allow `golang.org/x/*` at runtime** — quasi-stdlib and sometimes
  useful (`x/text` case folding), but SCIM's case-insensitivity is
  ASCII-scoped attribute names where `strings.EqualFold` suffices; opening
  the door invites drift.

**Decision: a.**

## References

- [RFC 7643 — SCIM: Core Schema](https://datatracker.ietf.org/doc/html/rfc7643)
- [RFC 7644 — SCIM: Protocol](https://datatracker.ietf.org/doc/html/rfc7644)
  ([inline-errata edition](https://www.ietf.org/rfc/inline-errata/rfc7644.html) —
  track this; EIDs 6893, 7898, 7916, 8096, 8365 are verified)
- [RFC 7642 — SCIM: Definitions, Overview, Concepts](https://datatracker.ietf.org/doc/html/rfc7642)
- [Microsoft Entra — Known SCIM 2.0 compliance issues](https://learn.microsoft.com/en-us/entra/identity/app-provisioning/application-provisioning-config-problem-scim-compatibility)
- [Okta — SCIM 2.0 implementation guide](https://developer.okta.com/docs/api/openapi/okta-scim/guides/scim-20)
  and [SCIM FAQs](https://developer.okta.com/docs/concepts/scim/faqs/)
- [OneLogin — SCIM developer docs](https://developers.onelogin.com/scim)
- Prior art: [elimity-com/scim](https://github.com/elimity-com/scim),
  [scim2/filter-parser](https://github.com/scim2/filter-parser),
  [imulab/go-scim](https://github.com/imulab/go-scim),
  [cybozu-go/scim](https://github.com/cybozu-go/scim) (archived),
  [thomaspoignant/scim-patch](https://github.com/thomaspoignant/scim-patch)
  (TS; its issue tracker documents the PATCH edge cases),
  [python-scim/scim2-tester](https://github.com/python-scim/scim2-tester)
- Field reports informing the compat layer: Entra string-boolean bug
  ([MS Q&A 1328467](https://learn.microsoft.com/en-us/answers/questions/1328467/)),
  pathless flattened replace
  ([MS Q&A 1435791](https://learn.microsoft.com/en-us/answers/questions/1435791/)),
  invalid remove-members PATCH
  ([MS Q&A 5524268](https://learn.microsoft.com/en-us/answers/questions/5524268/)),
  group PATCH throttling
  ([MS Q&A 1164652](https://learn.microsoft.com/en-us/answers/questions/1164652/)),
  Okta 204-PATCH failure
  ([devforum 19565](https://devforum.okta.com/t/scim-provisioning-patch-group/19565)),
  Okta PUT wiping group members
  ([devforum 16724](https://devforum.okta.com/t/pushing-group-updates-to-custom-app-integration-through-put-deletes-all-members-unexpectedly/16724))
