package patch

import (
	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// Op is one decoded, normalized PATCH operation. Path is nil for pathless
// add and replace operations, which target the whole resource; it is the
// parsed RFC 7644 Figure 7 path otherwise. Value holds the decoded JSON
// value — json.Number preserves the integer/decimal distinction until
// Apply resolves the target attribute's schema type — and is nil for
// remove operations.
type Op struct {
	Kind  Kind
	Path  *filter.Path
	Value any
}

// Result reports what Apply produced. Resource is the patched working
// copy; the input resource is never mutated. Changed reports whether any
// operation actually modified the resource — a fully idempotent PatchOp
// (adding a group member that is already present, for example) yields
// Changed=false, which callers use to skip the meta.version and
// meta.lastModified bumps (RFC 7644 §3.5.2).
type Result struct {
	Resource *scim.Resource
	Changed  bool
}
