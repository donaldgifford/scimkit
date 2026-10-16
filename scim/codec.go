package scim

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DecodeOption configures DecodeResource.
type DecodeOption func(*decodeConfig)

type decodeConfig struct {
	dropReadOnly bool
}

// DropReadOnly makes DecodeResource silently discard readOnly attributes
// (id, meta, User's groups, …) as required when ingesting client-supplied
// resource bodies: RFC 7644 §3.3 treats client-supplied readOnly data as
// ignorable, never an error. Without this option every attribute is kept,
// which is what a client parsing a service-provider response needs.
func DropReadOnly() DecodeOption {
	return func(c *decodeConfig) { c.dropReadOnly = true }
}

// DecodeResource parses a SCIM resource body against the resource type.
// Keys match case-insensitively, top-level URN keys are treated as schema
// extension containers, JSON numbers decode per the attribute's integer or
// decimal type, and unknown attributes are ignored. The schemas attribute is
// ignored as data — the Resource derives it from content.
func DecodeResource(data []byte, rt *ResourceType, opts ...DecodeOption) (*Resource, error) {
	var cfg decodeConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, ErrInvalidSyntax(fmt.Sprintf("parse resource body: %v", err))
	}
	if dec.More() {
		return nil, ErrInvalidSyntax("trailing data after resource object")
	}
	r := NewResource(rt)
	for key, v := range raw {
		if err := decodeTopLevel(r, key, v, cfg); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func decodeTopLevel(r *Resource, key string, v any, cfg decodeConfig) error {
	folded := foldName(key)
	switch {
	case folded == attrSchemas:
		return nil
	case folded == attrMeta:
		if cfg.dropReadOnly || v == nil {
			return nil
		}
		return decodeMeta(r, v)
	case folded == "id" || folded == "externalid":
		return decodeAttr(r.attrs, commonAttributes, folded, v, cfg)
	case strings.HasPrefix(folded, "urn:"):
		return decodeExtension(r, key, v, cfg)
	default:
		return decodeAttr(r.attrs, r.rt.Schema.Attributes, key, v, cfg)
	}
}

// decodeAttr coerces one attribute value into the container, ignoring
// unknown names and (under DropReadOnly) readOnly attributes.
func decodeAttr(container map[string]any, attrs []Attribute, key string, v any, cfg decodeConfig) error {
	attr := findAttribute(attrs, key)
	if attr == nil {
		return nil
	}
	if cfg.dropReadOnly && attr.Mutability == MutabilityReadOnly {
		return nil
	}
	cv, err := coerceValue(attr, v)
	if err != nil {
		return ErrInvalidValue(fmt.Sprintf("attribute %q: %v", attr.Name, err))
	}
	if cv != nil {
		container[foldName(attr.Name)] = cv
	}
	return nil
}

// decodeExtension fills an extension container from a top-level URN key.
// URNs not declared on the resource type are ignored like any unknown
// attribute.
func decodeExtension(r *Resource, key string, v any, cfg decodeConfig) error {
	ext := extensionSchema(r.rt, key)
	if ext == nil || v == nil {
		return nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return ErrInvalidSyntax(fmt.Sprintf("extension %q: value must be an object", key))
	}
	container := make(map[string]any, len(obj))
	for name, sv := range obj {
		if err := decodeAttr(container, ext.Attributes, name, sv, cfg); err != nil {
			return err
		}
	}
	if len(container) > 0 {
		r.attrs[foldName(ext.ID)] = container
	}
	return nil
}

// decodeMeta parses the meta complex into the Meta struct, ignoring unknown
// sub-attributes.
func decodeMeta(r *Resource, v any) error {
	obj, ok := v.(map[string]any)
	if !ok {
		return ErrInvalidSyntax(`attribute "meta": value must be an object`)
	}
	var m Meta
	for key, sv := range obj {
		if sv == nil {
			continue
		}
		if err := decodeMetaField(&m, foldName(key), sv); err != nil {
			return err
		}
	}
	r.SetMeta(m)
	return nil
}

func decodeMetaField(m *Meta, folded string, v any) error {
	var err error
	switch folded {
	case metaResourceType:
		m.ResourceType, err = metaString(folded, v)
	case metaCreated:
		m.Created, err = metaTime(folded, v)
	case metaLastModified:
		m.LastModified, err = metaTime(folded, v)
	case metaLocation:
		m.Location, err = metaString(folded, v)
	case metaVersion:
		m.Version, err = metaString(folded, v)
	}
	return err
}

func metaString(name string, v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", ErrInvalidValue(fmt.Sprintf("meta.%s: expected string value, got %T", name, v))
	}
	return s, nil
}

func metaTime(name string, v any) (time.Time, error) {
	cv, err := coerceDateTime(v)
	if err != nil {
		return time.Time{}, ErrInvalidValue(fmt.Sprintf("meta.%s: %v", name, err))
	}
	t, ok := cv.(time.Time)
	if !ok {
		return time.Time{}, ErrInvalidValue(fmt.Sprintf("meta.%s: expected dateTime value, got %T", name, cv))
	}
	return t, nil
}

// Projection selects which attributes a serialization emits, per the
// attributes/excludedAttributes query parameters and the schema's returned
// characteristic (RFC 7644 §3.4.2.5, §3.9): returned=never is stripped
// unconditionally, returned=always ignores both lists, returned=request is
// emitted only when explicitly listed in Attributes, and paths that do not
// resolve against the resource type are ignored. The zero value is the
// default projection.
type Projection struct {
	// Attributes lists the paths to emit; empty means the default set.
	Attributes []string
	// ExcludedAttributes lists paths to drop from the default set. It is
	// consulted only when Attributes is empty, matching the query
	// parameters' mutual exclusivity.
	ExcludedAttributes []string
}

// attrSelection is the projection verdict for one attribute: emit or not,
// and optionally a folded sub-attribute allow- or drop-list for partial
// complex emission.
type attrSelection struct {
	include bool
	only    map[string]bool
	drop    map[string]bool
}

// projector indexes a Projection's paths by folded "urn|attr" key.
type projector struct {
	rt         *ResourceType
	attributes map[string]map[string]bool
	excluded   map[string]map[string]bool
}

func newProjector(rt *ResourceType, p Projection) *projector {
	return &projector{
		rt:         rt,
		attributes: indexPaths(rt, p.Attributes),
		excluded:   indexPaths(rt, p.ExcludedAttributes),
	}
}

// indexPaths resolves projection paths to folded attr key → folded sub set;
// a nil set means the whole attribute. A bare extension URN selects every
// attribute of that extension, so attributes=<urn> emits the whole
// container and the body matches the schemas list. Unresolvable paths are
// skipped.
func indexPaths(rt *ResourceType, paths []string) map[string]map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	out := make(map[string]map[string]bool, len(paths))
	for _, path := range paths {
		rp, err := resolvePath(rt, path)
		if err != nil {
			continue
		}
		if rp.attr == nil {
			indexExtension(out, rt, rp.urn)
			continue
		}
		key := projKey(rp.urn, rp.attr)
		if rp.sub == nil {
			out[key] = nil
			continue
		}
		subs, seen := out[key]
		if seen && subs == nil {
			continue // whole attribute already requested
		}
		if subs == nil {
			subs = make(map[string]bool)
			out[key] = subs
		}
		subs[foldName(rp.sub.Name)] = true
	}
	return out
}

// indexExtension marks every attribute of the extension declared with urn
// as whole-attribute selected.
func indexExtension(out map[string]map[string]bool, rt *ResourceType, urn string) {
	ext := extensionSchema(rt, urn)
	if ext == nil {
		return
	}
	for i := range ext.Attributes {
		out[projKey(urn, &ext.Attributes[i])] = nil
	}
}

func projKey(urn string, a *Attribute) string {
	return foldName(urn) + "|" + foldName(a.Name)
}

// selection applies the RFC 7644 §3.4.2.5 rules for one attribute in the
// namespace urn ("" = core and common attributes).
func (pr *projector) selection(urn string, a *Attribute) attrSelection {
	switch a.Returned {
	case ReturnedNever:
		return attrSelection{}
	case ReturnedAlways:
		return attrSelection{include: true}
	case ReturnedRequest, ReturnedDefault:
	}
	key := projKey(urn, a)
	if pr.attributes != nil {
		subs, ok := pr.attributes[key]
		return attrSelection{include: ok, only: subs}
	}
	if a.Returned == ReturnedRequest {
		return attrSelection{}
	}
	subs, ok := pr.excluded[key]
	if ok && subs == nil {
		return attrSelection{}
	}
	return attrSelection{include: true, drop: subs}
}

// keepSub reports whether a folded sub-attribute key survives the
// selection's partial-emission lists.
func (s attrSelection) keepSub(folded string) bool {
	if s.only != nil {
		return s.only[folded]
	}
	return !s.drop[folded]
}

// MarshalJSON emits the resource with the default projection.
func (r *Resource) MarshalJSON() ([]byte, error) {
	return EncodeResource(r, Projection{})
}

// EncodeResource serializes a resource with schema-declared attribute
// casing and a deterministic layout: schemas, id, externalId, core schema
// attributes in declaration order, extension containers in declaration
// order, then meta. Projection is applied as a serialization stage per the
// design's constraint 6 — it never mutates the resource.
func EncodeResource(r *Resource, p Projection) ([]byte, error) {
	pr := newProjector(r.rt, p)
	fields := []jsonField{{name: attrSchemas, value: r.Schemas()}}
	fields = appendCommonFields(fields, r, pr)
	fields = appendSchemaFields(fields, r.attrs, "", r.rt.Schema.Attributes, pr)
	fields = appendExtensionFields(fields, r, pr)
	if mv := encodeMeta(r.metaValue(), pr); mv != nil {
		fields = append(fields, jsonField{name: attrMeta, value: mv})
	}
	return marshalOrdered(fields)
}

type jsonField struct {
	name  string
	value any
}

func appendCommonFields(fields []jsonField, r *Resource, pr *projector) []jsonField {
	for _, name := range []string{nameID, nameExternalID} {
		attr := findAttribute(commonAttributes, name)
		sel := pr.selection("", attr)
		if !sel.include {
			continue
		}
		if v, ok := r.attrs[foldName(name)]; ok {
			fields = append(fields, jsonField{name: attr.Name, value: encodeValue(attr, v, sel)})
		}
	}
	return fields
}

// appendSchemaFields emits assigned attributes of one schema namespace in
// declaration order.
func appendSchemaFields(
	fields []jsonField, container map[string]any, urn string, attrs []Attribute, pr *projector,
) []jsonField {
	for i := range attrs {
		attr := &attrs[i]
		sel := pr.selection(urn, attr)
		if !sel.include {
			continue
		}
		v, ok := container[foldName(attr.Name)]
		if !ok {
			continue
		}
		ev := encodeValue(attr, v, sel)
		if ev == nil {
			continue
		}
		fields = append(fields, jsonField{name: attr.Name, value: ev})
	}
	return fields
}

func appendExtensionFields(fields []jsonField, r *Resource, pr *projector) []jsonField {
	for i := range r.rt.Extensions {
		ext := &r.rt.Extensions[i].Schema
		container, ok := r.attrs[foldName(ext.ID)].(map[string]any)
		if !ok {
			continue
		}
		extFields := appendSchemaFields(nil, container, ext.ID, ext.Attributes, pr)
		if len(extFields) == 0 {
			continue
		}
		obj := make(map[string]any, len(extFields))
		for _, f := range extFields {
			obj[f.name] = f.value
		}
		fields = append(fields, jsonField{name: ext.ID, value: obj})
	}
	return fields
}

// encodeValue converts a stored value to its wire form with declared
// sub-attribute casing, applying the selection's partial-emission lists to
// complex values. It returns nil when nothing survives.
func encodeValue(attr *Attribute, v any, sel attrSelection) any {
	switch t := v.(type) {
	case ComplexValue:
		return encodeComplex(attr, t, sel)
	case MultiComplexValue:
		out := make([]any, 0, len(t))
		for _, cv := range t {
			if ev := encodeComplex(attr, cv, sel); ev != nil {
				out = append(out, ev)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case time.Time:
		// RFC3339Nano keeps sub-second precision (trailing zeros trimmed,
		// so whole-second instants still render as plain RFC 3339) and
		// round-trips through the decoder, which accepts fractional
		// seconds. Plain RFC3339 silently truncated to the second.
		return t.Format(time.RFC3339Nano)
	case []time.Time:
		out := make([]string, len(t))
		for i, ts := range t {
			out[i] = ts.Format(time.RFC3339Nano)
		}
		return out
	case []byte:
		return base64.StdEncoding.EncodeToString(t)
	case [][]byte:
		out := make([]string, len(t))
		for i, b := range t {
			out[i] = base64.StdEncoding.EncodeToString(b)
		}
		return out
	default:
		// string, bool, int64, float64, ReferenceValue, and their slices
		// already marshal to the right JSON types.
		return v
	}
}

func encodeComplex(attr *Attribute, cv ComplexValue, sel attrSelection) any {
	obj := make(map[string]any, len(cv))
	for i := range attr.SubAttributes {
		sub := &attr.SubAttributes[i]
		folded := foldName(sub.Name)
		if sub.Returned == ReturnedNever || !sel.keepSub(folded) {
			continue
		}
		v, ok := cv[folded]
		if !ok {
			continue
		}
		obj[sub.Name] = encodeValue(sub, v, attrSelection{include: true})
	}
	if len(obj) == 0 {
		return nil
	}
	return obj
}

// encodeMeta projects the assigned Meta fields (as produced by
// Resource.metaValue) with declared casing, honoring projection of meta and
// its sub-attributes.
func encodeMeta(cv ComplexValue, pr *projector) any {
	if cv == nil {
		return nil
	}
	attr := findAttribute(commonAttributes, attrMeta)
	sel := pr.selection("", attr)
	if !sel.include {
		return nil
	}
	return encodeComplex(attr, cv, sel)
}

// marshalOrdered assembles a JSON object preserving field order. Nested
// objects are maps, which encoding/json emits with sorted keys, so output
// is fully deterministic.
func marshalOrdered(fields []jsonField) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		name, err := json.Marshal(f.name)
		if err != nil {
			return nil, fmt.Errorf("encoding field name %q: %w", f.name, err)
		}
		buf.Write(name)
		buf.WriteByte(':')
		value, err := json.Marshal(f.value)
		if err != nil {
			return nil, fmt.Errorf("encoding field %q: %w", f.name, err)
		}
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
