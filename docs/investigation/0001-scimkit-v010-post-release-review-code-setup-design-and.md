---
id: INV-0001
title: "scimkit v0.1.0 post-release review: code, setup, design, and dependency bumps"
status: Concluded
author: Donald Gifford
created: 2026-10-16
---

<!-- markdownlint-disable-file MD025 MD041 -->

# INV-0001: scimkit v0.1.0 post-release review: code, setup, design, and dependency bumps

<!--toc:start-->
<!-- markdownlint-capture -->
<!-- markdownlint-disable MD013 -->
- [Question](#question)
- [Hypothesis](#hypothesis)
- [Context](#context)
- [Approach](#approach)
- [Environment](#environment)
- [Findings](#findings)
  - [Observation 1 — Toolchain and dependency drift](#observation-1--toolchain-and-dependency-drift)
  - [Observation 2 — Repo setup and support files](#observation-2--repo-setup-and-support-files)
  - [Observation 3 — Code review: scim, filter, patch](#observation-3--code-review-scim-filter-patch)
  - [Observation 4 — Code review: server, memstore, client, scimtest, cmd](#observation-4--code-review-server-memstore-client-scimtest-cmd)
  - [Observation 5 — DESIGN-0001 against the shipped code](#observation-5--design-0001-against-the-shipped-code)
  - [Observation 6 — Test gaps and Go 1.27 readiness](#observation-6--test-gaps-and-go-127-readiness)
- [Conclusion](#conclusion)
- [Recommendation](#recommendation)
- [References](#references)
<!-- markdownlint-restore -->
<!--toc:end-->

## Question

Now that v0.1.0 has shipped (PR #2, all IMPL-0001 phases 0–6), what must
change before the library is pointed at a real IdP or copied into a private
repo as a shared library? Specifically: which toolchain and dependency pins
are behind, which support files are stale, which code findings are
release-blocking versus polish, and where does DESIGN-0001 no longer
describe reality?

## Hypothesis

The protocol core is sound (the conformance suites, golden corpus, and
simulator self-check all pass), so findings will cluster in three places:
(1) the toolchain is a full Go minor behind the fleet and carries stdlib
CVEs, (2) scaffold leftovers in support files were never cleaned up after
phase 0, and (3) the design's "O(delta) group membership" and "zero
runtime deps / go directive = toolchain" promises have edges the v0 code
and the Q6 deferral do not honor.

## Context

v0.1.0 was tagged from PR #2 on 2026-10-16. The next body of work is a
cleanup pass (`chore/cleaunup`) before new features, and a parallel
question of what the repo would need to look like as a private library or
as the SCIM subtree of a larger Okta SSO package. This investigation is the
shared input to both.

**Triggered by:** DESIGN-0001 (phase 7 "Hardening" + "Revisit deferred
decisions"), IMPL-0001 phase 7, v0.1.0 release.

## Approach

1. Inventory pins: `mise outdated`, `mise latest <tool>` for every pinned
   tool, `go list -m -u all`, `govulncheck ./...`, Docker Hub tags for
   `golang`, the action refs across every workflow.
2. Read every support file (`.github/**`, `.golangci.yml`, `.codecov.yml`,
   `cliff.toml`, `.goreleaser.yml`, `Dockerfile`, `docker-bake.hcl`,
   `justfile`/`docker.just`/`Makefile`, `mise.toml`, `.docz.yaml`,
   `renovate.json5`, `scripts/`) for scaffold leftovers, drift, and
   security posture.
3. Three independent read-only code reviews (`scim`/`filter`/`patch`;
   `server`/`memstore`/`client`/`scimtest`/`cmd`; setup + design critique),
   each asked for correctness, security at trust boundaries, concurrency,
   API smells before v1, robustness limits, over-engineering, test gaps,
   and Go 1.27 readiness. Reviewers ran throwaway probes and benchmarks
   where a claim needed evidence; the tree was left clean.
4. Spot-verify every high and medium finding against the source before
   recording it here.
5. Compare DESIGN-0001's sketches and decisions (Q1–Q8) with `go doc` and
   the shipped behavior; separate "proved right", "stale", and "gap".

## Environment

| Component | Current | Latest / fleet | Notes |
| --------- | ------- | -------------- | ----- |
| Go (`go.mod`, `mise.toml`, `Dockerfile`) | 1.26.4 | 1.27.2 (libtftest is on 1.27.1) | 15 stdlib CVEs reachable, fixed in 1.26.5–1.26.9 |
| golangci-lint (`mise.toml`, `ci.yml`) | 2.11.4 (built with go1.26.1) | 2.14.0 (libtftest 2.13.2) | pinned in two places |
| testify | 1.11.1 | 1.12.1 | test-only |
| go-cmp | 0.7.0 | 0.7.0 | current |
| markdownlint-cli2 | 0.18.1 | 0.23.3 | |
| yamlfmt / yamllint | 0.20.0 / 1.37.1 | 0.21.0 / 1.38.0 | |
| prettier | 3.7.4 | 3.9.10 | |
| yq | 4.48.1 | 4.54.1 | |
| docz | 1.2.2 (`latest`) | 2.0.0 | major; `mise upgrade` picks it up |
| just / syft / govulncheck / goimports | `latest`, behind | 1.59.0 / 1.54.1 / 1.9.0 / 0.52.0 | non-reproducible pins |
| `golang` image | 1.26.4 | 1.27.2 | tracks `go.mod` |
| `distroless/static-debian12:nonroot` | tag only | — | no digest pin |
| Renovate | `renovate.json5` → `donaldgifford/renovate-config` (+go/docker/mise/ci) | — | Dependabot in security-only mode alongside |

## Findings

### Observation 1 — Toolchain and dependency drift

**Go is a full minor behind and the gap is a security issue, not just
hygiene.** `govulncheck` reports 15 stdlib vulnerabilities reachable from
scimkit's own call graph (`net/http`, `crypto/tls`, `net/url`,
`net/textproto`, `mime/multipart`, `encoding/asn1`), all fixed by Go
1.26.6–1.26.9 and therefore by 1.27.2:

```text
GO-2026-6617 6613 6612 6611 6610 6605 6603  net/http          fixed go1.26.9
GO-2026-6608                                net/textproto     fixed go1.26.9
GO-2026-6607 6090 5856                      crypto/tls        fixed go1.26.9 / .6 / .5
GO-2026-6218                                net/url           fixed go1.26.6
GO-2026-6089 5026                           net/http          fixed go1.26.6
GO-2026-5972                                encoding/asn1     fixed go1.26.6
```

The CI `security` job runs govulncheck and should have been failing; it
has not been re-run since the release. Bumping Go touches four files that
must move together: `go.mod` (`go` directive), `mise.toml`, `Dockerfile`
`FROM golang:`, and the golangci-lint pin (2.11.4 was built with go1.26.1
and will not analyze 1.27 code). The `go.mod` directive bump is also a
consumer-visible floor change, which is the Q6 question in Observation 5.

**Pinned tool drift** is routine: every pinned formatter/linter is one or
more releases behind (table above). The `latest` pins (just, git-cliff,
actionlint, syft, govulncheck, docz, goimports, godoc, golines, checkmake,
jq, makefmt) make the toolchain non-reproducible across machines and CI;
docz has a 2.0.0 major waiting behind `latest`.

**Go module deps**: only testify has an update (1.11.1 → 1.12.1). The
`require` block still contains test-only dependencies, satisfying
DESIGN-0001 Q8.

### Observation 2 — Repo setup and support files

Scaffold leftovers from phase 0 were never fully cleaned:

| File | Finding | Sev |
| ---- | ------- | --- |
| `Makefile` + `justfile` | Two hand-maintained copies of the same targets, already drifted: Makefile lacks `self-check`/`smoke`, header typo "primatives", `run-local` says "Run exporter". libtftest removed its Makefile (IMPL-0005) | med |
| `.golangci.yml` | `errcheck.exclude-functions` lists `fatih/color`, `spf13/cobra`; gosec exclusion scoped to `cmd/(compare\|diff)\.go$`; `./main.go` and `mock_.*\.go$` rules; `lll`/`dupl` referenced in exclusions but not enabled | low |
| `.golangci.yml` | `gocritic` `experimental` tag and `testifylint enable-all` grow on every lint upgrade; the 2.11 → 2.14 bump is the likeliest source of new failures | med |
| `.codecov.yml` | ignores `main.go` (none at root), 60%/40% targets disagree with the 80% `coverage-gate.sh` floor; Codecov is trend UI only | low |
| `.github/workflows/*` | Three pinning styles: SHA only in `changelog-regen.yml`; exact tags for codeql `v4.35.5`, trivy `v0.36.0`, trufflehog `v3.95.8`, goreleaser `v7.1.0` (ci) vs floating `v7` (release); majors elsewhere | med |
| `changelog.yml` / `changelog-regen.yml` | Regen auto-commits on main *after* merge, so any un-rebased PR sees drift; `cliff.toml`'s skip `^chore.*[Cc]hangelog` is case-sensitive (`CHANGELOG` is not skipped, bit PR #2) | med |
| `release.yml` | Publish Image job (added in PR #2) is unverified in a real release; image is not cosign-signed although `docker-bake.hcl` comments assume it; `needs: bump-version` only, so the image can publish when goreleaser fails | med |
| `.github/labeler.yml` | `go:` label matches only `cmd/**.go` and `internal/**.go`; the six library packages never get it; `repo:` lists `.goreleaser.yaml`/`.codecov.yaml` (repo uses `.yml`) | med |
| `.github/dependabot.yml` + `pr-labels.yml` | Renovate PRs must carry a semver label or `pr-labels.yml` blocks them; verify the upstream preset adds `dont-release`/`patch` | med |
| `mise.toml` + `ci.yml` | golangci-lint version pinned in both (`2.11.4` / `v2.11.4`); CI should read the mise pin | med |
| `security.yml`, `codeql.yml` | Stale comments: links to `oapi-codegen`'s scanning tab; codeql says "same slot as security.yml's grype" but security.yml runs govulncheck | low |
| `pr-labels.yml`, `license-check.yml`, `actionlint.yml` | Malformed `$schema` URL (`https://schemastore.org/https://www.schemastore.org/...`) | low |
| `trufflehog.yml` | `--results=verified,unknown` will noise on fixture tokens (`"token": "secret"` in examples) | low |
| `.github/CODEOWNERS` | "Replace @org/CHANGEME before merging" scaffold comment still present | low |
| `scripts/labels.sh` | usage says `./tools/labels.sh`; `ai` label / `docs/ai/` are scaffold leftovers | low |
| `.gitignore` | duplicated entries (`*.out`, `.idea/`, `.vscode/`, `.env`, `dist/`), a `donald-loop runtime state` entry | low |
| `Dockerfile` | distroless base unpinned by digest; check whether the Renovate docker preset pins digests | low |
| `docs/` | DESIGN-0001 still `In Review`, IMPL-0001 still `In Progress` after v0.1.0; `.docz.yaml` configures `wiki` but no `mkdocs.yml` exists | low |

What is already right: `renovate.json5` extends the upstream presets as
CLAUDE.md says; Dependabot is correctly in security-updates-only mode;
`permissions` blocks are present on every job; `catalog-info.yaml` and
`CODEOWNERS` exist for Backstage/ownership; the per-package coverage gate
is the real quality gate and passes at 82–94%.

### Observation 3 — Code review: scim, filter, patch

Correctness is deliberate and the intentional deviations are all written
down. The real risk in these packages is allocation-heavy hot paths, one of
which undercuts a headline design promise.

| Where | Finding | Sev |
| ----- | ------- | --- |
| `patch/apply.go:189` (`matchingElements`) | Builds a probe `Resource` and runs `filter.Match` **per element**; one `remove members[value eq …]` on a 5 000-member group measured 5.5 ms / 13.8 MB / 80 k allocs. `memstore.Patch` routes through `patch.Apply`, so the "native" Patcher is O(n) with a large constant too, which is exactly the Entra one-member-per-PATCH storm DESIGN-0001 constraint 8 warns about | high (perf) |
| `patch/apply.go:71`, `apply_add.go:247` | Re-sending the *identical* value to a top-level `immutable` attribute fails `400 mutability`; the sub-attribute path and the server PUT pipeline both implement match-or-error (RFC 7643 §2.2). IdPs re-send full resources | med |
| `scim/errors.go:107` | `*scim.Error` has no `Is` method and no sentinels; `errors.Is(err, scim.ErrNotFound(""))` is always false because constructors allocate. Consumers must `errors.As` then switch on fields | med |
| `scim/resource.go:98` | `Resource.Get` swallows path-resolution errors: `Get("usrName")` → `(nil, false)`, indistinguishable from unassigned | med |
| `scim/value.go:342`, `scim/user.go:85` | `findAttribute` calls `strings.ToLower` per schema attribute per lookup (7 allocs / 209 ns for `name.givenName`, under every filter and PATCH op); `User.Resource()` rebuilds the whole `UserResourceType()` literal per call (17 KB / 35 allocs) | med (perf) |
| `scim/codec.go:349` | dateTime encodes with `RFC3339`, dropping fractional seconds that decode accepts (`…00.5Z` → `…00Z`). RFC 7643 §2.3.5 permits fractions | low/med |
| `patch/normalize.go:52` | Under `Default`, a pathless replace whose object key contains `:` but is not a URN (`"foo:bar"`) becomes a pathed op and fails `invalidPath`; `Strict` ignores it. Default is stricter than Strict for this input | low/med |
| `filter/parse.go:296` | Nested valuePath inside a valFilter (`emails[type eq "work" and value[x eq 1]]`) parses although the ABNF forbids it; `Match` evaluates false and `patch` reports `noTarget` instead of `invalidFilter`/`invalidPath` | low/med |
| `filter/parse.go:208` | No nesting-depth limit; 200 000 nested `(` parse in ~65 ms via stack growth. Not a panic, but a `maxDepth` guard is cheap | low |
| `filter/match.go:22` | `Match` allocates `scim.CommonAttributes()` (4 deep clones) per call; multiplied by the per-element probe above | low/med (perf) |
| `filter/parse.go:16` | `Lenient` never reaches PATCH paths: `patch.Decode` calls `ParsePath` with no options and `Profile` has no knob | low |
| `scim/codec.go:220` | `attributes=<extension URN>` silently drops the container yet `schemas` still lists the extension; wire claims an extension not in the body | low |
| `scim/user.go:16` | `MultiValue.Primary bool` cannot express explicit `primary:false`; typed view drops `""` while `Resource.Set` stores it; typed and dynamic layers disagree on empty | low |
| `scim/codec.go:56` | `schemas` is never validated on decode or create (RFC 7643 §3 REQUIRED); acceptable as tolerant posture, should be a Strict check | low |
| `patch/profile.go:63` | `Strict`/`Default`/`Entra`/`Okta` are mutable package vars; one assignment changes every server in the process | low |
| `patch/decode.go:21` | No cap on `Operations` length; with the per-element probe, one PATCH with thousands of valuePath removes is quadratic | low/med |
| `patch/apply_add.go:108` | Two scratch-resource helpers (`coerce`, `coerceElemSub`) exist only to borrow `Set`'s coercion; an exported `scim.Coerce(attr, v)` deletes both | low (grug) |
| `scim/resource.go:107` vs `value.go:17` | Extension containers come back as `map[string]any`, complex attributes as `ComplexValue`; consumers type-switch on both | low (API) |

### Observation 4 — Code review: server, memstore, client, scimtest, cmd

Concurrency primitives (striped locks, memstore RWMutex, `lazyHandler`,
chaos `Rand` guard) hold under the race detector and the auth-exemption
split has no bypass. Two findings are release-blocking for real-IdP use.

| Where | Finding | Sev |
| ----- | ------- | --- |
| `server/typehandler.go:68` | `readBody` is `io.ReadAll` with **no `http.MaxBytesReader`**; POST/PUT/PATCH/.search buffer unbounded bodies before decode. The recorder and simulator do the same. Needs `Config.MaxBodyBytes` (default ~1 MiB) | high |
| `client/transport.go:36` | The retry loop retries network errors and 5xx for **every method including POST** with no idempotency key; a lost response after the server committed replays `POST /Users` up to 10 times → duplicate users or 409s. Gate on method idempotency, or make Create lookup-then-create by default | high |
| `server/etag.go:30` + `store.go` | `stampETag` **mutates** the resource the store returned, and the `Store` contract never says "return independent copies". A store handing back its internal pointer gets the hash written into `meta.version`; after the first GET the ETag never changes, `If-None-Match` serves stale 304s, `If-Match` cannot fail. Document the clone requirement as MUST or clone before stamping. `toystore_test.go` clones, so this is untested by construction | med |
| `server/query.go:140` | `count` has no upper bound; `?count=10000000` clones and encodes the whole table. `MaxFilterResults` caps only the scan fallback. Add `MaxPageSize`, clamped not rejected (§3.4.2.4) | med |
| `server/handler_patch.go:21` | PATCH never runs `validateResource` after `Apply`; under Strict a canonical-value violation is accepted on PATCH but rejected on PUT/POST | med |
| `server/engine.go:16` | Sort is pushed to the store with no `SortUnsupported` capability; a minimal Store that ignores `SortBy` returns unsorted pages while SPC advertises `sort.supported=true` | med |
| `server/memstore/list.go:65` | Unfiltered/unsorted List iterates a Go map, so page order is nondeterministic across requests; a client paging `/Users` without `sortBy` sees duplicates and gaps on *static* data | med |
| `client/capabilities.go:29` | `capsMu` is held across the SPC HTTP fetch; every concurrent `Patch` blocks behind one round trip, a hung SPC stalls all PATCH traffic for the full timeout | med |
| `scimtest/simulate.go:119` | `simTransport` uses `&http.Client{}` with no timeout and default redirect following; library callers with `context.Background()` hang on a black-holed target (the CLI `--timeout` masks it) and a 302 to another host is silently exercised | med |
| `scimtest/options.go:66` | `WithResourceType` **replaces** the default User+Group pair when any type is supplied; the common "add Device alongside Users" case silently drops Users. Should append, with `WithoutDefaultTypes()` to opt out | med (API) |
| `server/middleware.go:26` | `statusRecorder` lacks `Unwrap()`, blocking `http.ResponseController` and consumer middleware | low |
| `server/auth.go:38` | No constant-time compare helper; the shipped mock `verify` uses `!=` | low |
| `server/sort.go:21` | Unknown `sortBy` silently no-ops (200 unsorted) although the server knows the type and could 400 | low |
| `server/handler_crud.go:72` | PUT holds the stripe lock across decode/merge/validate; PATCH decodes before locking | low |
| `server/memstore/crud.go:63` | CAS compares raw `meta.version` strings; a client sending a normalized tag gets a spurious 412 | low |
| `client/query.go:78` | A server returning a fixed non-empty page with `totalResults: 0` loops forever; a same-page guard is cheap | low |
| `client/options.go:33` | `WithBearerToken` is static; real provisioners use expiring OAuth tokens → `WithTokenSource(func(ctx) (string, error))` | low |
| `scimtest/seed.go:51` | Seeds are Users/Groups only; custom types cannot be seeded | low |
| `scimtest/recorder.go:67` | Recorder buffers every body for the process lifetime; long chaos loops grow linearly | low |
| `cmd/scimkit/mock.go:46` | `http.Server` sets only `ReadHeaderTimeout`; with the unbounded body a slow-loris holds a goroutine on the container | low |
| `cmd/scimkit/mock.go:25`, `exercise.go:21` | `--fail-rate` not validated to `[0,1]`; `--token` on the command line lands in `ps`/history (add `SCIMKIT_TOKEN` env fallback); no warning when binding non-loopback with no token | low |

### Observation 5 — DESIGN-0001 against the shipped code

**Proved right.** Q2 (hybrid `Resource` + hand-written views) landed the
PATCH engine, projection, and corpus replay with no reflection; Q3 (5-method
`Store` + capability sniffing) was proven by the toy-store lifecycle test;
Q4 (Default server / Strict mock) caught real shape bugs while the corpus
passes under IdP profiles; Q7 (single CLI) reused all plumbing; IMPL Q5
(corpus as scenario source) means tests and simulator cannot drift; schema
generation from RFC JSON surfaced the `schemaExtensions` vendoring bug that
hand transcription would have hidden.

**Stale or aspirational.**

- **Q6 (Go floor) is the one decision whose deferral condition is now met.**
  The directive equals the toolchain, so the Go bump in Observation 1
  raises every consumer's floor. The moment the code is imported by a
  second module (the private-library scenario), option a is required: a
  `go 1.26` directive with no patch, a `toolchain` line for dev, a CI matrix
  on oldstable + stable, and a Renovate rule that leaves the directive
  alone.
- Phase 7 is entirely unstarted (`schema gen`, scim2-tester job, fuzz
  cron, `examples/`, docs site). `schema gen` is cited three times as the
  answer for custom schemas, so the design over-promises today.
- "Mock spin-off", UI, multi-tenant virtual servers are narrative only.
- The `client` "typed sugar" is really schema-bound `*scim.Resource`
  services; the doc was corrected in PR #2 but the ergonomic gap
  (`UserFromResource` juggling at every call site) is a v1 API question.
- `server.Config.MaxFilterResults` is required with no default, a sharp
  knob for a "mount and go" story.

**Gaps a v1 design should address** (none are v0 regressions; all are
absent by omission):

1. Request body limits (Observation 4) — the design never mentions them.
2. Observability seam: no metrics hook, no trace/request-id propagation;
   only `slog` lines. Consumers cannot count PATCH no-ops, 412s, or filter
   fallbacks without wrapping the handler.
3. Backpressure: constraint 8 names Entra retry storms, striped locks
   serialize, nothing sheds load.
4. Multi-tenancy: one `BaseURL`, one store set per handler. Should be a
   stated non-goal or a v1 design.
5. Custom resource types: `Registration` accepts any type, but there is no
   typed-view path without `schema gen`, and `scimtest.WithResourceType`
   replaces defaults.
6. Auth seam: `Wrap + Scheme` is fine; an OAuth/JWT bearer helper is the
   first thing an SSO library wants.
7. API stability tooling: "minor may break" is policy, but no `apidiff` /
   `gorelease` gate makes breaks deliberate.
8. O(delta) group membership is promised by constraint 8 and the `Patcher`
   docs, but the memstore native path is O(n) with large constants
   (Observation 3). The reference implementation should honor the contract
   it documents.

**Zero-runtime-deps sustainability**: cheap to keep; nothing used needs
`golang.org/x`. The unsustainable half is the toolchain-coupled directive,
which is Q6.

### Observation 6 — Test gaps and Go 1.27 readiness

- **No `testdata/fuzz/*` corpora exist anywhere**, although IMPL-0001
  phases 1–3 record "seed corpus committed"; seeds live only in `f.Add`
  calls, so any crasher found by a fuzz run is not persisted. Run each
  target once with `-fuzztime` and commit the generated directories before
  the phase-7 cron job.
- No test sends an oversized body, a `count` beyond the data set, a
  `sortBy` on an unknown attribute, a network error on POST, or a hanging
  or redirecting `Simulate` target.
- Untested branches: `add` with a value filter on a multi-valued *simple*
  attribute; `replace` whole-element omitting an assigned immutable sub;
  nested valuePath.
- Go 1.27: nothing breaks. `b.Loop`, `for range n`, `slices`/`maps` are in
  use. Cheap modernizations: `errors.AsType[*scim.Error]` replaces eleven
  `var e *scim.Error; errors.As` sites; `slices.SortStableFunc` for
  `server/sort.go` and the four `sort.Slice` fold comparators (`modernize`
  will flag them); `testing/synctest` can replace the `sleeper` seam in
  `client/retry.go`; `strings.SplitSeq` for `splitAttrList`/`etagMatch`.
  `encoding/json/v2` would replace `marshalOrdered` and the hand-rolled
  case-insensitive key matching but is still experimental.

## Conclusion

**Answer:** Confirmed, with one surprise. The protocol core is sound and
every intentional deviation is documented; findings cluster exactly where
hypothesized (toolchain, scaffold leftovers, Q6). The surprise is that the
two release-blocking code findings are not SCIM semantics at all but
boundary hygiene: an unbounded request body on the server and non-idempotent
POST retry in the client. The second-tier surprise is performance: the
per-element valuePath probe makes the "O(delta)" group-membership promise
false even on the reference store.

Severity roll-up across the three reviews:

| Sev | Count | Release-blocking for real-IdP use |
| --- | ----- | --------------------------------- |
| high | 4 | Go CVEs, body limit, POST retry, O(n) member PATCH |
| med | 24 | ETag/clone contract, page cap, `WithResourceType`, Q6, capability lock, simulator timeout |
| low | ~35 | polish |

## Recommendation

Sequence as four PRs on `chore/cleaunup` and successors, smallest and most
urgent first:

1. **Toolchain bump (this branch, `patch` label).** Go 1.26.4 → 1.27.2 in
   `go.mod`, `mise.toml`, `Dockerfile`; golangci-lint 2.11.4 → 2.14.0 in
   `mise.toml` and `ci.yml` (then fix whatever `gocritic`/`testifylint`
   newly flag); testify 1.12.1; all pinned formatters to latest; replace
   `latest` with explicit pins and `# renovate:` annotations where Renovate
   can track them. Re-run `govulncheck` to confirm zero findings. Decide Q6
   in the same PR: recommend flipping to option a now (`go 1.26`
   directive, `toolchain go1.27.2`, oldstable matrix) because the private
   copy will need it immediately.
2. **Support-file cleanup (same or next PR, `patch`).** Delete the
   Makefile (justfile is canonical, libtftest precedent) and the
   "either/or" header; scrub `.golangci.yml` of foreign-project rules;
   fix labeler globs so library packages get `go`; one action-pinning
   style (SHA + version comment, matching `changelog-regen.yml`); fix
   `$schema` URLs, stale comments, CODEOWNERS placeholder, `labels.sh`
   usage, `.gitignore` duplicates; `.codecov.yml` ignores aligned with the
   gate; `trufflehog --results=verified`; make the `cliff.toml` skip
   case-insensitive; have `release.yml`'s image job `needs: [release]`
   and add cosign keyless signing; move DESIGN-0001 → `Implemented`,
   IMPL-0001 → `Completed` and open IMPL-0002 for phase 7 + the v1 gaps.
3. **Boundary hardening (`minor`, API additive).** `Config.MaxBodyBytes`
   + `MaxPageSize` on the server; method-gated retry (`RetryConfig.
   RetryNonIdempotent bool`, default false) and `WithTokenSource` on the
   client; `Store` clone contract documented as MUST and the handler
   cloning before `stampETag`; `validateResource` after PATCH; a
   `SortUnsupported` capability; deterministic memstore order; a
   `singleflight`-style capability fetch; simulator client timeout +
   `CheckRedirect`; `WithResourceType` append semantics +
   `WithoutDefaultTypes`; `(*scim.Error).Is`; `Resource.Lookup` that
   surfaces path errors; immutable match-or-error at top level; the
   `Default`-stricter-than-`Strict` key split; nested-valuePath parse
   error; `RFC3339Nano`. Commit fuzz corpora. Each is small and
   independently testable.
4. **Performance pass (`minor`).** Evaluate valuePath filters directly
   against `ComplexValue` elements instead of materializing a probe
   `Resource` per element; pre-fold schema attribute names; share one
   immutable built-in `*ResourceType` for the typed-view bridge; stop
   cloning `CommonAttributes()` per `Match`. Add a benchmark for the
   5 000-member remove so the O(delta) claim is measured, not asserted.

The private-repo / Okta SSO library question is answered separately in
the uncommitted working notes (`.notes/private-okta-sso-library.md`);
items 1 and 3 above are its prerequisites.

## References

- [DESIGN-0001 — scimkit library architecture](../design/0001-scimkit-library-architecture.md)
- [IMPL-0001 — scimkit v0 implementation](../impl/0001-scimkit-v0-implementation.md)
- [PR #2 — scimkit v0 (phases 1–6) → v0.1.0](https://github.com/donaldgifford/scimkit/pull/2)
- [Go vulnerability database](https://pkg.go.dev/vuln/) — GO-2026-5026 … GO-2026-6617
- [RFC 7643 §2.2 (mutability), §2.3.5 (dateTime), §3 (schemas)](https://datatracker.ietf.org/doc/html/rfc7643)
- [RFC 7644 §3.4.2.4 (pagination), §3.14 (versioning)](https://datatracker.ietf.org/doc/html/rfc7644)
- libtftest (`go 1.27.1`, golangci-lint 2.13.2, Makefile removed in its IMPL-0005) as the fleet reference library
