package scim

// AttrType is a SCIM attribute data type (RFC 7643 §2.3). The zero value is
// invalid; schema sources that omit "type" default to TypeString per §2.2,
// applied by ParseSchema / Schema.WithDefaults.
type AttrType string

// SCIM attribute data types (RFC 7643 §2.3).
const (
	TypeString    AttrType = "string"
	TypeBoolean   AttrType = "boolean"
	TypeDecimal   AttrType = "decimal"
	TypeInteger   AttrType = "integer"
	TypeDateTime  AttrType = "dateTime"
	TypeBinary    AttrType = "binary"
	TypeReference AttrType = "reference"
	TypeComplex   AttrType = "complex"
)

func (t AttrType) valid() bool {
	switch t {
	case TypeString, TypeBoolean, TypeDecimal, TypeInteger,
		TypeDateTime, TypeBinary, TypeReference, TypeComplex:
		return true
	default:
		return false
	}
}

// Mutability describes when an attribute may be written (RFC 7643 §7). The
// zero value is invalid; absent values default to MutabilityReadWrite per
// §2.2, applied by ParseSchema / Schema.WithDefaults.
type Mutability string

// SCIM mutability values (RFC 7643 §7).
const (
	MutabilityReadOnly  Mutability = "readOnly"
	MutabilityReadWrite Mutability = "readWrite"
	MutabilityImmutable Mutability = "immutable"
	MutabilityWriteOnly Mutability = "writeOnly"
)

func (m Mutability) valid() bool {
	switch m {
	case MutabilityReadOnly, MutabilityReadWrite, MutabilityImmutable, MutabilityWriteOnly:
		return true
	default:
		return false
	}
}

// Returned describes when an attribute appears in responses (RFC 7643 §7).
// The zero value is invalid; absent values default to ReturnedDefault per
// §2.2, applied by ParseSchema / Schema.WithDefaults.
type Returned string

// SCIM returned values (RFC 7643 §7).
const (
	ReturnedAlways  Returned = "always"
	ReturnedNever   Returned = "never"
	ReturnedDefault Returned = "default"
	ReturnedRequest Returned = "request"
)

func (r Returned) valid() bool {
	switch r {
	case ReturnedAlways, ReturnedNever, ReturnedDefault, ReturnedRequest:
		return true
	default:
		return false
	}
}

// Uniqueness describes how attribute values must be unique (RFC 7643 §7).
// The zero value is invalid; absent values default to UniquenessNone per
// §2.2, applied by ParseSchema / Schema.WithDefaults.
type Uniqueness string

// SCIM uniqueness values (RFC 7643 §7).
const (
	UniquenessNone   Uniqueness = "none"
	UniquenessServer Uniqueness = "server"
	UniquenessGlobal Uniqueness = "global"
)

func (u Uniqueness) valid() bool {
	switch u {
	case UniquenessNone, UniquenessServer, UniquenessGlobal:
		return true
	default:
		return false
	}
}
