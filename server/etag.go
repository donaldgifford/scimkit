package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/donaldgifford/scimkit/scim"
)

// resourceETag returns the entity tag for a resource: the store-supplied
// meta.version when present, otherwise a weak tag over the canonical
// unprojected JSON form. The hash is computed before the tag is mirrored
// into meta.version, so it is stable across reads.
func resourceETag(res *scim.Resource) (string, error) {
	if v := res.Meta().Version; v != "" {
		return v, nil
	}
	body, err := scim.EncodeResource(res, scim.Projection{})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return `W/"` + hex.EncodeToString(sum[:]) + `"`, nil
}

// stampETag mirrors the entity tag into meta.version (RFC 7644 §3.14: the
// ETag header and meta.version carry the same value) and returns it.
func stampETag(res *scim.Resource) (string, error) {
	tag, err := resourceETag(res)
	if err != nil {
		return "", err
	}
	m := res.Meta()
	m.Version = tag
	res.SetMeta(m)
	return tag, nil
}

// etagMatch implements the RFC 7232 §2.3.2 weak comparison: tags match
// when their opaque parts are equal, ignoring weakness prefixes. "*"
// matches any tag.
func etagMatch(header, tag string) bool {
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		if opaqueTag(candidate) == opaqueTag(tag) {
			return true
		}
	}
	return false
}

func opaqueTag(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "W/")
	return strings.Trim(tag, `"`)
}

// checkIfMatch enforces an If-Match precondition against the current
// resource. A missing header passes; a mismatch is the RFC 7644 §3.14
// 412 error.
func checkIfMatch(r *http.Request, cur *scim.Resource) error {
	header := r.Header.Get("If-Match")
	if header == "" {
		return nil
	}
	tag, err := resourceETag(cur)
	if err != nil {
		return err
	}
	if !etagMatch(header, tag) {
		return scim.ErrPreconditionFailed("resource version does not match If-Match")
	}
	return nil
}
