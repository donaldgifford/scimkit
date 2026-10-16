# scimkit

Production-grade SCIM 2.0 primitives for Go — composable building blocks
for custom SCIM service providers (servers that receive IdP provisioning)
and SCIM clients (custom provisioners), plus a mock SCIM server and IdP
traffic simulator for testing integrations locally without a live IdP.

> **Status: v0.** The architecture is specified in
> [DESIGN-0001](docs/design/0001-scimkit-library-architecture.md).
> Pre-1.0, minor versions may break the API; breaking changes are called
> out in the [CHANGELOG](CHANGELOG.md).

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
library. Requires Go 1.26 or newer (the `go.mod` floor; development and
CI run on the pinned `toolchain`, and CI tests both).

## Quickstarts

Every snippet below is a trimmed copy of a runnable `Example` test in the
package it demonstrates, so it compiles and passes in CI.

### Service provider

Register the built-in User and Group types against a store, mount the
handler, and it answers CRUD, PATCH, filtered list, and discovery. Swap
`memstore` for your own five-method `server.Store` to back it with a real
database ([`server/example_test.go`](server/example_test.go)).

```go
handler, err := server.New(server.Config{
    BaseURL: "https://app.example.com/scim/v2",
    ResourceTypes: []server.Registration{
        {Type: scim.UserResourceType(), Store: memstore.New(scim.UserResourceType())},
        {Type: scim.GroupResourceType(), Store: memstore.New(scim.GroupResourceType())},
    },
    Profile:          patch.Default, // tolerant input, strict output
    MaxFilterResults: 200,
})
if err != nil {
    log.Fatal(err)
}

mux := http.NewServeMux()
mux.Handle("/scim/v2/", http.StripPrefix("/scim/v2", handler))
```

### Provisioner client

Look a user up by `userName` before creating it (the Okta/OneLogin flow),
add it to a group idempotently, and iterate a filtered list with
transparent pagination ([`client/example_test.go`](client/example_test.go)).

```go
c, err := client.New("https://target.example.com/scim/v2",
    client.WithBearerToken(token))
if err != nil {
    log.Fatal(err)
}

// Lookup is 0-or-1: nil means not found, not an error.
u, err := c.Users().Lookup(ctx, filter.Eq("userName", "bjensen@example.com"))
if err != nil {
    log.Fatal(err)
}
if u == nil {
    user := scim.User{UserName: "bjensen@example.com", DisplayName: "Barbara Jensen"}
    res, _ := user.Resource()
    u, err = c.Users().Create(ctx, res)
}

// EnsureMember is idempotent: already-present is success.
userID, _ := u.Get("id")
_, err = c.Groups().EnsureMember(ctx, groupID, userID.(string))

// List follows pagination for you; the loop yields one error at most.
for r, err := range c.Users().List(ctx, client.Query{Filter: filter.Sw("userName", "b")}) {
    if err != nil {
        log.Fatal(err)
    }
    name, _ := r.Get("userName")
    fmt.Println(name)
}
```

### Mock server in `go test`

Point your provisioner at an in-memory SCIM server backed by the real
`server` package, then assert on what it sent
([`scimtest/example_test.go`](scimtest/example_test.go)). The mock
defaults to the strict profile so sloppy requests fail in tests, not in
the field.

```go
func TestProvisioner(t *testing.T) {
    ts := scimtest.NewServer(t,
        scimtest.WithBearerToken("secret"),
        scimtest.WithSeedJSON([]byte(`{"users": [
            {"schemas": ["urn:ietf:params:scim:schemas:core:2.0:User"],
             "userName": "bjensen@example.com"}
        ]}`)),
    )

    runMyProvisioner(t, ts.URL(), "secret")

    last, _ := ts.Recorder().Last()
    if last.Method != http.MethodPatch {
        t.Fatalf("expected a PATCH, got %s %s", last.Method, last.Path)
    }
}
```

### IdP traffic simulator

Replay a built-in IdP scenario — real Entra/Okta/OneLogin request shapes,
including the non-compliant ones — against any SCIM endpoint and inspect
the report.

```go
sc, _ := scimtest.LookupScenario(scimtest.ScenarioEntraUserLifecycle)
report, err := scimtest.Simulate(ctx, targetURL, token, sc)
if err != nil {
    log.Fatal(err)
}
fmt.Println(report.Passed, report.Text())
```

The same thing from the CLI, with `text` or `json` reports and a non-zero
exit on failure:

```sh
scimkit exercise --target https://target.example.com/scim/v2 --token "$TOKEN" \
  --scenario entra-user-lifecycle --report json
```

Scenarios: `rfc-strict-lifecycle`, `entra-user-lifecycle`,
`entra-group-membership`, `okta-user-lifecycle`, `okta-group-put`,
`onelogin-lookup-dance`.

### Mock server in Docker

The image runs `scimkit mock` by default. State is in-memory and the
rootfs is read-only; seed it with a mounted JSON file.

```sh
docker run --rm -p 8080:8080 ghcr.io/donaldgifford/scimkit:latest
# SCIM 2.0 endpoint at http://localhost:8080 — point your provisioner at it

docker run --rm -p 8080:8080 -v "$PWD/seed.json:/seed.json:ro" \
  ghcr.io/donaldgifford/scimkit:latest \
  mock --profile okta --token secret --seed /seed.json --latency 50ms --fail-rate 0.05
```

`--profile` picks the compat posture (`strict` by default, or `default`,
`entra`, `okta`); `--latency` and `--fail-rate` inject chaos for retry
testing.

## Development

```sh
mise install                  # toolchain
just                          # task menu
just build                    # CLI binary at build/bin/scimkit
just test                     # race + coverage
just check                    # pre-commit gate (lint + test)
just self-check               # simulator ↔ mock self-check over the built CLI
```

## Releases

PR-label driven: every PR carries one of `major`/`minor`/`patch`/
`dont-release`; merging to main tags and releases automatically via
goreleaser and publishes the container image to GHCR, signed keyless with
cosign. Pre-1.0, breaking changes ride `minor`.

## Conventions

See `CLAUDE.md` for the operating notes and
[DESIGN-0001](docs/design/0001-scimkit-library-architecture.md) for the
architecture and its decision record.

## License

Apache-2.0
