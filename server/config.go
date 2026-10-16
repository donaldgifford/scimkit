package server

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/donaldgifford/scimkit/patch"
	"github.com/donaldgifford/scimkit/scim"
)

// Registration binds one resource type to the store that holds its
// resources.
type Registration struct {
	Type  scim.ResourceType
	Store Store
}

// Config wires a SCIM service provider. BaseURL, ResourceTypes, and
// MaxFilterResults are required; everything else has a working default.
type Config struct {
	// BaseURL is the absolute external URL the handler is mounted at,
	// e.g. "https://app.example.com/scim/v2". It is used to generate
	// meta.location values; a trailing slash is trimmed.
	BaseURL string

	// ResourceTypes registers each served resource type with its store.
	ResourceTypes []Registration

	// Profile is the PATCH compatibility profile requests are decoded
	// and applied under. It doubles as the write-validation strictness
	// knob: the zero value (patch.Strict) enforces canonicalValues on
	// create and replace, every other profile tolerates non-canonical
	// values the way real IdPs send them.
	Profile patch.Profile

	// Auth guards every endpoint; nil disables the layer. See
	// ExemptServiceProviderConfig for the discovery carve-out.
	Auth Auth

	// ExemptServiceProviderConfig serves GET /ServiceProviderConfig
	// without authentication, the RFC 7644 §4 SHOULD that lets clients
	// discover capabilities before they hold credentials.
	ExemptServiceProviderConfig bool

	// MaxFilterResults caps the scan-and-match filter fallback and is
	// advertised as filter.maxResults in ServiceProviderConfig.
	// Required: a filtered list against a store without native filter
	// support fails with tooMany rather than scanning unboundedly.
	MaxFilterResults int

	// DefaultPageSize is the page size when a list request carries no
	// count parameter. Defaults to 100.
	DefaultPageSize int

	// MaxPageSize caps the count a list request may ask for. Larger
	// values are clamped, never rejected (RFC 7644 §3.4.2.4 lets the
	// service provider return fewer than requested; itemsPerPage reports
	// what was actually returned). Defaults to 200 or DefaultPageSize,
	// whichever is larger; an explicit value below DefaultPageSize is a
	// configuration error.
	MaxPageSize int

	// MaxBodyBytes bounds every request body the handler reads (create,
	// replace, patch, .search). A larger body is answered with 413 before
	// any decoding happens. Defaults to 1 MiB; negative is a
	// configuration error.
	MaxBodyBytes int64

	// Logger receives request and startup log lines. Defaults to
	// slog.Default().
	Logger *slog.Logger
}

// defaultPageSize applies when a list request has no count parameter and
// the config does not override it.
const defaultPageSize = 100

// defaultMaxPageSize caps count when the config does not override it.
const defaultMaxPageSize = 200

// defaultMaxBodyBytes bounds request bodies when the config does not
// override it: generous for any real SCIM resource, small enough that a
// hostile client cannot make the handler buffer gigabytes.
const defaultMaxBodyBytes = 1 << 20

// validate checks the config and applies defaults in place.
func (c *Config) validate() error {
	if c.BaseURL == "" {
		return errors.New("server: Config.BaseURL is required")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if len(c.ResourceTypes) == 0 {
		return errors.New("server: Config.ResourceTypes must register at least one resource type")
	}
	if c.MaxFilterResults <= 0 {
		return errors.New("server: Config.MaxFilterResults must be positive")
	}
	if c.DefaultPageSize == 0 {
		c.DefaultPageSize = defaultPageSize
	}
	if c.DefaultPageSize < 0 {
		return errors.New("server: Config.DefaultPageSize must not be negative")
	}
	if c.MaxPageSize == 0 {
		c.MaxPageSize = max(defaultMaxPageSize, c.DefaultPageSize)
	}
	if c.MaxPageSize < c.DefaultPageSize {
		return fmt.Errorf("server: Config.MaxPageSize (%d) must be at least DefaultPageSize (%d)",
			c.MaxPageSize, c.DefaultPageSize)
	}
	if c.MaxBodyBytes == 0 {
		c.MaxBodyBytes = defaultMaxBodyBytes
	}
	if c.MaxBodyBytes < 0 {
		return errors.New("server: Config.MaxBodyBytes must not be negative")
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c.validateRegistrations()
}

func (c *Config) validateRegistrations() error {
	seen := make(map[string]bool, len(c.ResourceTypes))
	for i := range c.ResourceTypes {
		reg := &c.ResourceTypes[i]
		if reg.Store == nil {
			return fmt.Errorf("server: resource type %q has no store", reg.Type.Name)
		}
		ep := reg.Type.Endpoint
		if ep == "" || !strings.HasPrefix(ep, "/") || strings.Count(ep, "/") != 1 {
			return fmt.Errorf("server: resource type %q endpoint %q must be a single path segment like \"/Users\"",
				reg.Type.Name, ep)
		}
		folded := scim.FoldName(ep)
		if seen[folded] {
			return fmt.Errorf("server: duplicate endpoint %q", ep)
		}
		seen[folded] = true
		if err := reg.Type.Schema.Validate(); err != nil {
			return fmt.Errorf("server: resource type %q: %w", reg.Type.Name, err)
		}
		for j := range reg.Type.Extensions {
			if err := reg.Type.Extensions[j].Schema.Validate(); err != nil {
				return fmt.Errorf("server: resource type %q extension: %w", reg.Type.Name, err)
			}
		}
	}
	return nil
}

// strict reports whether write validation runs in strict mode: the zero
// profile means no tolerance was asked for.
func (c *Config) strict() bool {
	return c.Profile == patch.Strict
}
