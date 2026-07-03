// Package scimtest provides testing utilities for SCIM integrations: an
// embeddable in-memory mock SCIM server (backed by the real server
// package, strict by default) for go test, and an IdP traffic simulator
// that replays realistic identity-provider request shapes — including
// documented non-compliant ones — against a target SCIM server.
package scimtest
