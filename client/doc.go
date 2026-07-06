// Package client is a generic typed SCIM 2.0 client for building custom
// provisioners: resource CRUD, transparent list pagination, retry with
// 429/Retry-After backoff, capability discovery from ServiceProviderConfig,
// and provisioner idioms (lookup-then-create, idempotent group membership,
// deactivate-as-delete).
package client
