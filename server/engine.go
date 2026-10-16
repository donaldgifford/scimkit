package server

import (
	"context"
	"fmt"

	"github.com/donaldgifford/scimkit/filter"
	"github.com/donaldgifford/scimkit/scim"
)

// runQuery executes a normalized list request against the type's store:
// full push-down when the store supports the filter and the sort,
// otherwise an in-memory scan capped by MaxFilterResults that applies
// whichever of the two the store could not. Whoever filters sorts, so
// results are ordered exactly once.
func (h *typeHandler) runQuery(ctx context.Context, q Query) (Page, error) {
	pushFilter := q.Filter == nil || h.supportsFilter(q.Filter)
	pushSort := q.SortBy == nil || h.supportsSort(q.SortBy)
	if pushFilter && pushSort {
		return h.store.List(ctx, q)
	}
	if !pushSort {
		h.cfg.Logger.Debug("scim sort fallback: store declined sortBy, sorting in memory",
			"type", h.rt.Name, "sortBy", fullPathText(q.SortBy))
	}
	return h.scanQuery(ctx, q, pushFilter)
}

// supportsFilter consults the optional capability; a store without it is
// assumed to push every expression down.
func (h *typeHandler) supportsFilter(f filter.Expr) bool {
	if h.filterCap == nil {
		return true
	}
	return h.filterCap.SupportsFilter(f)
}

// supportsSort consults the optional capability; a store without it is
// assumed to honor every sortBy path.
func (h *typeHandler) supportsSort(p *filter.Path) bool {
	if h.sortCap == nil {
		return true
	}
	return h.sortCap.SupportsSort(p)
}

// scanQuery is the in-memory fallback: list the result set in one page,
// match in memory unless the filter was pushed down, sort, and paginate.
// The fetched set is capped by MaxFilterResults so an unindexed filter or
// an unsortable path degrades loudly (tooMany), not silently.
func (h *typeHandler) scanQuery(ctx context.Context, q Query, pushFilter bool) (Page, error) {
	limit := h.cfg.MaxFilterResults
	fetch := Query{StartIndex: 1, Count: limit + 1}
	if pushFilter {
		fetch.Filter = q.Filter
	}
	page, err := h.store.List(ctx, fetch)
	if err != nil {
		return Page{}, err
	}
	if page.TotalResults > limit || len(page.Resources) > limit {
		return Page{}, scim.ErrTooMany(fmt.Sprintf(
			"query fallback would scan %d resources, above the %d limit", page.TotalResults, limit))
	}
	matched := page.Resources
	if !pushFilter {
		matched = make([]*scim.Resource, 0, len(page.Resources))
		for _, res := range page.Resources {
			ok, err := filter.Match(res, q.Filter)
			if err != nil {
				return Page{}, err
			}
			if ok {
				matched = append(matched, res)
			}
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
