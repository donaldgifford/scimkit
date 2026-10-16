package scimtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
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
	// Resources seeds any registered resource type, keyed by its declared
	// name (case-insensitive). Users and Groups are sugar for the "User"
	// and "Group" keys and are merged with them.
	Resources map[string][]json.RawMessage `json:"resources,omitempty"`
}

// empty reports whether the seed would create nothing.
func (s Seed) empty() bool {
	if len(s.Users) > 0 || len(s.Groups) > 0 {
		return false
	}
	for _, docs := range s.Resources {
		if len(docs) > 0 {
			return false
		}
	}
	return true
}

// merge appends other's entries into s.
func (s *Seed) merge(other Seed) {
	s.Users = append(s.Users, other.Users...)
	s.Groups = append(s.Groups, other.Groups...)
	for name, docs := range other.Resources {
		if len(docs) == 0 {
			continue
		}
		if s.Resources == nil {
			s.Resources = make(map[string][]json.RawMessage)
		}
		s.Resources[name] = append(s.Resources[name], docs...)
	}
}

// byType flattens the seed into ordered (typeName, docs) batches: Users,
// then Groups, then Resources in sorted key order, so seeding is
// deterministic and groups can reference users created earlier.
func (s Seed) byType() []seedBatch {
	batches := make([]seedBatch, 0, 2+len(s.Resources))
	if len(s.Users) > 0 {
		batches = append(batches, seedBatch{typeName: "User", docs: s.Users})
	}
	if len(s.Groups) > 0 {
		batches = append(batches, seedBatch{typeName: "Group", docs: s.Groups})
	}
	for _, name := range slices.Sorted(maps.Keys(s.Resources)) {
		batches = append(batches, seedBatch{typeName: name, docs: s.Resources[name]})
	}
	return batches
}

// seedBatch is one resource type's worth of seed documents.
type seedBatch struct {
	typeName string
	docs     []json.RawMessage
}

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
	for _, batch := range seed.byType() {
		endpoint := endpoints[storeKey(batch.typeName)]
		for _, raw := range batch.docs {
			if err := seedOne(handler, endpoint, batch.typeName, raw, token); err != nil {
				return err
			}
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
