package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

// validateResource enforces the schema's write-time constraints on a
// decoded resource: required attributes and required extensions always,
// canonicalValues only in strict mode — real IdPs routinely send
// non-canonical type labels, so tolerant profiles let them through.
func validateResource(res *scim.Resource, rt *scim.ResourceType, strict bool) error {
	if err := validateContainer(res, "", rt.Schema.Attributes, strict); err != nil {
		return err
	}
	for i := range rt.Extensions {
		ext := &rt.Extensions[i]
		if ext.Required && !slices.ContainsFunc(res.Schemas(), func(s string) bool {
			return scim.FoldName(s) == scim.FoldName(ext.Schema.ID)
		}) {
			return scim.ErrInvalidValue(fmt.Sprintf("schema extension %s is required", ext.Schema.ID))
		}
		if err := validateContainer(res, ext.Schema.ID+":", ext.Schema.Attributes, strict); err != nil {
			return err
		}
	}
	return nil
}

// validateContainer checks one schema namespace; prefix is the URN colon
// prefix for extension attribute paths, empty for the core schema.
func validateContainer(res *scim.Resource, prefix string, attrs []scim.Attribute, strict bool) error {
	for i := range attrs {
		attr := &attrs[i]
		v, ok := res.Get(prefix + attr.Name)
		if !ok {
			if attr.Required && attr.Mutability != scim.MutabilityReadOnly {
				return scim.ErrInvalidValue(fmt.Sprintf("attribute %s%s is required", prefix, attr.Name))
			}
			continue
		}
		if err := validateValue(attr, v, strict); err != nil {
			return err
		}
	}
	return nil
}

// validateValue checks one assigned value against its attribute:
// required sub-attributes inside complex elements, and canonicalValues
// in strict mode.
func validateValue(attr *scim.Attribute, v any, strict bool) error {
	switch t := v.(type) {
	case scim.ComplexValue:
		return validateElement(attr, t, strict)
	case scim.MultiComplexValue:
		for _, el := range t {
			if err := validateElement(attr, el, strict); err != nil {
				return err
			}
		}
		return nil
	case string:
		return checkCanonical(attr, t, strict)
	case []string:
		for _, s := range t {
			if err := checkCanonical(attr, s, strict); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func validateElement(attr *scim.Attribute, el scim.ComplexValue, strict bool) error {
	for i := range attr.SubAttributes {
		sub := &attr.SubAttributes[i]
		sv, ok := el[scim.FoldName(sub.Name)]
		if !ok {
			if sub.Required && sub.Mutability != scim.MutabilityReadOnly {
				return scim.ErrInvalidValue(fmt.Sprintf(
					"sub-attribute %s.%s is required", attr.Name, sub.Name))
			}
			continue
		}
		if s, isStr := sv.(string); isStr {
			if err := checkCanonical(sub, s, strict); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkCanonical rejects values outside the attribute's canonicalValues
// list in strict mode. Comparison is case-insensitive — the RFC declares
// canonical values, IdPs send them in arbitrary case, and rejecting only
// genuinely unknown labels is the useful strict behavior.
func checkCanonical(attr *scim.Attribute, s string, strict bool) error {
	if !strict || len(attr.CanonicalValues) == 0 {
		return nil
	}
	for _, c := range attr.CanonicalValues {
		if scim.FoldName(c) == scim.FoldName(s) {
			return nil
		}
	}
	return scim.ErrInvalidValue(fmt.Sprintf("attribute %s: %q is not a canonical value", attr.Name, s))
}

// mergePut applies the RFC 7644 §3.5.1 replace semantics: readOnly
// attributes are retained from the current resource (the client's copies
// were already dropped at decode), immutable attributes must match the
// current value or the request fails with mutability; an immutable
// attribute omitted from the request is retained rather than removed.
// The server-owned identity comes from the current resource: id, and
// meta with created retained, lastModified bumped, and version carried
// over as the store's compare-and-swap token.
func mergePut(incoming, current *scim.Resource, rt *scim.ResourceType) error {
	if err := mergePutAttrs(incoming, current, "", rt.Schema.Attributes); err != nil {
		return err
	}
	for i := range rt.Extensions {
		ext := &rt.Extensions[i].Schema
		if err := mergePutAttrs(incoming, current, ext.ID+":", ext.Attributes); err != nil {
			return err
		}
	}
	id, _ := current.Get("id")
	if err := incoming.Set("id", id); err != nil {
		return err
	}
	cur := current.Meta()
	incoming.SetMeta(scim.Meta{
		ResourceType: cur.ResourceType,
		Created:      cur.Created,
		LastModified: time.Now().UTC(),
		Location:     cur.Location,
		Version:      cur.Version,
	})
	return nil
}

func mergePutAttrs(incoming, current *scim.Resource, prefix string, attrs []scim.Attribute) error {
	for i := range attrs {
		attr := &attrs[i]
		path := prefix + attr.Name
		curV, curOK := current.Get(path)
		switch attr.Mutability {
		case scim.MutabilityReadOnly:
			if !curOK {
				continue
			}
			if err := incoming.Set(path, curV); err != nil {
				return err
			}
		case scim.MutabilityImmutable:
			if err := mergeImmutable(incoming, path, curV, curOK); err != nil {
				return err
			}
		case scim.MutabilityReadWrite, scim.MutabilityWriteOnly:
		}
	}
	return nil
}

// mergeImmutable enforces match-or-error on an assigned immutable
// attribute and retains it when the request omits it.
func mergeImmutable(incoming *scim.Resource, path string, curV any, curOK bool) error {
	if !curOK {
		return nil
	}
	inV, inOK := incoming.Get(path)
	if !inOK {
		return incoming.Set(path, curV)
	}
	if !jsonEqual(inV, curV) {
		return scim.ErrMutability(fmt.Sprintf("attribute %s is immutable and already assigned", path))
	}
	return nil
}

// jsonEqual compares two stored values by their canonical JSON encoding.
// Stored forms marshal deterministically (maps emit sorted keys), so
// equal values always produce equal bytes.
func jsonEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}
