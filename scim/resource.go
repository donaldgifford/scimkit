package scim

import (
	"fmt"
	"slices"
	"time"
)

// Folded names of the common attributes that Resource handles specially:
// schemas is derived from content and meta lives in the Meta struct.
const (
	attrSchemas = "schemas"
	attrMeta    = "meta"
)

// Meta is the resource metadata complex attribute (RFC 7643 §3.1). Zero
// fields are unassigned.
type Meta struct {
	// ResourceType names the resource's ResourceType, e.g. "User".
	ResourceType string
	// Created is when the resource was added to the service provider.
	Created time.Time
	// LastModified is when the resource was last updated.
	LastModified time.Time
	// Location is the URI of the resource.
	Location string
	// Version is the resource's weak ETag value.
	Version string
}

// Resource is the canonical representation of a SCIM resource: a
// schema-aware, case-insensitive, presence-tracking attribute container.
// An attribute is either assigned (present in the container) or unassigned;
// JSON null, empty arrays, and absent keys all canonicalize to unassigned
// (RFC 7643 §2.5). The zero value is not usable — construct with
// NewResource.
//
// Values returned by Get share backing storage with the resource; callers
// that need an independent copy should Clone the resource first. Resource
// is not safe for concurrent mutation.
type Resource struct {
	rt *ResourceType
	// attrs maps folded attribute names (core schema + id/externalId) to
	// stored values, and folded extension URNs to map[string]any containers
	// of the same shape. Empty containers are never stored.
	attrs map[string]any
	meta  Meta
}

// NewResource returns an empty resource of the given type. The
// ResourceType is retained by reference and must not be mutated afterwards.
func NewResource(rt *ResourceType) *Resource {
	return &Resource{rt: rt, attrs: make(map[string]any)}
}

// ResourceType returns the type this resource was constructed with. The
// returned value is shared and must be treated as read-only.
func (r *Resource) ResourceType() *ResourceType {
	return r.rt
}

// Schemas returns the resource's schemas attribute: the core schema URN
// followed by the URN of each extension that currently holds data, in
// declaration order. It is derived from the container contents, so it never
// drifts from the data.
func (r *Resource) Schemas() []string {
	out := []string{r.rt.Schema.ID}
	for _, ext := range r.rt.Extensions {
		if _, ok := r.attrs[foldName(ext.Schema.ID)]; ok {
			out = append(out, ext.Schema.ID)
		}
	}
	return out
}

// Meta returns the resource metadata.
func (r *Resource) Meta() Meta {
	return r.meta
}

// SetMeta replaces the resource metadata. Meta is readOnly on the wire;
// this is the service-provider-side entry point for stamping it.
//
//nolint:gocritic // Meta is passed by value for setter API symmetry with Meta().
func (r *Resource) SetMeta(m Meta) {
	r.meta = m
}

// Get returns the stored value at path and whether the attribute is
// assigned. Paths use standard attribute notation, matched
// case-insensitively. Unresolvable paths report unassigned.
func (r *Resource) Get(path string) (any, bool) {
	rp, err := resolvePath(r.rt, path)
	if err != nil {
		return nil, false
	}
	if rp.common {
		return r.getCommon(rp)
	}
	container := r.attrs
	if rp.urn != "" {
		ext, ok := r.attrs[foldName(rp.urn)].(map[string]any)
		if !ok {
			return nil, false
		}
		if rp.attr == nil {
			return ext, true
		}
		container = ext
	}
	v, ok := container[foldName(rp.attr.Name)]
	if !ok {
		return nil, false
	}
	return subValue(v, rp.sub)
}

// getCommon serves the RFC 7643 §3.1 common attributes: id and externalId
// from the container, schemas derived, meta projected from the Meta struct.
func (r *Resource) getCommon(rp resolvedPath) (any, bool) {
	switch foldName(rp.attr.Name) {
	case attrSchemas:
		return r.Schemas(), true
	case attrMeta:
		mv := r.metaValue()
		if mv == nil {
			return nil, false
		}
		return subValue(mv, rp.sub)
	default:
		v, ok := r.attrs[foldName(rp.attr.Name)]
		return v, ok
	}
}

// subValue projects a resolved sub-attribute out of a stored complex value.
func subValue(v any, sub *Attribute) (any, bool) {
	if sub == nil {
		return v, true
	}
	cv, ok := v.(ComplexValue)
	if !ok {
		return nil, false
	}
	sv, ok := cv[foldName(sub.Name)]
	return sv, ok
}

// metaValue projects the assigned Meta fields as a ComplexValue, or nil
// when every field is zero.
func (r *Resource) metaValue() ComplexValue {
	cv := make(ComplexValue)
	if r.meta.ResourceType != "" {
		cv["resourcetype"] = r.meta.ResourceType
	}
	if !r.meta.Created.IsZero() {
		cv["created"] = r.meta.Created
	}
	if !r.meta.LastModified.IsZero() {
		cv["lastmodified"] = r.meta.LastModified
	}
	if r.meta.Location != "" {
		cv["location"] = r.meta.Location
	}
	if r.meta.Version != "" {
		cv["version"] = r.meta.Version
	}
	if len(cv) == 0 {
		return nil
	}
	return cv
}

// Set writes a schema-validated value at path. Values are coerced to the
// attribute's SCIM type (see the stored-form table in value.go); nil and
// empty multi-values unassign, preserving null/[]/absent equivalence. Set
// validates type and shape only — mutability rules are a protocol concern
// enforced by the decode and PATCH/PUT pipelines, so service providers can
// Set readOnly attributes like id directly.
func (r *Resource) Set(path string, v any) error {
	rp, err := resolvePath(r.rt, path)
	if err != nil {
		return err
	}
	switch {
	case rp.common:
		return r.setCommon(rp, path, v)
	case rp.urn != "":
		return r.setExtension(rp, path, v)
	default:
		return setInContainer(r.attrs, rp, path, v)
	}
}

func (r *Resource) setCommon(rp resolvedPath, path string, v any) error {
	switch foldName(rp.attr.Name) {
	case attrSchemas:
		return ErrMutability(`"schemas" is derived from resource content and cannot be set`)
	case attrMeta:
		return ErrMutability(`"meta" is set via SetMeta`)
	default:
		return setInContainer(r.attrs, rp, path, v)
	}
}

// setExtension writes into an extension's container, creating or removing
// the container (and thereby its schemas entry) as it fills or empties.
func (r *Resource) setExtension(rp resolvedPath, path string, v any) error {
	key := foldName(rp.urn)
	if rp.attr == nil {
		return r.setExtensionContainer(rp, path, key, v)
	}
	container, ok := r.attrs[key].(map[string]any)
	if !ok {
		container = make(map[string]any)
	}
	if err := setInContainer(container, rp, path, v); err != nil {
		return err
	}
	if len(container) == 0 {
		delete(r.attrs, key)
	} else {
		r.attrs[key] = container
	}
	return nil
}

// setExtensionContainer replaces a whole extension container from a map
// value, coercing each entry against the extension schema.
func (r *Resource) setExtensionContainer(rp resolvedPath, path, key string, v any) error {
	if v == nil {
		delete(r.attrs, key)
		return nil
	}
	src, ok := toStringMap(v)
	if !ok {
		return ErrInvalidValue(fmt.Sprintf("path %q: extension value must be an object", path))
	}
	ext := extensionSchema(r.rt, rp.urn)
	container := make(map[string]any, len(src))
	for name, sv := range src {
		attr := findAttribute(ext.Attributes, name)
		if attr == nil {
			continue
		}
		cv, err := coerceValue(attr, sv)
		if err != nil {
			return ErrInvalidValue(fmt.Sprintf("path %q: attribute %q: %v", path, attr.Name, err))
		}
		if cv != nil {
			container[foldName(attr.Name)] = cv
		}
	}
	if len(container) == 0 {
		delete(r.attrs, key)
	} else {
		r.attrs[key] = container
	}
	return nil
}

func toStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case ComplexValue:
		return m, true
	default:
		return nil, false
	}
}

// setInContainer coerces and stores v at the resolved attribute (or
// sub-attribute) inside container, deleting entries that canonicalize to
// unassigned so empty values are never stored.
func setInContainer(container map[string]any, rp resolvedPath, path string, v any) error {
	if rp.sub != nil {
		return setSub(container, rp, path, v)
	}
	cv, err := coerceValue(rp.attr, v)
	if err != nil {
		return ErrInvalidValue(fmt.Sprintf("path %q: %v", path, err))
	}
	key := foldName(rp.attr.Name)
	if cv == nil {
		delete(container, key)
		return nil
	}
	container[key] = cv
	return nil
}

// setSub writes one sub-attribute of a single-valued complex attribute,
// deleting the parent when it empties.
func setSub(container map[string]any, rp resolvedPath, path string, v any) error {
	cv, err := coerceValue(rp.sub, v)
	if err != nil {
		return ErrInvalidValue(fmt.Sprintf("path %q: %v", path, err))
	}
	key := foldName(rp.attr.Name)
	parent, ok := container[key].(ComplexValue)
	subKey := foldName(rp.sub.Name)
	if cv == nil {
		if !ok {
			return nil
		}
		delete(parent, subKey)
		if len(parent) == 0 {
			delete(container, key)
		}
		return nil
	}
	if !ok {
		parent = make(ComplexValue)
	}
	parent[subKey] = cv
	container[key] = parent
	return nil
}

// Unassign removes the value at path. Unassigning an already-unassigned
// attribute is a no-op; the error cases are unresolvable paths and the
// derived schemas/meta attributes.
func (r *Resource) Unassign(path string) error {
	return r.Set(path, nil)
}

// Clone returns a deep copy sharing only the (immutable) ResourceType.
func (r *Resource) Clone() *Resource {
	return &Resource{rt: r.rt, attrs: cloneStringMap(r.attrs), meta: r.meta}
}

func cloneStringMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

// cloneValue deep-copies a stored value. The closed set of stored forms
// (see value.go) makes the type switch exhaustive.
func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneStringMap(t)
	case ComplexValue:
		return ComplexValue(cloneStringMap(t))
	case MultiComplexValue:
		out := make(MultiComplexValue, len(t))
		for i, cv := range t {
			out[i] = ComplexValue(cloneStringMap(cv))
		}
		return out
	case []byte:
		return slices.Clone(t)
	case []string:
		return slices.Clone(t)
	case []bool:
		return slices.Clone(t)
	case []int64:
		return slices.Clone(t)
	case []float64:
		return slices.Clone(t)
	case []time.Time:
		return slices.Clone(t)
	case []ReferenceValue:
		return slices.Clone(t)
	case [][]byte:
		out := make([][]byte, len(t))
		for i, b := range t {
			out[i] = slices.Clone(b)
		}
		return out
	default:
		// Scalars (string, bool, int64, float64, time.Time,
		// ReferenceValue) are immutable values.
		return v
	}
}
