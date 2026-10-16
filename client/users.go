package client

import (
	"context"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// UserService is the typed client for the standard User resource type.
// It embeds ResourceService, so Create/Get/Replace/Patch/Delete/List/
// Lookup are all available directly.
type UserService struct {
	*ResourceService
}

// Deactivate soft-deletes a user by PATCHing active to false — the Okta
// deletion model, an alternative to Delete.
func (s *UserService) Deactivate(ctx context.Context, id string) (*scim.Resource, error) {
	return s.Patch(ctx, id, []patch.Op{{
		Kind:  patch.KindReplace,
		Path:  &filter.Path{Attr: "active"},
		Value: false,
	}})
}
