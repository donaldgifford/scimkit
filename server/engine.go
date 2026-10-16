package server

import (
	"context"
	"fmt"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// runQuery executes a normalized list request against the type's store:
// full push-down when the store supports the filter, otherwise an
// in-memory scan-and-match capped by MaxFilterResults. Whoever filters
// sorts, so results are ordered exactly once.
func (h *typeHandler) runQuery(ctx context.Context, q Query) (Page, error) {
	if q.Filter == nil || h.supportsFilter(q.Filter) {
		return h.store.List(ctx, q)
	}
	return h.scanQuery(ctx, q)
}

// supportsFilter consults the optional capability; a store without it is
// assumed to push every expression down.
func (h *typeHandler) supportsFilter(f filter.Expr) bool {
	if h.filterCap == nil {
		return true
	}
	return h.filterCap.SupportsFilter(f)
}

// scanQuery is the filter fallback: list every resource unfiltered, match
// in memory, sort, and paginate. The unfiltered set is capped by
// MaxFilterResults so an unindexed filter degrades loudly (tooMany), not
// silently.
func (h *typeHandler) scanQuery(ctx context.Context, q Query) (Page, error) {
	limit := h.cfg.MaxFilterResults
	page, err := h.store.List(ctx, Query{StartIndex: 1, Count: limit + 1})
	if err != nil {
		return Page{}, err
	}
	if page.TotalResults > limit || len(page.Resources) > limit {
		return Page{}, scim.ErrTooMany(fmt.Sprintf(
			"filter fallback would scan %d resources, above the %d limit", page.TotalResults, limit))
	}
	matched := make([]*scim.Resource, 0, len(page.Resources))
	for _, res := range page.Resources {
		ok, err := filter.Match(res, q.Filter)
		if err != nil {
			return Page{}, err
		}
		if ok {
			matched = append(matched, res)
		}
	}
	if q.SortBy != nil {
		SortResources(matched, &h.rt, q.SortBy, q.Ascending)
	}
	return Page{Resources: paginate(matched, q.StartIndex, q.Count), TotalResults: len(matched)}, nil
}

// paginate slices the full result set per the normalized startIndex and
// count.
func paginate(list []*scim.Resource, startIndex, count int) []*scim.Resource {
	offset := startIndex - 1
	if offset >= len(list) || count == 0 {
		return nil
	}
	end := min(offset+count, len(list))
	return list[offset:end]
}
