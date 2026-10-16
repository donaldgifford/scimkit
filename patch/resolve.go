package patch

import (
	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// commonAttrs is the package's read-only copy of the RFC 7643 §3.1 common
// attributes; CommonAttributes() deep-clones on every call.
var commonAttrs = scim.CommonAttributes()

// resolveTarget maps a parsed path to schema metadata: the top-level
// attribute and, when the path names one, the sub-attribute. ok=false
// means the path is undefined for this resource type — normalization
// passes skip such ops and leave errors to Apply.
func resolveTarget(rt *scim.ResourceType, p *filter.Path) (attr, sub *scim.Attribute, ok bool) {
	attrs, ok := namespaceAttrs(rt, p.URN)
	if !ok {
		return nil, nil, false
	}
	attr = findAttr(attrs, p.Attr)
	if attr == nil && p.URN == "" {
		attr = findAttr(commonAttrs, p.Attr)
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
	sub = findAttr(attr.SubAttributes, p.SubAttr)
	if sub == nil {
		return nil, nil, false
	}
	return attr, sub, true
}

// namespaceAttrs returns the attribute list a URN qualifier selects: the
// core schema for "" or the core URN, a declared extension's attributes
// otherwise.
func namespaceAttrs(rt *scim.ResourceType, urn string) ([]scim.Attribute, bool) {
	if urn == "" || scim.FoldName(urn) == scim.FoldName(rt.Schema.ID) {
		return rt.Schema.Attributes, true
	}
	return extensionAttrs(rt, urn)
}

// extensionAttrs resolves a URN to a declared extension's attributes.
func extensionAttrs(rt *scim.ResourceType, urn string) ([]scim.Attribute, bool) {
	folded := scim.FoldName(urn)
	for i := range rt.Extensions {
		if scim.FoldName(rt.Extensions[i].Schema.ID) == folded {
			return rt.Extensions[i].Schema.Attributes, true
		}
	}
	return nil, false
}

func findAttr(attrs []scim.Attribute, name string) *scim.Attribute {
	return scim.FindAttribute(attrs, name)
}
