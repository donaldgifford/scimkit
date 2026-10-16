package scim

import (
	"crypto/rand"
	"fmt"
)

// NewID returns a version 4 UUID from crypto/rand, the service provider's
// resource id generator: ids are server-generated, stable, and never
// reassigned (RFC 7643 §3.1).
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating uuid: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// ValidateID rejects ids RFC 7643 §3.1 forbids: the empty string and the
// reserved keyword "bulkId". The keyword check folds case — id values are
// caseExact, but accepting "BULKID" would defeat the reservation's purpose
// (RFC 7644 §3.7 bulkId references).
func ValidateID(id string) error {
	if id == "" {
		return ErrInvalidValue("id must not be empty")
	}
	if foldName(id) == "bulkid" {
		return ErrInvalidValue(`id must not be the reserved keyword "bulkId"`)
	}
	return nil
}
