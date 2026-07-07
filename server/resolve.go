package server

import (
	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// resolveAttr maps a parsed path to schema metadata: the top-level
// attribute and, when named, the sub-attribute. ok=false means the path
// is undefined for this resource type.
func resolveAttr(rt *scim.ResourceType, p *filter.Path) (attr, sub *scim.Attribute, ok bool) {
	attrs, ok := namespaceAttrs(rt, p.URN)
	if !ok {
		return nil, nil, false
	}
	attr = scim.FindAttribute(attrs, p.Attr)
	if attr == nil && p.URN == "" {
		attr = scim.FindAttribute(scim.CommonAttributes(), p.Attr)
	}
	if attr == nil {
		return nil, nil, false
	}
	if p.SubAttr == "" {
		return attr, nil, true
	}
	if attr.Type != scim.TypeComplex {
		return nil, nil, false
	}
	sub = scim.FindAttribute(attr.SubAttributes, p.SubAttr)
	if sub == nil {
		return nil, nil, false
	}
	return attr, sub, true
}

// namespaceAttrs returns the attribute list a URN qualifier selects.
func namespaceAttrs(rt *scim.ResourceType, urn string) ([]scim.Attribute, bool) {
	if urn == "" || scim.FoldName(urn) == scim.FoldName(rt.Schema.ID) {
		return rt.Schema.Attributes, true
	}
	folded := scim.FoldName(urn)
	for i := range rt.Extensions {
		if scim.FoldName(rt.Extensions[i].Schema.ID) == folded {
			return rt.Extensions[i].Schema.Attributes, true
		}
	}
	return nil, false
}

// attrPathText renders the Resource.Get path for a path's top-level
// attribute; fullPathText includes the sub-attribute.
func attrPathText(p *filter.Path) string {
	if p.URN != "" {
		return p.URN + ":" + p.Attr
	}
	return p.Attr
}

func fullPathText(p *filter.Path) string {
	s := attrPathText(p)
	if p.SubAttr != "" {
		s += "." + p.SubAttr
	}
	return s
}
