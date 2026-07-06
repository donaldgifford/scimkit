# scimkit

Production-grade SCIM 2.0 primitives for Go — composable building blocks
for custom SCIM service providers (servers that receive IdP provisioning)
and SCIM clients (custom provisioners), plus a mock SCIM server and IdP
traffic simulator for testing integrations locally without a live IdP.

> **Status: pre-v0.1.0.** The architecture is specified in
> [DESIGN-0001](docs/design/0001-scimkit-library-architecture.md);
> packages are being built out per its rollout plan. APIs will move
> until v0.1.0 is tagged.

## What's in the box

| Package | Purpose |
| --- | --- |
| `scim` | Core RFC 7643 model: schemas, the canonical `Resource`, typed `User`/`Group` views, errors, URNs |
| `filter` | RFC 7644 filter/path parsing, evaluation against resources, and a client-side filter builder |
| `patch` | PATCH decode → normalize → apply, with compat `Profile`s for real IdP behavior (Entra, Okta) |
| `server` | `net/http` service-provider toolkit — implement a small `Store`, get a compliant SCIM endpoint |
| `client` | Generic typed client for provisioners: CRUD, pagination, retries, capability discovery |
| `scimtest` | Embeddable mock server for `go test` + IdP traffic simulator |
| `cmd/scimkit` | CLI: `scimkit mock` (local SCIM server) and `scimkit exercise` (simulator), shipped as a Docker image |

Each layer is usable on its own: the filter parser without the server,
the PATCH engine without a store, the typed models without HTTP.

## Install

```sh
go get github.com/donaldgifford/scimkit
```

Zero runtime dependencies — the library imports only the standard
library.

## Mock server

```sh
docker run --rm -p 8080:8080 ghcr.io/donaldgifford/scimkit:latest
# SCIM 2.0 endpoint at http://localhost:8080 — point your provisioner at it
```

## Development

```sh
mise install                  # toolchain
just                          # task menu
just build                    # CLI binary at build/bin/scimkit
just test                     # race + coverage
just check                    # pre-commit gate (lint + test)
```

## Releases

PR-label driven: every PR carries one of `major`/`minor`/`patch`/
`dont-release`; merging to main tags and releases automatically via
goreleaser. Pre-1.0, breaking changes ride `minor`.

## Conventions

See `CLAUDE.md` for the operating notes and
[DESIGN-0001](docs/design/0001-scimkit-library-architecture.md) for the
architecture and its decision record.

## License

Apache-2.0
