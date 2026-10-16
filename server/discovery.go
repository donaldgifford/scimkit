package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/donaldgifford/scimkit/scim"
)

// discovery holds the pre-marshaled RFC 7644 §4 documents. They are
// derived from the wiring once at New — discovery never lies about
// capabilities because it is generated from the same config that
// enables them.
type discovery struct {
	spc           json.RawMessage
	resourceTypes []json.RawMessage
	byName        map[string]json.RawMessage
	schemas       []json.RawMessage
	byURN         map[string]json.RawMessage
	logger        *slog.Logger
}

// supported/unsupported are the RFC 7643 §5 capability objects.
type spcSupported struct {
	Supported bool `json:"supported"`
}

type spcBulk struct {
	Supported      bool `json:"supported"`
	MaxOperations  int  `json:"maxOperations"`
	MaxPayloadSize int  `json:"maxPayloadSize"`
}

type spcFilter struct {
	Supported  bool `json:"supported"`
	MaxResults int  `json:"maxResults"`
}

type discoveryMeta struct {
	ResourceType string `json:"resourceType"`
	Location     string `json:"location"`
}

type spcDoc struct {
	Schemas               []string      `json:"schemas"`
	Patch                 spcSupported  `json:"patch"`
	Bulk                  spcBulk       `json:"bulk"`
	Filter                spcFilter     `json:"filter"`
	ChangePassword        spcSupported  `json:"changePassword"`
	Sort                  spcSupported  `json:"sort"`
	ETag                  spcSupported  `json:"etag"`
	AuthenticationSchemes []AuthScheme  `json:"authenticationSchemes"`
	Meta                  discoveryMeta `json:"meta"`
}

type schemaExtensionDoc struct {
	Schema   string `json:"schema"`
	Required bool   `json:"required"`
}

type resourceTypeDoc struct {
	Schemas          []string             `json:"schemas"`
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Description      string               `json:"description,omitempty"`
	Endpoint         string               `json:"endpoint"`
	Schema           string               `json:"schema"`
	SchemaExtensions []schemaExtensionDoc `json:"schemaExtensions,omitempty"`
	Meta             discoveryMeta        `json:"meta"`
}

type attributeDoc struct {
	Name            string         `json:"name"`
	Type            scim.AttrType  `json:"type"`
	SubAttributes   []attributeDoc `json:"subAttributes,omitempty"`
	MultiValued     bool           `json:"multiValued"`
	Description     string         `json:"description,omitempty"`
	Required        bool           `json:"required"`
	CanonicalValues []string       `json:"canonicalValues,omitempty"`
	CaseExact       bool           `json:"caseExact"`
	Mutability      string         `json:"mutability"`
	Returned        string         `json:"returned"`
	Uniqueness      string         `json:"uniqueness"`
	ReferenceTypes  []string       `json:"referenceTypes,omitempty"`
}

type schemaDoc struct {
	Schemas     []string       `json:"schemas"`
	ID          string         `json:"id"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Attributes  []attributeDoc `json:"attributes"`
	Meta        discoveryMeta  `json:"meta"`
}

// newDiscovery generates and marshals every discovery document from the
// validated config.
func newDiscovery(cfg *Config) (*discovery, error) {
	d := &discovery{
		resourceTypes: make([]json.RawMessage, 0, len(cfg.ResourceTypes)),
		byName:        make(map[string]json.RawMessage, len(cfg.ResourceTypes)),
		byURN:         make(map[string]json.RawMessage, len(cfg.ResourceTypes)),
		logger:        cfg.Logger,
	}
	if err := d.buildSPC(cfg); err != nil {
		return nil, err
	}
	for i := range cfg.ResourceTypes {
		if err := d.buildResourceType(cfg, &cfg.ResourceTypes[i].Type); err != nil {
			return nil, err
		}
	}
	if err := d.buildSchemas(cfg); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *discovery) buildSPC(cfg *Config) error {
	doc := spcDoc{
		Schemas:               []string{scim.SchemaServiceProviderConfig},
		Patch:                 spcSupported{Supported: true},
		Bulk:                  spcBulk{Supported: false},
		Filter:                spcFilter{Supported: true, MaxResults: cfg.MaxFilterResults},
		ChangePassword:        spcSupported{Supported: false},
		Sort:                  spcSupported{Supported: true},
		ETag:                  spcSupported{Supported: true},
		AuthenticationSchemes: []AuthScheme{},
		Meta: discoveryMeta{
			ResourceType: "ServiceProviderConfig",
			Location:     cfg.BaseURL + "/ServiceProviderConfig",
		},
	}
	if cfg.Auth != nil {
		doc.AuthenticationSchemes = []AuthScheme{cfg.Auth.Scheme()}
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling ServiceProviderConfig: %w", err)
	}
	d.spc = body
	return nil
}

func (d *discovery) buildResourceType(cfg *Config, rt *scim.ResourceType) error {
	doc := resourceTypeDoc{
		Schemas:     []string{scim.SchemaResourceType},
		ID:          rt.Name,
		Name:        rt.Name,
		Description: rt.Description,
		Endpoint:    rt.Endpoint,
		Schema:      rt.Schema.ID,
		Meta: discoveryMeta{
			ResourceType: "ResourceType",
			Location:     cfg.BaseURL + "/ResourceTypes/" + rt.Name,
		},
	}
	for i := range rt.Extensions {
		doc.SchemaExtensions = append(doc.SchemaExtensions, schemaExtensionDoc{
			Schema:   rt.Extensions[i].Schema.ID,
			Required: rt.Extensions[i].Required,
		})
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling ResourceType %q: %w", rt.Name, err)
	}
	d.resourceTypes = append(d.resourceTypes, body)
	d.byName[scim.FoldName(rt.Name)] = body
	return nil
}

// buildSchemas emits every distinct schema the registered types use —
// core schemas and extensions, deduplicated by URN.
func (d *discovery) buildSchemas(cfg *Config) error {
	for i := range cfg.ResourceTypes {
		rt := &cfg.ResourceTypes[i].Type
		if err := d.addSchema(cfg, &rt.Schema); err != nil {
			return err
		}
		for j := range rt.Extensions {
			if err := d.addSchema(cfg, &rt.Extensions[j].Schema); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *discovery) addSchema(cfg *Config, s *scim.Schema) error {
	key := scim.FoldName(s.ID)
	if _, seen := d.byURN[key]; seen {
		return nil
	}
	doc := schemaDoc{
		Schemas:     []string{scim.SchemaSchema},
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		Attributes:  attributeDocs(s.Attributes),
		Meta: discoveryMeta{
			ResourceType: "Schema",
			Location:     cfg.BaseURL + "/Schemas/" + s.ID,
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling schema %q: %w", s.ID, err)
	}
	d.schemas = append(d.schemas, body)
	d.byURN[key] = body
	return nil
}

func attributeDocs(attrs []scim.Attribute) []attributeDoc {
	out := make([]attributeDoc, 0, len(attrs))
	for i := range attrs {
		a := &attrs[i]
		out = append(out, attributeDoc{
			Name:            a.Name,
			Type:            a.Type,
			SubAttributes:   attributeDocs(a.SubAttributes),
			MultiValued:     a.MultiValued,
			Description:     a.Description,
			Required:        a.Required,
			CanonicalValues: a.CanonicalValues,
			CaseExact:       a.CaseExact,
			Mutability:      string(a.Mutability),
			Returned:        string(a.Returned),
			Uniqueness:      string(a.Uniqueness),
			ReferenceTypes:  a.ReferenceTypes,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// rejectFilter answers 403 for filtered discovery queries, the RFC 7644
// §4 rule that keeps discovery endpoints from becoming a query engine.
func (d *discovery) rejectFilter(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("filter") == "" {
		return false
	}
	writeError(w, d.logger, &scim.Error{
		Status: http.StatusForbidden,
		Detail: "filtering is not permitted on discovery endpoints",
	})
	return true
}

// handleSPC serves GET /ServiceProviderConfig.
func (d *discovery) handleSPC(w http.ResponseWriter, r *http.Request) {
	if d.rejectFilter(w, r) {
		return
	}
	writeBody(w, d.logger, http.StatusOK, d.spc)
}

// handleResourceTypes serves GET /ResourceTypes as a list response.
func (d *discovery) handleResourceTypes(w http.ResponseWriter, r *http.Request) {
	d.serveList(w, r, d.resourceTypes)
}

// handleResourceType serves GET /ResourceTypes/{name}.
func (d *discovery) handleResourceType(w http.ResponseWriter, r *http.Request) {
	d.serveDoc(w, r, d.byName, r.PathValue("name"), "resource type")
}

// handleSchemas serves GET /Schemas as a list response.
func (d *discovery) handleSchemas(w http.ResponseWriter, r *http.Request) {
	d.serveList(w, r, d.schemas)
}

// handleSchema serves GET /Schemas/{id}.
func (d *discovery) handleSchema(w http.ResponseWriter, r *http.Request) {
	d.serveDoc(w, r, d.byURN, r.PathValue("id"), "schema")
}

func (d *discovery) serveList(w http.ResponseWriter, r *http.Request, docs []json.RawMessage) {
	if d.rejectFilter(w, r) {
		return
	}
	writeJSON(w, d.logger, http.StatusOK, scim.ListResponse{
		TotalResults: len(docs),
		StartIndex:   1,
		ItemsPerPage: len(docs),
		Resources:    docs,
	})
}

func (d *discovery) serveDoc(
	w http.ResponseWriter, r *http.Request, index map[string]json.RawMessage, key, kind string,
) {
	if d.rejectFilter(w, r) {
		return
	}
	doc, ok := index[scim.FoldName(key)]
	if !ok {
		writeError(w, d.logger, scim.ErrNotFound(kind+" "+key+" not found"))
		return
	}
	writeBody(w, d.logger, http.StatusOK, doc)
}
