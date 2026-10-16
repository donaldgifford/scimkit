package scimtest

import (
	"fmt"
	"strings"

	"github.com/donaldgifford/scimkit/patch"
)

// ParseProfileName resolves a profile name to its patch.Profile. It lets
// the CLI accept a --profile flag without importing the patch package. The
// empty string and "strict" both map to the strict profile.
func ParseProfileName(name string) (patch.Profile, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "strict":
		return patch.Strict, nil
	case "default":
		return patch.Default, nil
	case "entra":
		return patch.Entra, nil
	case "okta":
		return patch.Okta, nil
	default:
		return patch.Profile{}, fmt.Errorf("scimtest: unknown profile %q (strict|default|entra|okta)", name)
	}
}
