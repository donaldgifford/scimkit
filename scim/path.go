package scim

import (
	"fmt"
	"strings"
)

// Common attributes defined outside every resource schema (RFC 7643 §3.1).
// The slice is package state but treated as immutable: resolution hands out
// pointers into it and nothing may write through them.
var commonAttributes = []Attribute{
	{
		Name:       "id",
		Type:       TypeString,
		CaseExact:  true,
		Mutability: MutabilityReadOnly,
		Returned:   ReturnedAlways,
		Uniqueness: UniquenessServer,
	},
	{
		Name:       "externalId",
		Type:       TypeString,
		CaseExact:  true,
		Mutability: MutabilityReadWrite,
		Returned:   ReturnedDefault,
		Uniqueness: UniquenessNone,
	},
	{
		Name:       attrMeta,
		Type:       TypeComplex,
		Mutability: MutabilityReadOnly,
		Returned:   ReturnedDefault,
		SubAttributes: []Attribute{
			{Name: "resourceType", Type: TypeString, CaseExact: true, Mutability: MutabilityReadOnly, Returned: ReturnedDefault},
			{Name: "created", Type: TypeDateTime, Mutability: MutabilityReadOnly, Returned: ReturnedDefault},
			{Name: "lastModified", Type: TypeDateTime, Mutability: MutabilityReadOnly, Returned: ReturnedDefault},
			{Name: "location", Type: TypeString, CaseExact: true, Mutability: MutabilityReadOnly, Returned: ReturnedDefault},
			{Name: "version", Type: TypeString, CaseExact: true, Mutability: MutabilityReadOnly, Returned: ReturnedDefault},
		},
	},
	{
		Name:        attrSchemas,
		Type:        TypeString,
		MultiValued: true,
		CaseExact:   true,
		Mutability:  MutabilityReadWrite,
		Returned:    ReturnedAlways,
	},
}

// resolvedPath is the outcome of resolving standard attribute notation
// (RFC 7644 §3.10) against a ResourceType. Attribute pointers reference the
// ResourceType's schema (or commonAttributes) and must be treated as
// read-only.
type resolvedPath struct {
	// urn is the declared extension schema URN, or "" when the path targets
	// the core schema namespace (with or without an explicit core URN
	// prefix).
	urn string
	// attr is the resolved top-level attribute. It is nil only when the
	// path is a bare extension URN referring to the whole container.
	attr *Attribute
	// sub is the resolved sub-attribute for attrName.subAttr paths.
	sub *Attribute
	// common is true when attr came from the RFC 7643 §3.1 common
	// attributes rather than a schema.
	common bool
}

// resolvePath resolves a path in standard attribute notation against rt:
// attrName, attrName.subAttr, urn:attrName, urn:attrName.subAttr, or a bare
// extension URN. Matching is case-insensitive throughout; the returned
// attributes carry declared casing for wire emission.
func resolvePath(rt *ResourceType, path string) (resolvedPath, error) {
	if path == "" {
		return resolvedPath{}, ErrInvalidPath("empty path")
	}
	urn, attrExpr, isCore := splitURNPrefix(rt, path)
	if urn == "" && !isCore && strings.HasPrefix(foldName(path), "urn:") {
		return resolvedPath{}, ErrInvalidPath(
			fmt.Sprintf("path %q: schema URN not part of resource type %q", path, rt.Name),
		)
	}
	if attrExpr == "" {
		if urn == "" {
			return resolvedPath{}, ErrInvalidPath(
				fmt.Sprintf("path %q: core schema URN needs an attribute name", path),
			)
		}
		return resolvedPath{urn: urn}, nil
	}
	return resolveAttrExpr(rt, urn, attrExpr, path)
}

// splitURNPrefix matches path against the resource type's schema URNs
// (longest match wins) and returns the declared extension URN ("" for core
// or no prefix), the remaining attrName[.subAttr] expression, and whether
// the core URN was explicitly matched.
func splitURNPrefix(rt *ResourceType, path string) (urn, attrExpr string, isCore bool) {
	folded := foldName(path)
	if !strings.HasPrefix(folded, "urn:") {
		return "", path, false
	}
	best := ""
	for _, candidate := range append([]string{rt.Schema.ID}, extensionIDs(rt)...) {
		fc := foldName(candidate)
		if folded != fc && !strings.HasPrefix(folded, fc+":") {
			continue
		}
		if len(candidate) > len(best) {
			best = candidate
		}
	}
	if best == "" {
		return "", path, false
	}
	if len(path) > len(best) {
		attrExpr = path[len(best)+1:]
	}
	if foldName(best) == foldName(rt.Schema.ID) {
		return "", attrExpr, true
	}
	return best, attrExpr, false
}

func extensionIDs(rt *ResourceType) []string {
	ids := make([]string, len(rt.Extensions))
	for i, ext := range rt.Extensions {
		ids[i] = ext.Schema.ID
	}
	return ids
}

// resolveAttrExpr resolves attrName[.subAttr] within the namespace selected
// by urn ("" = core schema plus common attributes).
func resolveAttrExpr(rt *ResourceType, urn, attrExpr, path string) (resolvedPath, error) {
	attrName, subName, ok := splitAttrExpr(attrExpr)
	if !ok {
		return resolvedPath{}, ErrInvalidPath(fmt.Sprintf("path %q: malformed attribute expression", path))
	}
	rp := resolvedPath{urn: urn}
	if urn == "" {
		rp.attr = findAttribute(rt.Schema.Attributes, attrName)
		if rp.attr == nil {
			rp.attr = findAttribute(commonAttributes, attrName)
			rp.common = rp.attr != nil
		}
	} else if s := extensionSchema(rt, urn); s != nil {
		rp.attr = findAttribute(s.Attributes, attrName)
	}
	if rp.attr == nil {
		return resolvedPath{}, ErrInvalidPath(fmt.Sprintf("path %q: no attribute %q", path, attrName))
	}
	if subName == "" {
		return rp, nil
	}
	if rp.attr.Type != TypeComplex {
		return resolvedPath{}, ErrInvalidPath(
			fmt.Sprintf("path %q: attribute %q is not complex", path, rp.attr.Name),
		)
	}
	if rp.attr.MultiValued {
		return resolvedPath{}, ErrInvalidPath(fmt.Sprintf(
			"path %q: sub-attribute access on multi-valued %q requires a value filter", path, rp.attr.Name,
		))
	}
	rp.sub = findAttribute(rp.attr.SubAttributes, subName)
	if rp.sub == nil {
		return resolvedPath{}, ErrInvalidPath(
			fmt.Sprintf("path %q: %q has no sub-attribute %q", path, rp.attr.Name, subName),
		)
	}
	return rp, nil
}

// splitAttrExpr splits "attr" or "attr.sub" and rejects deeper nesting or
// empty segments.
func splitAttrExpr(expr string) (attr, sub string, ok bool) {
	attr, sub, found := strings.Cut(expr, ".")
	if attr == "" || (found && (sub == "" || strings.Contains(sub, "."))) {
		return "", "", false
	}
	return attr, sub, true
}

// extensionSchema returns the extension schema declared with the given URN,
// or nil when the resource type has no such extension.
func extensionSchema(rt *ResourceType, urn string) *Schema {
	folded := foldName(urn)
	for i := range rt.Extensions {
		if foldName(rt.Extensions[i].Schema.ID) == folded {
			return &rt.Extensions[i].Schema
		}
	}
	return nil
}
