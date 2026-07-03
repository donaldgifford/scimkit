# CLAUDE.md

Per-repo orientation for `donaldgifford/scimkit`. This file is a
Go-shaped overlay on top of the universal homelab `CLAUDE.md` (see
[homelab/docs](https://github.com/donaldgifford/docs)); the universals
apply here too — only repo-specific guidance is captured below.

## What this is

`scimkit` is a Go **library** of production-grade SCIM 2.0 primitives
(RFC 7643/7644) for building both SCIM service providers (servers that
receive IdP provisioning from Okta/Entra/OneLogin) and SCIM clients
(custom provisioners) — plus a `scimkit` CLI (mock SCIM server + IdP
traffic simulator) shipped as a distroless container image.

**The architecture is specified in
`docs/design/0001-scimkit-library-architecture.md` (DESIGN-0001).**
Read it before touching package boundaries or public API; its Open
Questions section records the decisions and their rationale.

## Layout

```text
scim/           # core: schemas, Resource, typed views, errors, URNs
filter/         # RFC 7644 filter/path parsing, evaluation, builder
patch/          # PATCH decode/normalize/apply + compat Profiles
server/         # net/http service-provider toolkit (Store contract)
client/         # generic typed SCIM client for provisioners
scimtest/       # embeddable mock server + IdP simulator for go test
cmd/scimkit/    # CLI (mock/exercise subcommands) — keep thin
internal/       # non-exported helpers only; the public API lives above
docs/           # docz-managed docs (RFC/ADR/DESIGN/IMPL/PLAN/INV)
Dockerfile      # multi-stage distroless build of the CLI
.goreleaser.yml # release config (multi-arch archives + checksums)
mise.toml       # pinned go + golangci-lint + universal tools
justfile        # `just` task runner — `just` for the menu
```

Package dependency direction is strictly one-way — do not add edges:

```text
scim ← filter ← patch ← server ← scimtest ← cmd/scimkit
   ↖──── client ─────────────────↗
```

## Workflows

### Build + test

- `just build` — CLI binary at `build/bin/scimkit`
- `just test` — race detector; `just test-coverage` writes `coverage.out`
- `just lint` / `just lint-fix` — golangci-lint
- `just check` — pre-commit gate (lint + test)

### Release

- **PR-label driven, no manual tagging.** Every PR must carry exactly
  one of `major` / `minor` / `patch` / `dont-release` (enforced by
  `pr-labels.yml`). On merge to main, `release.yml` (pr-semver-bump)
  tags and goreleaser publishes multi-arch archives.
- Pre-1.0: breaking API changes ride the `minor` label and get a
  CHANGELOG-visible note.
- Version metadata (`main.version`, `main.commit`, `main.date`) is
  injected via `-ldflags`.

### Container

`docker.just` recipes drive `docker buildx bake` (see `docker-bake.hcl`);
the image runs `scimkit mock` by default. Mock state is in-memory —
the distroless rootfs is read-only (nonroot UID 65532).

## Go conventions (repo-specific)

- **Zero runtime dependencies.** Stdlib only; `golang.org/x/*` counts
  as a dependency. Test-only deps (go-cmp/testify) are allowed — they
  don't propagate to consumers. (DESIGN-0001 Q8)
- **`go.mod` go directive matches `mise.toml`** (currently `go 1.26.4`),
  fleet convention. Decoupling to an N-1 floor for consumers was
  considered and deferred (DESIGN-0001 Q6) — revisit when external
  consumers need it.
- **`internal/` is for helpers only.** This repo's product is its public
  API; new packages go top-level per the design, not under `internal/`.
- **Wire-format correctness is a feature.** Attribute names fold case on
  input but output emits schema-declared casing; presence
  (`null`/`[]`/absent are equivalent) is tracked by `scim.Resource` —
  never shortcut presence with Go zero values.
- **Strictness posture** (DESIGN-0001 Q4): `server` defaults to
  tolerant-input/strict-output (`patch.Default`); `scimtest` and the
  mock default to `patch.Strict`. IdP quirks belong in named Profiles,
  never in the core semantics.
- Universals apply: `slog` (default handler set in `main()`), no
  `init()` for behavior, tests next to code, errors wrap with `%w`.

## Gotchas

- **goreleaser v2 config**: `archives[].format` became
  `archives[].formats` (slice). Validate with `goreleaser check`.
- **CI coverage** comes from `just test-coverage` → `coverage.out`
  (not `coverage.txt`).
- **Golden IdP corpus** under `testdata/idp/` doubles as the simulator's
  scenario source — changing a fixture changes simulator behavior.

## Renovate

- `go.mod` updates are PR'd by Renovate's Go module manager.
- Container base images in `Dockerfile` are PR'd by the Docker manager.
- `mise.toml` versions are handled by a custom regex manager configured
  upstream in `donaldgifford/renovate-config`.
