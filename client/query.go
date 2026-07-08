package client

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// defaultPageSize is the page size List requests when Query.Count is
// unset, matching the server's own default.
const defaultPageSize = 100

// Query is a list request. The zero value lists everything in pages of
// 100 from the start. Attributes and ExcludedAttributes are mutually
// exclusive (the server enforces this); Search forces the PII-safe
// POST /.search transport for this call regardless of the client-wide
// default.
type Query struct {
	Filter             filter.Expr
	SortBy             *filter.Path
	Ascending          bool
	StartIndex         int
	Count              int
	Attributes         []string
	ExcludedAttributes []string
	Search             bool
}

// List returns an iterator over every resource matching the query,
// following pagination transparently. It tolerates the RFC's
// non-snapshot pagination — duplicates or gaps are possible across pages
// under concurrent mutation and are not corrected. On any error the
// iterator yields it once, then stops.
//
//nolint:gocritic // Query is passed by value: List/Lookup ergonomic public API
func (s *ResourceService) List(ctx context.Context, q Query) iter.Seq2[*scim.Resource, error] {
	return func(yield func(*scim.Resource, error) bool) {
		rt, err := s.resolver.resourceType(ctx)
		if err != nil {
			yield(nil, err)
			return
		}
		start := max(q.StartIndex, 1)
		count := q.Count
		if count <= 0 {
			count = defaultPageSize
		}
		for {
			page, err := s.fetchPage(ctx, rt, &q, start, count)
			if err != nil {
				yield(nil, err)
				return
			}
			for _, raw := range page.Resources {
				res, derr := scim.DecodeResource(raw, rt)
				if derr != nil {
					yield(nil, fmt.Errorf("scimkit client: decoding list item: %w", derr))
					return
				}
				if !yield(res, nil) {
					return
				}
			}
			// Terminate on an empty page — the definitive "no more"
			// signal — or once the cursor passes a reported total. Do
			// not stop merely because a page was shorter than the
			// requested count: RFC 7644 servers may cap page size below
			// the request (Capabilities.MaxFilterResults), which would
			// truncate the walk after the first page.
			if len(page.Resources) == 0 {
				return
			}
			start += len(page.Resources)
			if page.TotalResults > 0 && start > page.TotalResults {
				return
			}
		}
	}
}

// Lookup returns the single resource matching the filter, nil when none
// match, or ErrAmbiguousLookup when more than one does. An empty result
// is success, not an error (the Okta/OneLogin lookup-then-create flow).
func (s *ResourceService) Lookup(ctx context.Context, f filter.Expr) (*scim.Resource, error) {
	var found *scim.Resource
	n := 0
	for res, err := range s.List(ctx, Query{Filter: f, Count: 2}) {
		if err != nil {
			return nil, err
		}
		n++
		if n > 1 {
			return nil, ErrAmbiguousLookup
		}
		found = res
	}
	return found, nil
}

// fetchPage retrieves one page via GET or POST /.search.
func (s *ResourceService) fetchPage(
	ctx context.Context, rt *scim.ResourceType, q *Query, start, count int,
) (scim.ListResponse, error) {
	if q.Search || s.client.search {
		return s.searchPage(ctx, rt, q, start, count)
	}
	return s.getPage(ctx, rt, q, start, count)
}

func (s *ResourceService) getPage(
	ctx context.Context, rt *scim.ResourceType, q *Query, start, count int,
) (scim.ListResponse, error) {
	rawURL := s.client.requestURL(rt.Endpoint) + "?" + q.params(start, count).Encode()
	resp, err := s.client.do(ctx, http.MethodGet, rawURL, nil, nil)
	if err != nil {
		return scim.ListResponse{}, err
	}
	return decodeListResponse(resp)
}

func (s *ResourceService) searchPage(
	ctx context.Context, rt *scim.ResourceType, q *Query, start, count int,
) (scim.ListResponse, error) {
	body, err := json.Marshal(q.searchRequest(start, count))
	if err != nil {
		return scim.ListResponse{}, fmt.Errorf("scimkit client: encode search request: %w", err)
	}
	rawURL := s.client.requestURL(rt.Endpoint, ".search")
	resp, err := s.client.do(ctx, http.MethodPost, rawURL, body, nil)
	if err != nil {
		return scim.ListResponse{}, err
	}
	return decodeListResponse(resp)
}

func decodeListResponse(resp *rawResponse) (scim.ListResponse, error) {
	if resp.status >= 400 {
		return scim.ListResponse{}, decodeError(resp.status, resp.body)
	}
	var lr scim.ListResponse
	if err := lr.UnmarshalJSON(resp.body); err != nil {
		return scim.ListResponse{}, fmt.Errorf("scimkit client: decode list response: %w", err)
	}
	return lr, nil
}

// params renders the query as GET URL parameters.
func (q *Query) params(start, count int) url.Values {
	v := url.Values{}
	if q.Filter != nil {
		v.Set("filter", q.Filter.String())
	}
	if q.SortBy != nil {
		v.Set("sortBy", q.SortBy.String())
		if !q.Ascending {
			v.Set("sortOrder", "descending")
		}
	}
	v.Set("startIndex", strconv.Itoa(start))
	v.Set("count", strconv.Itoa(count))
	if len(q.Attributes) > 0 {
		v.Set("attributes", strings.Join(q.Attributes, ","))
	}
	if len(q.ExcludedAttributes) > 0 {
		v.Set("excludedAttributes", strings.Join(q.ExcludedAttributes, ","))
	}
	return v
}

// searchRequestBody is the POST /.search body (RFC 7644 §3.4.3).
type searchRequestBody struct {
	Schemas            []string `json:"schemas"`
	Filter             string   `json:"filter,omitempty"`
	SortBy             string   `json:"sortBy,omitempty"`
	SortOrder          string   `json:"sortOrder,omitempty"`
	StartIndex         int      `json:"startIndex"`
	Count              int      `json:"count"`
	Attributes         []string `json:"attributes,omitempty"`
	ExcludedAttributes []string `json:"excludedAttributes,omitempty"`
}

func (q *Query) searchRequest(start, count int) searchRequestBody {
	body := searchRequestBody{
		Schemas:            []string{scim.MessageSearchRequest},
		StartIndex:         start,
		Count:              count,
		Attributes:         q.Attributes,
		ExcludedAttributes: q.ExcludedAttributes,
	}
	if q.Filter != nil {
		body.Filter = q.Filter.String()
	}
	if q.SortBy != nil {
		body.SortBy = q.SortBy.String()
		if !q.Ascending {
			body.SortOrder = "descending"
		}
	}
	return body
}
