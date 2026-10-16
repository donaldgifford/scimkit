package scimtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
)

// Seed is a set of resources to preload into a mock at construction. Each
// entry is a bare SCIM resource body — the same shape a POST /Users or
// POST /Groups client sends — with no envelope. The mock replays every
// entry through its real create pipeline, so seeded resources get the same
// id, meta, and validation any provisioner would.
type Seed struct {
	Users  []json.RawMessage `json:"users,omitempty"`
	Groups []json.RawMessage `json:"groups,omitempty"`
}

// empty reports whether the seed would create nothing.
func (s Seed) empty() bool { return len(s.Users) == 0 && len(s.Groups) == 0 }

// DecodeSeed reads a seed document from r. It is the shared entry point for
// the CLI --seed flag and WithSeed callers building from JSON.
func DecodeSeed(r io.Reader) (Seed, error) {
	var s Seed
	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return Seed{}, fmt.Errorf("scimtest: decode seed: %w", err)
	}
	return s, nil
}

// LoadSeedFile reads and decodes a seed document from a file path.
func LoadSeedFile(path string) (Seed, error) {
	f, err := os.Open(path)
	if err != nil {
		return Seed{}, fmt.Errorf("scimtest: open seed file: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only file, close error is not actionable
	return DecodeSeed(f)
}

// applySeed replays every seed entry through the handler's create
// pipeline. It runs against the bare handler before the recorder wraps it,
// so setup traffic never pollutes recorded provisioner requests.
func applySeed(handler http.Handler, seed Seed, endpoints map[string]string, token string) error {
	for _, raw := range seed.Users {
		if err := seedOne(handler, endpoints[storeKey("User")], "User", raw, token); err != nil {
			return err
		}
	}
	for _, raw := range seed.Groups {
		if err := seedOne(handler, endpoints[storeKey("Group")], "Group", raw, token); err != nil {
			return err
		}
	}
	return nil
}

// seedOne POSTs one resource body through the handler and fails on any
// non-2xx response, surfacing the SCIM error detail.
func seedOne(handler http.Handler, endpoint, typeName string, body json.RawMessage, token string) error {
	if endpoint == "" {
		return fmt.Errorf("scimtest: cannot seed %s: no such resource type registered", typeName)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/scim+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code >= http.StatusMultipleChoices {
		return fmt.Errorf("scimtest: seed %s: status %d: %s",
			typeName, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	return nil
}
