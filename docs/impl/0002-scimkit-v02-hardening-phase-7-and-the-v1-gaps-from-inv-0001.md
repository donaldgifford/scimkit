---
id: IMPL-0002
title: "scimkit v0.2 hardening: phase 7 and the v1 gaps from INV-0001"
status: Draft
author: Donald Gifford
created: 2026-10-16
---

<!-- markdownlint-disable-file MD024 MD025 MD041 -->

# IMPL-0002: scimkit v0.2 hardening: phase 7 and the v1 gaps from INV-0001

<!--toc:start-->
<!-- markdownlint-capture -->
<!-- markdownlint-disable MD013 -->
- [Objective](#objective)
- [Scope](#scope)
  - [In Scope](#in-scope)
  - [Out of Scope](#out-of-scope)
- [Implementation Phases](#implementation-phases)
  - [Phase 1: CI assurance](#phase-1-ci-assurance)
    - [Tasks](#tasks)
    - [Success Criteria](#success-criteria)
  - [Phase 2: Library seams](#phase-2-library-seams)
    - [Tasks](#tasks-1)
    - [Success Criteria](#success-criteria-1)
  - [Phase 3: Custom schemas, examples, docs site](#phase-3-custom-schemas-examples-docs-site)
    - [Tasks](#tasks-2)
    - [Success Criteria](#success-criteria-2)
- [File Changes](#file-changes)
- [Testing Plan](#testing-plan)
- [Dependencies](#dependencies)
- [References](#references)
<!-- markdownlint-restore -->
<!--toc:end-->

<!--docz:objective:start-->
## Objective

Carry DESIGN-0001's phase 7 ("Hardening", deferred past v0.1.0) plus the
design gaps INV-0001 surfaced that were *not* folded into the v0.1.1
cleanup PR. The cleanup PR closed the toolchain drift, the support-file
leftovers, the boundary-hardening findings (body/page limits, idempotent
retry, ETag clone contract, sort capability, simulator timeouts,
`WithResourceType` semantics, Go 1.26 floor) and the performance pass; this
document tracks everything that remains between v0.1.x and a credible v1.

**Implements:** DESIGN-0001 (phase 7); INV-0001 (Observation 5 gaps 2–7)

<!--docz:objective:end-->

<!--docz:scope:start-->
## Scope

<!--docz:in-scope:start-->
### In Scope

- CI assurance jobs that make existing guarantees continuous: fuzz cron,
  external compliance (scim2-tester), API-diff gate, RFC JSON fidelity.
- Library seams a production consumer needs and cannot add from outside:
  observability hooks, server-side backpressure, an OAuth/JWT bearer
  helper, a decided multi-tenancy posture.
- Custom-schema story: `scimkit schema gen`, seedable/registrable custom
  types end to end, `examples/`, and a published docs site.

<!--docz:in-scope:end-->

<!--docz:out-of-scope:start-->
### Out of Scope

- `/Bulk` and `/Me` (DESIGN-0001 Q5 stands until an IdP sends them).
- Persistence adapters, SSO protocols, a mock UI, the mock spin-off
  (reassessed after v0.2 usage, not before).
- Breaking the typed-view API (`MultiValue.Primary` tri-state, `Profile`
  vars → functions). Collected for v1.0 under a future DESIGN.

<!--docz:out-of-scope:end-->
<!--docz:scope:end-->

## Implementation Phases

Each phase builds on the previous one. A phase is complete when all its tasks
are checked off and its success criteria are met.

---

<!--docz:phase:start-->
### Phase 1: CI assurance

Make the guarantees the repo already has continuous and externally
checked, so later API work cannot regress silently.

<!--docz:tasks:start-->
#### Tasks

- [ ] Scheduled fuzz job (weekly cron, `-fuzztime 10m` per target over
      `scim` codec, `filter.Parse`/`ParsePath`, `patch.Decode`); new
      crashers open an issue and the minimized input is committed under
      `testdata/fuzz/`
- [ ] `scim2-tester` container job against `scimkit mock` (non-blocking,
      `continue-on-error`); findings triaged into issues with the RFC
      section cited; a `just compliance` recipe runs it locally
- [ ] API-diff gate: `gorelease` (or `apidiff`) against the previous tag in
      CI; a detected incompatible change fails unless the PR carries a
      `breaking` label *and* the CHANGELOG has a matching note
- [ ] RFC 7643 §8.7 fidelity: checksum-pin the vendored `scim/schemas/*.json`
      in a test so an edit is loud (IMPL-0001 Q3 follow-up)
- [ ] Coverage ratchet: `coverage-gate.sh` reads per-package floors from a
      committed table so packages above 90% cannot drift back to 80%

<!--docz:tasks:end-->

<!--docz:criteria:start-->
#### Success Criteria

- Fuzz cron and compliance jobs green or triaged for two consecutive weeks
- An intentionally breaking test PR is blocked by the API-diff gate
- Editing one byte of a vendored schema fails `just test`

<!--docz:criteria:end-->
<!--docz:phase:end-->

---

<!--docz:phase:start-->
### Phase 2: Library seams

The consumer-facing hooks INV-0001 found missing. All additive.

<!--docz:tasks:start-->
#### Tasks

- [ ] Observability seam: `server.Config.Metrics` / `client.WithMetrics`
      taking a small interface (counters for requests by endpoint+status,
      PATCH no-ops, 412s, filter fallbacks, retries) with a no-op default;
      request-id propagation (`X-Request-Id` honored or generated, echoed
      on responses, present in every `slog` line); document how to wrap
      with OpenTelemetry without the library importing it
- [ ] Backpressure: `server.Config.MaxInFlight` per resource type (semaphore
      ahead of the striped locks) answering `503` + `Retry-After` when
      saturated, advertised nowhere (SPC has no field) but documented for
      the Entra retry-storm case
- [ ] OAuth bearer helper: `server.JWTBearer(keys KeyFunc, opts...)` that
      validates `iss`/`aud`/`exp`/alg allow-list with stdlib crypto only,
      plus `server.IntrospectingBearer(url, creds)` for RFC 7662; both
      implement `server.Auth` and fill `authenticationSchemes` honestly
- [ ] Multi-tenancy posture: a short DESIGN section (or ADR) deciding
      "one handler per tenant, mounted by prefix" versus a `TenantResolver`
      in `Config`; implement only if the decision is the resolver
- [ ] Client ergonomics: `client.Users().CreateUser(ctx, scim.User)` /
      `GetUser` typed wrappers over the resource services so callers stop
      juggling `UserFromResource` (INV-0001 Observation 5, "typed sugar")
- [ ] `server.Config` defaults: `MaxFilterResults` gets a documented
      default (200) instead of being required

<!--docz:tasks:end-->

<!--docz:criteria:start-->
#### Success Criteria

- A consumer can count PATCH no-ops and 412s without wrapping the handler
- The concurrent same-group PATCH storm test passes with `MaxInFlight: 2`
  and observes 503s rather than unbounded queueing
- JWT bearer helper passes a table of RFC 7519 negative cases (expired,
  wrong aud, `alg: none`, wrong key)

<!--docz:criteria:end-->
<!--docz:phase:end-->

---

<!--docz:phase:start-->
### Phase 3: Custom schemas, examples, docs site

The remaining DESIGN-0001 phase 7 deliverables.

<!--docz:tasks:start-->
#### Tasks

- [ ] `scimkit schema gen`: schema JSON (file or live `/Schemas` URL) →
      typed struct + `Resource` conversions; golden test proves it
      reproduces the hand-written `User`/`Group`/`EnterpriseUser` views
      byte-for-byte (DESIGN-0001 Q2 part 3)
- [ ] Custom types end to end: `scimtest` seeding by type name (landed in
      the cleanup PR) exercised in an `Example`, a corpus fixture for a
      custom `Device` type, and a README section
- [ ] `examples/`: minimal custom server with its own `Store` (SQLite via
      stdlib-only driver is out — use a map-backed store that shows the
      contract) and a minimal provisioner; built and run in CI
- [ ] Docs site: `docz wiki init mkdocs` publishing `docs/` plus
      `docz:go-docz` API pages; `mkdocs.yml` is already referenced by
      `.docz.yaml`
- [ ] README: add the custom-type and observability quickstarts backed by
      `Example` tests

<!--docz:tasks:end-->

<!--docz:criteria:start-->
#### Success Criteria

- `schema gen` golden test green; a generated `Device` view round-trips
  through `Resource` and the codec
- `examples/` compile and run in CI
- Docs site builds in CI and the wiki nav lists every docz document

<!--docz:criteria:end-->
<!--docz:phase:end-->

---

<!--docz:file-changes:start-->
## File Changes

| File | Action | Description |
| ---- | ------ | ----------- |
| `.github/workflows/fuzz.yml` | Create | Weekly fuzz cron over all targets |
| `.github/workflows/compliance.yml` | Create | scim2-tester container job (non-blocking) |
| `.github/workflows/ci.yml` | Modify | API-diff gate step; examples build |
| `scim/schemas/schemas_test.go` | Modify | Checksum pinning of vendored RFC JSON |
| `scripts/coverage-gate.sh` + `coverage-floors.txt` | Modify / Create | Per-package ratchet |
| `server/{metrics,backpressure,jwt}.go` | Create | Observability, MaxInFlight, bearer helpers |
| `client/{metrics,typed}.go` | Create | Client metrics hook, typed User/Group wrappers |
| `cmd/scimkit/schema.go` + `internal/schemagen/views/` | Create | `schema gen` subcommand and generator |
| `examples/{server,provisioner}/` | Create | Runnable examples |
| `mkdocs.yml`, `docs/wiki/` | Create | Docs site |

<!--docz:file-changes:end-->

<!--docz:testing:start-->
## Testing Plan

- [ ] Table-driven unit tests for every new exported symbol (house style)
- [ ] Concurrency tests for `MaxInFlight` under `-race`
- [ ] JWT negative-case table; introspection against an `httptest` server
- [ ] Golden tests for `schema gen` against the hand-written views
- [ ] `examples/` executed in CI; docs site built in CI

<!--docz:testing:end-->

<!--docz:dependencies:start-->
## Dependencies

- Phase 1 has no code dependencies and can start immediately after the
  v0.1.1 cleanup PR merges.
- Phase 2's client typed wrappers depend on nothing; the JWT helper must
  stay stdlib-only (DESIGN-0001 Q8), which rules out JOSE libraries.
- Phase 3's `schema gen` is the prerequisite for the custom-type example
  and README section.

<!--docz:dependencies:end-->

<!--docz:references:start-->
## References

- [DESIGN-0001 — scimkit library architecture](../design/0001-scimkit-library-architecture.md)
- [IMPL-0001 — scimkit v0 implementation](0001-scimkit-v0-implementation.md)
- [INV-0001 — v0.1.0 post-release review](../investigation/0001-scimkit-v010-post-release-review-code-setup-design-and.md)
- [scim2-tester](https://github.com/python-scim/scim2-tester)
- [gorelease](https://pkg.go.dev/golang.org/x/exp/cmd/gorelease)

<!--docz:references:end-->
