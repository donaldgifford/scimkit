package scim

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Attribute describes a single SCIM attribute: its type and the seven
// characteristics of RFC 7643 §2.2/§7. Values produced by ParseSchema or
// Schema.WithDefaults always carry explicit characteristics (absent source
// fields are filled with the §2.2 defaults).
type Attribute struct {
	Name            string
	Type            AttrType
	SubAttributes   []Attribute
	MultiValued     bool
	Description     string
	Required        bool
	CanonicalValues []string
	CaseExact       bool
	Mutability      Mutability
	Returned        Returned
	Uniqueness      Uniqueness
	ReferenceTypes  []string
}

// clone returns a deep copy of the attribute.
func (a *Attribute) clone() Attribute {
	c := *a
	c.CanonicalValues = slices.Clone(a.CanonicalValues)
	c.ReferenceTypes = slices.Clone(a.ReferenceTypes)
	if a.SubAttributes != nil {
		c.SubAttributes = make([]Attribute, len(a.SubAttributes))
		for i := range a.SubAttributes {
			c.SubAttributes[i] = a.SubAttributes[i].clone()
		}
	}
	return c
}

// withDefaults fills absent characteristics with the RFC 7643 §2.2 defaults,
// canonicalizes empty slices to nil (absent and empty are equivalent), and
// recurses into sub-attributes. It mutates the receiver in place.
func (a *Attribute) withDefaults() {
	if a.Type == "" {
		a.Type = TypeString
	}
	if len(a.CanonicalValues) == 0 {
		a.CanonicalValues = nil
	}
	if len(a.ReferenceTypes) == 0 {
		a.ReferenceTypes = nil
	}
	if len(a.SubAttributes) == 0 {
		a.SubAttributes = nil
	}
	if a.Mutability == "" {
		a.Mutability = MutabilityReadWrite
	}
	if a.Returned == "" {
		a.Returned = ReturnedDefault
	}
	if a.Uniqueness == "" {
		a.Uniqueness = UniquenessNone
	}
	for i := range a.SubAttributes {
		a.SubAttributes[i].withDefaults()
	}
}

// validate reports the first invalid characteristic, if any. path is the
// dotted attribute path used in error messages.
func (a *Attribute) validate(path string) error {
	if a.Name == "" {
		return fmt.Errorf("attribute %q: missing name", path)
	}
	if !a.Type.valid() {
		return fmt.Errorf("attribute %q: invalid type %q", path, a.Type)
	}
	if !a.Mutability.valid() {
		return fmt.Errorf("attribute %q: invalid mutability %q", path, a.Mutability)
	}
	if !a.Returned.valid() {
		return fmt.Errorf("attribute %q: invalid returned %q", path, a.Returned)
	}
	if !a.Uniqueness.valid() {
		return fmt.Errorf("attribute %q: invalid uniqueness %q", path, a.Uniqueness)
	}
	if len(a.SubAttributes) > 0 && a.Type != TypeComplex {
		return fmt.Errorf("attribute %q: subAttributes on non-complex type %q", path, a.Type)
	}
	for i := range a.SubAttributes {
		sub := &a.SubAttributes[i]
		if err := sub.validate(path + "." + sub.Name); err != nil {
			return err
		}
	}
	return nil
}

// Schema is a SCIM schema definition (RFC 7643 §7): a URN identifier plus
// attribute metadata. It round-trips to the /Schemas wire representation via
// ParseSchema and MarshalJSON.
type Schema struct {
	ID          string
	Name        string
	Description string
	Attributes  []Attribute
}

// WithDefaults returns a deep copy of the schema with the RFC 7643 §2.2
// defaults applied to every attribute whose characteristics are absent.
// Hand-authored schemas can therefore stay terse.
func (s Schema) WithDefaults() Schema {
	c := s
	c.Attributes = make([]Attribute, len(s.Attributes))
	for i := range s.Attributes {
		c.Attributes[i] = s.Attributes[i].clone()
		c.Attributes[i].withDefaults()
	}
	return c
}

// Validate checks that every attribute carries a name and valid
// characteristic values. Call after WithDefaults (or ParseSchema, which
// validates internally).
func (s Schema) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("schema: missing id")
	}
	seen := make(map[string]struct{}, len(s.Attributes))
	for i := range s.Attributes {
		a := &s.Attributes[i]
		if err := a.validate(a.Name); err != nil {
			return fmt.Errorf("schema %q: %w", s.ID, err)
		}
		key := foldName(a.Name)
		if _, dup := seen[key]; dup {
			return fmt.Errorf("schema %q: duplicate attribute %q", s.ID, a.Name)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// attributeJSON is the RFC 7643 §7 wire shape of an attribute definition.
type attributeJSON struct {
	Name            string          `json:"name"`
	Type            AttrType        `json:"type"`
	SubAttributes   []attributeJSON `json:"subAttributes,omitempty"`
	MultiValued     bool            `json:"multiValued"`
	Description     string          `json:"description,omitempty"`
	Required        bool            `json:"required"`
	CanonicalValues []string        `json:"canonicalValues,omitempty"`
	CaseExact       bool            `json:"caseExact"`
	Mutability      Mutability      `json:"mutability"`
	Returned        Returned        `json:"returned"`
	Uniqueness      Uniqueness      `json:"uniqueness"`
	ReferenceTypes  []string        `json:"referenceTypes,omitempty"`
}

// schemaJSON is the RFC 7643 §7 wire shape of a schema definition. The
// optional meta block present in some published schema documents is ignored:
// servers attach meta at serve time.
type schemaJSON struct {
	ID          string          `json:"id"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Attributes  []attributeJSON `json:"attributes"`
}

func (a *Attribute) toJSON() attributeJSON {
	j := attributeJSON{
		Name:            a.Name,
		Type:            a.Type,
		MultiValued:     a.MultiValued,
		Description:     a.Description,
		Required:        a.Required,
		CanonicalValues: a.CanonicalValues,
		CaseExact:       a.CaseExact,
		Mutability:      a.Mutability,
		Returned:        a.Returned,
		Uniqueness:      a.Uniqueness,
		ReferenceTypes:  a.ReferenceTypes,
	}
	if len(a.SubAttributes) > 0 {
		j.SubAttributes = make([]attributeJSON, len(a.SubAttributes))
		for i := range a.SubAttributes {
			j.SubAttributes[i] = a.SubAttributes[i].toJSON()
		}
	}
	return j
}

func (j *attributeJSON) toAttribute() Attribute {
	a := Attribute{
		Name:            j.Name,
		Type:            j.Type,
		MultiValued:     j.MultiValued,
		Description:     j.Description,
		Required:        j.Required,
		CanonicalValues: j.CanonicalValues,
		CaseExact:       j.CaseExact,
		Mutability:      j.Mutability,
		Returned:        j.Returned,
		Uniqueness:      j.Uniqueness,
		ReferenceTypes:  j.ReferenceTypes,
	}
	if len(j.SubAttributes) > 0 {
		a.SubAttributes = make([]Attribute, len(j.SubAttributes))
		for i := range j.SubAttributes {
			a.SubAttributes[i] = j.SubAttributes[i].toAttribute()
		}
	}
	return a
}

// MarshalJSON renders the schema in its RFC 7643 §7 wire representation with
// all attribute characteristics explicit (the normalized form; omitted and
// explicit-default characteristics are equivalent per §2.2).
func (s Schema) MarshalJSON() ([]byte, error) {
	j := schemaJSON{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		Attributes:  make([]attributeJSON, len(s.Attributes)),
	}
	for i := range s.Attributes {
		j.Attributes[i] = s.Attributes[i].toJSON()
	}
	return json.Marshal(j)
}

// ParseSchema decodes an RFC 7643 schema document (the /Schemas wire form),
// applies the §2.2 defaults to absent characteristics, and validates the
// result. Unknown top-level fields (such as meta) are ignored.
func ParseSchema(data []byte) (Schema, error) {
	var j schemaJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return Schema{}, fmt.Errorf("parse schema: %w", err)
	}
	s := Schema{
		ID:          j.ID,
		Name:        j.Name,
		Description: j.Description,
		Attributes:  make([]Attribute, len(j.Attributes)),
	}
	for i := range j.Attributes {
		s.Attributes[i] = j.Attributes[i].toAttribute()
	}
	s = s.WithDefaults()
	if err := s.Validate(); err != nil {
		return Schema{}, err
	}
	return s, nil
}

// SchemaExtension names an extension schema attached to a resource type
// (RFC 7643 §6).
type SchemaExtension struct {
	Schema   Schema
	Required bool
}

// ResourceType binds an endpoint name to a base schema and its extensions
// (RFC 7643 §6).
type ResourceType struct {
	Name        string
	Description string
	Endpoint    string
	Schema      Schema
	Extensions  []SchemaExtension
}
