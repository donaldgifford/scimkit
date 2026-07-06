package scim

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ListResponse is the urn:ietf:params:scim:api:messages:2.0:ListResponse
// message (RFC 7644 §3.4.2). Resources stay raw so callers can decode each
// element against its own resource type.
type ListResponse struct {
	// TotalResults is the total number of results matching the query,
	// which can exceed len(Resources) when paging.
	TotalResults int
	// StartIndex is the 1-based index of the first returned result; zero
	// means unassigned (not paged).
	StartIndex int
	// ItemsPerPage is the page size; zero means unassigned.
	ItemsPerPage int
	// Resources holds the returned resources verbatim.
	Resources []json.RawMessage
}

// MarshalJSON emits the wire invariants the codec owns: the Resources key
// is capitalized and totalResults/startIndex/itemsPerPage are JSON numbers.
// Resources is always present — an empty page is an empty array, never a
// missing key — while zero pagination fields are omitted.
func (lr ListResponse) MarshalJSON() ([]byte, error) {
	fields := []jsonField{
		{name: attrSchemas, value: []string{MessageListResponse}},
		{name: "totalResults", value: lr.TotalResults},
	}
	if lr.StartIndex > 0 {
		fields = append(fields, jsonField{name: "startIndex", value: lr.StartIndex})
	}
	if lr.ItemsPerPage > 0 {
		fields = append(fields, jsonField{name: "itemsPerPage", value: lr.ItemsPerPage})
	}
	resources := lr.Resources
	if resources == nil {
		resources = []json.RawMessage{}
	}
	fields = append(fields, jsonField{name: "Resources", value: resources})
	return marshalOrdered(fields)
}

// UnmarshalJSON decodes a ListResponse body with case-insensitive keys, so
// service providers that emit "resources" instead of "Resources" still
// parse.
func (lr *ListResponse) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return ErrInvalidSyntax(fmt.Sprintf("parse list response: %v", err))
	}
	out := ListResponse{}
	for key, rv := range raw {
		var err error
		switch foldName(key) {
		case "totalresults":
			out.TotalResults, err = listInt(key, rv)
		case "startindex":
			out.StartIndex, err = listInt(key, rv)
		case "itemsperpage":
			out.ItemsPerPage, err = listInt(key, rv)
		case "resources":
			if err = json.Unmarshal(rv, &out.Resources); err != nil {
				err = ErrInvalidSyntax(fmt.Sprintf("list response %q: %v", key, err))
			}
		}
		if err != nil {
			return err
		}
	}
	*lr = out
	return nil
}

// listInt decodes a pagination field, tolerating string-wrapped numbers
// from lax implementations while the marshal side always emits numbers.
func listInt(key string, raw json.RawMessage) (int, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return 0, ErrInvalidValue(fmt.Sprintf("list response %q: %v", key, err))
	}
	var n json.Number
	switch t := v.(type) {
	case json.Number:
		n = t
	case string:
		n = json.Number(t)
	default:
		return 0, ErrInvalidValue(fmt.Sprintf("list response %q: expected integer, got %T", key, v))
	}
	i, err := n.Int64()
	if err != nil {
		return 0, ErrInvalidValue(fmt.Sprintf("list response %q: %v", key, err))
	}
	return int(i), nil
}
