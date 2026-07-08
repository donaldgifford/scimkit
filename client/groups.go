package client

import (
	"context"
	"errors"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// GroupService is the typed client for the standard Group resource type.
// It embeds ResourceService for the generic operations and adds
// idempotent membership helpers.
type GroupService struct {
	*ResourceService
}

// EnsureMember adds a member to a group and is idempotent: adding a
// member that is already present changes nothing (the server's
// value-identity dedupe makes the PatchOp a no-op). It emits the
// spec-shaped add-to-members op.
func (s *GroupService) EnsureMember(ctx context.Context, groupID, memberID string) (*scim.Resource, error) {
	return s.Patch(ctx, groupID, []patch.Op{{
		Kind:  patch.KindAdd,
		Path:  &filter.Path{Attr: "members"},
		Value: []any{map[string]any{"value": memberID}},
	}})
}

// RemoveMember removes a member from a group and is idempotent:
// removing an absent member is success. It emits the spec-shaped
// members[value eq "id"] valuePath remove; a target that wrongly
// answers noTarget for a non-matching remove is tolerated as success.
func (s *GroupService) RemoveMember(ctx context.Context, groupID, memberID string) (*scim.Resource, error) {
	res, err := s.Patch(ctx, groupID, []patch.Op{{
		Kind: patch.KindRemove,
		Path: &filter.Path{Attr: "members", ValueFilter: filter.Eq("value", memberID)},
	}})
	if err != nil && isNoTarget(err) {
		return s.Get(ctx, groupID)
	}
	return res, err
}

// isNoTarget reports whether err is a noTarget protocol error, which
// some targets wrongly return for a valuePath remove that matches
// nothing (RFC 7644 Table 4 says it MUST NOT fail).
func isNoTarget(err error) bool {
	var scimErr *scim.Error
	return errors.As(err, &scimErr) && scimErr.ScimType == scim.ScimTypeNoTarget
}
