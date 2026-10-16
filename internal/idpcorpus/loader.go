// Package idpcorpus loads the golden corpus of PATCH request shapes
// real identity providers send, authored from vendor documentation.
// The patch package's conformance tests replay it, and the mock-server
// simulator will emit it — the fixtures are embedded so a compiled
// binary carries the corpus without a source tree.
package idpcorpus

import (
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

//go:embed testdata/idp
var corpusFS embed.FS

// Fixture is one documented IdP request shape plus the behavior it must
// produce under each relevant compatibility profile.
type Fixture struct {
	// Provider is the fixture's directory name: entra, okta, onelogin.
	Provider string `json:"-"`
	// Name is the fixture file's basename without extension.
	Name string `json:"-"`
	// Source cites the vendor documentation the shape was authored
	// from. Every fixture must carry one.
	Source string `json:"_source"`
	// Description says what the shape exercises.
	Description string `json:"description"`
	// ResourceType names the target type: User or Group.
	ResourceType string `json:"resourceType"`
	// Request is the verbatim PatchOp message body.
	Request json.RawMessage `json:"request"`
	// Cases assert per-profile behavior for the same bytes.
	Cases []Case `json:"cases"`
}

// Case asserts one profile's handling of the fixture request.
type Case struct {
	// Profile names the compatibility profile: strict, default, entra,
	// okta.
	Profile string `json:"profile"`
	// Seed is the resource the ops apply to, as a SCIM resource
	// document. Empty means an empty resource.
	Seed json.RawMessage `json:"seed,omitempty"`
	// DecodeError, when set, expects Decode to fail with this scimType.
	DecodeError string `json:"decodeError,omitempty"`
	// ApplyError, when set, expects Apply to fail with this scimType.
	ApplyError string `json:"applyError,omitempty"`
	// WantChanged asserts Result.Changed when the ops apply cleanly.
	WantChanged *bool `json:"wantChanged,omitempty"`
	// Want asserts stored values by path after a clean apply.
	Want []PathAssertion `json:"want,omitempty"`
}

// PathAssertion checks one Resource.Get path after apply.
type PathAssertion struct {
	Path string `json:"path"`
	// Value is the expected stored value for scalar paths.
	Value any `json:"value,omitempty"`
	// Unassigned expects the path to have no value.
	Unassigned bool `json:"unassigned,omitempty"`
	// Len expects a multi-valued attribute with this many elements.
	Len *int `json:"len,omitempty"`
}

// All returns every fixture, ordered by provider then name.
func All() ([]Fixture, error) {
	var out []Fixture
	err := fs.WalkDir(corpusFS, "testdata/idp", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		data, err := corpusFS.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read fixture %s: %w", p, err)
		}
		var f Fixture
		if err := json.Unmarshal(data, &f); err != nil {
			return fmt.Errorf("parse fixture %s: %w", p, err)
		}
		f.Provider = path.Base(path.Dir(p))
		f.Name = strings.TrimSuffix(path.Base(p), ".json")
		out = append(out, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b Fixture) int {
		return cmp.Or(strings.Compare(a.Provider, b.Provider), strings.Compare(a.Name, b.Name))
	})
	return out, nil
}
