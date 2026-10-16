package server

import (
	"context"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// Store is the minimum a consumer implements per resource type.
// Resources passed in are already validated, normalized, and carry
// server-assigned id and meta.
//
// Error contract: return *scim.Error values for protocol-mappable
// failures — scim.ErrNotFound for a missing id, scim.ErrUniqueness when
// a uniqueness constraint would break (the server answers 409), and
// scim.ErrPreconditionFailed from Replace when version-based
// compare-and-swap fails. Any other error becomes a generic 500.
//
// Version contract: a store that tracks resource versions treats the
// incoming resource's meta.version on Replace as a compare-and-swap
// token — mismatch with the stored version returns
// scim.ErrPreconditionFailed — and bumps the version itself on a
// successful swap. A store that does not track versions ignores
// meta.version; the server then derives ETags by hashing resource
// content, and concurrent writers degrade to last-write-wins.
type Store interface {
	Create(ctx context.Context, res *scim.Resource) (*scim.Resource, error)
	Get(ctx context.Context, id string) (*scim.Resource, error)
	List(ctx context.Context, q Query) (Page, error)
	Replace(ctx context.Context, id string, res *scim.Resource) (*scim.Resource, error)
	Delete(ctx context.Context, id string) error
}

// Query is the read request a Store's List receives. Filter is pushed
// down verbatim; a store that cannot evaluate an expression says so via
// the FilterUnsupported capability and the server falls back to
// scanning with filter.Match.
type Query struct {
	// Filter is nil for an unfiltered list.
	Filter filter.Expr
	// SortBy is nil when no ordering was requested. Stores producing
	// the final filtered set are responsible for its order; the
	// SortResources helper implements the RFC 7644 §3.4.2.3 semantics.
	SortBy *filter.Path
	// Ascending is meaningful only when SortBy is set.
	Ascending bool
	// StartIndex is 1-based, already normalized by the server.
	StartIndex int
	// Count is the page size; 0 means a count-only query — return
	// TotalResults with no Resources.
	Count int
}

// Page is a List result. TotalResults is always the filtered total,
// independent of pagination.
type Page struct {
	Resources    []*scim.Resource
	TotalResults int
}

// Patcher is the optional native-PATCH capability. Stores holding large
// multi-valued state (group membership) should implement it so a
// two-member change does not round-trip the whole resource through
// Get and Replace.
//
// Contract: apply ops atomically, bump meta.version and
// meta.lastModified only when an op actually changed the resource (an
// idempotent PatchOp leaves both untouched), and return the resulting
// resource. Without this capability the server falls back to
// Get → patch.Apply → conditional Replace.
type Patcher interface {
	Patch(ctx context.Context, id string, ops []patch.Op) (*scim.Resource, error)
}

// FilterUnsupported is the optional capability a store implements when
// it cannot evaluate every filter expression natively. Reporting false
// for an expression makes the server scan with filter.Match instead,
// capped by Config.MaxFilterResults. A store without this capability is
// assumed to support full push-down.
type FilterUnsupported interface {
	SupportsFilter(f filter.Expr) bool
}
