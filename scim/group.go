package scim

import (
	"fmt"
)

// Member is one element of the Group members attribute (RFC 7643 §4.2).
// Ref maps the $ref sub-attribute; Type is "User" or "Group".
type Member struct {
	Value string
	Ref   string
	Type  string
}

// Group is the typed view of the core Group resource (RFC 7643 §4.2). Zero
// fields are unassigned.
type Group struct {
	ID          string
	ExternalID  string
	Meta        Meta
	DisplayName string
	Members     []Member
}

// Resource converts the typed view to a canonical Resource backed by the
// built-in Group resource type.
func (g *Group) Resource() (*Resource, error) {
	rt := GroupResourceType()
	r := NewResource(&rt)
	r.SetMeta(g.Meta)
	fields := []struct {
		path  string
		value any
	}{
		{"id", stringValue(g.ID)},
		{"externalId", stringValue(g.ExternalID)},
		{"displayName", stringValue(g.DisplayName)},
		{"members", memberElements(g.Members)},
	}
	for _, f := range fields {
		if f.value == nil {
			continue
		}
		if err := r.Set(f.path, f.value); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// GroupFromResource converts a canonical Resource into the typed view. The
// resource's core schema must be the Group schema.
func GroupFromResource(r *Resource) (*Group, error) {
	if foldName(r.ResourceType().Schema.ID) != foldName(SchemaGroup) {
		return nil, fmt.Errorf("resource has core schema %q, want %q", r.ResourceType().Schema.ID, SchemaGroup)
	}
	return &Group{
		ID:          getString(r, "id"),
		ExternalID:  getString(r, "externalId"),
		Meta:        r.Meta(),
		DisplayName: getString(r, "displayName"),
		Members:     membersAt(r),
	}, nil
}

func memberElements(vals []Member) any {
	if len(vals) == 0 {
		return nil
	}
	out := make(MultiComplexValue, 0, len(vals))
	for _, m := range vals {
		cv := make(ComplexValue)
		putString(cv, "value", m.Value)
		putString(cv, "$ref", m.Ref)
		putString(cv, "type", m.Type)
		out = append(out, cv)
	}
	return out
}

func membersAt(r *Resource) []Member {
	mcv := multiComplexAt(r, "members")
	if mcv == nil {
		return nil
	}
	out := make([]Member, len(mcv))
	for i, cv := range mcv {
		out[i] = Member{
			Value: stringOf(cv["value"]),
			Ref:   stringOf(cv["$ref"]),
			Type:  stringOf(cv["type"]),
		}
	}
	return out
}
