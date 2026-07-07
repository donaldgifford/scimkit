package server

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// listRequest is a normalized list/query request: the store-facing Query
// plus the projection applied at serialization.
type listRequest struct {
	query      Query
	projection scim.Projection
}

// parseListParams normalizes the RFC 7644 §3.4.2 query parameters from
// a GET request's URL.
func parseListParams(values url.Values, defaultCount int) (listRequest, error) {
	raw := rawListParams{
		filter:     values.Get("filter"),
		sortBy:     values.Get("sortBy"),
		sortOrder:  values.Get("sortOrder"),
		startIndex: values.Get("startIndex"),
		count:      values.Get("count"),
		attributes: values.Get("attributes"),
		excluded:   values.Get("excludedAttributes"),
	}
	return raw.normalize(defaultCount)
}

// searchRequest is the urn:…:api:messages:2.0:SearchRequest body
// (RFC 7644 §3.4.3). Attribute lists arrive as JSON arrays instead of
// the query parameters' comma-separated strings.
type searchRequest struct {
	Attributes         []string `json:"attributes"`
	ExcludedAttributes []string `json:"excludedAttributes"`
	Filter             string   `json:"filter"`
	SortBy             string   `json:"sortBy"`
	SortOrder          string   `json:"sortOrder"`
	StartIndex         *int     `json:"startIndex"`
	Count              *int     `json:"count"`
}

// parseSearchBody normalizes a POST /.search body into the same shape as
// the GET query parameters.
func parseSearchBody(body []byte, defaultCount int) (listRequest, error) {
	var sr searchRequest
	if err := json.Unmarshal(body, &sr); err != nil {
		return listRequest{}, scim.ErrInvalidSyntax(fmt.Sprintf("parse search request: %v", err))
	}
	raw := rawListParams{
		filter:    sr.Filter,
		sortBy:    sr.SortBy,
		sortOrder: sr.SortOrder,
	}
	if sr.StartIndex != nil {
		raw.startIndex = strconv.Itoa(*sr.StartIndex)
	}
	if sr.Count != nil {
		raw.count = strconv.Itoa(*sr.Count)
	}
	if len(sr.Attributes) > 0 {
		raw.attributes = strings.Join(sr.Attributes, ",")
	}
	if len(sr.ExcludedAttributes) > 0 {
		raw.excluded = strings.Join(sr.ExcludedAttributes, ",")
	}
	return raw.normalize(defaultCount)
}

// rawListParams holds the string forms both transports reduce to.
type rawListParams struct {
	filter     string
	sortBy     string
	sortOrder  string
	startIndex string
	count      string
	attributes string
	excluded   string
}

func (raw *rawListParams) normalize(defaultCount int) (listRequest, error) {
	var lr listRequest
	var err error
	if lr.projection, err = parseProjection(raw.attributes, raw.excluded); err != nil {
		return listRequest{}, err
	}
	if raw.filter != "" {
		if lr.query.Filter, err = filter.Parse(raw.filter); err != nil {
			return listRequest{}, scim.ErrInvalidFilter(err.Error())
		}
	}
	if err := raw.normalizeSort(&lr); err != nil {
		return listRequest{}, err
	}
	if err := raw.normalizePagination(&lr, defaultCount); err != nil {
		return listRequest{}, err
	}
	return lr, nil
}

func (raw *rawListParams) normalizeSort(lr *listRequest) error {
	if raw.sortBy != "" {
		p, err := filter.ParsePath(raw.sortBy)
		if err != nil {
			return scim.ErrInvalidValue(fmt.Sprintf("sortBy: %v", err))
		}
		lr.query.SortBy = &p
	}
	switch strings.ToLower(raw.sortOrder) {
	case "", "ascending":
		lr.query.Ascending = true
	case "descending":
		lr.query.Ascending = false
	default:
		return scim.ErrInvalidValue(fmt.Sprintf("sortOrder: unknown value %q", raw.sortOrder))
	}
	return nil
}

// normalizePagination applies the §3.4.2.4 rules: startIndex below 1
// becomes 1, negative count becomes 0 (totals only), absent count means
// the default page size.
func (raw *rawListParams) normalizePagination(lr *listRequest, defaultCount int) error {
	lr.query.StartIndex = 1
	if raw.startIndex != "" {
		n, err := strconv.Atoi(raw.startIndex)
		if err != nil {
			return scim.ErrInvalidValue(fmt.Sprintf("startIndex: %v", err))
		}
		if n > 1 {
			lr.query.StartIndex = n
		}
	}
	lr.query.Count = defaultCount
	if raw.count != "" {
		n, err := strconv.Atoi(raw.count)
		if err != nil {
			return scim.ErrInvalidValue(fmt.Sprintf("count: %v", err))
		}
		lr.query.Count = max(n, 0)
	}
	return nil
}

// parseProjection builds the serialization projection from the two
// attribute-list parameters, enforcing their mutual exclusivity
// (RFC 7644 §3.4.2.5: clients MUST NOT combine them).
func parseProjection(attributes, excluded string) (scim.Projection, error) {
	if attributes != "" && excluded != "" {
		return scim.Projection{}, scim.ErrInvalidValue(
			"attributes and excludedAttributes are mutually exclusive")
	}
	return scim.Projection{
		Attributes:         splitAttrList(attributes),
		ExcludedAttributes: splitAttrList(excluded),
	}, nil
}

func splitAttrList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
