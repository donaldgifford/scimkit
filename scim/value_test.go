package scim

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scalarAttr builds a minimal single-valued attribute for coercion tests.
func scalarAttr(name string, typ AttrType) *Attribute {
	return &Attribute{Name: name, Type: typ}
}

// emailsAttr models a multi-valued complex attribute with the standard
// value/type/primary sub-attribute trio.
func emailsAttr() *Attribute {
	return &Attribute{
		Name:        "emails",
		Type:        TypeComplex,
		MultiValued: true,
		SubAttributes: []Attribute{
			{Name: "value", Type: TypeString},
			{Name: "type", Type: TypeString},
			{Name: "primary", Type: TypeBoolean},
		},
	}
}

func TestCoerceValueScalars(t *testing.T) {
	utc := time.Date(2015, 5, 23, 4, 34, 22, 639000000, time.UTC)
	tests := []struct {
		name    string
		attr    *Attribute
		in      any
		want    any
		wantErr string
	}{
		{name: "nil is unassigned", attr: scalarAttr("s", TypeString), in: nil, want: nil},
		{name: "string", attr: scalarAttr("s", TypeString), in: "hi", want: "hi"},
		{name: "string rejects int", attr: scalarAttr("s", TypeString), in: 7, wantErr: "expected string"},
		{name: "boolean", attr: scalarAttr("b", TypeBoolean), in: true, want: true},
		{name: "boolean rejects string", attr: scalarAttr("b", TypeBoolean), in: "true", wantErr: "expected boolean"},
		{name: "decimal from float64", attr: scalarAttr("d", TypeDecimal), in: 3.14, want: 3.14},
		{name: "decimal from int", attr: scalarAttr("d", TypeDecimal), in: 3, want: 3.0},
		{name: "decimal from json.Number", attr: scalarAttr("d", TypeDecimal), in: json.Number("3.14"), want: 3.14},
		{name: "decimal rejects string", attr: scalarAttr("d", TypeDecimal), in: "3.14", wantErr: "expected decimal"},
		{name: "integer from int", attr: scalarAttr("i", TypeInteger), in: 42, want: int64(42)},
		{name: "integer from int64", attr: scalarAttr("i", TypeInteger), in: int64(42), want: int64(42)},
		{name: "integer from json.Number", attr: scalarAttr("i", TypeInteger), in: json.Number("42"), want: int64(42)},
		{name: "integer from integral float64", attr: scalarAttr("i", TypeInteger), in: 42.0, want: int64(42)},
		{name: "integer rejects fractional float64", attr: scalarAttr("i", TypeInteger), in: 42.5, wantErr: "expected integer"},
		{name: "integer rejects fractional json.Number", attr: scalarAttr("i", TypeInteger), in: json.Number("4.2"), wantErr: "expected integer"},
		{name: "dateTime from time.Time", attr: scalarAttr("t", TypeDateTime), in: utc, want: utc},
		{name: "dateTime from RFC 3339 string", attr: scalarAttr("t", TypeDateTime), in: "2015-05-23T04:34:22.639Z", want: utc},
		{name: "dateTime rejects malformed string", attr: scalarAttr("t", TypeDateTime), in: "23/05/2015", wantErr: "invalid dateTime"},
		{name: "dateTime rejects int", attr: scalarAttr("t", TypeDateTime), in: 5, wantErr: "expected dateTime"},
		{name: "binary from bytes", attr: scalarAttr("x", TypeBinary), in: []byte("hello"), want: []byte("hello")},
		{name: "binary from base64 string", attr: scalarAttr("x", TypeBinary), in: "aGVsbG8=", want: []byte("hello")},
		{name: "binary rejects bad base64", attr: scalarAttr("x", TypeBinary), in: "!!!", wantErr: "invalid base64"},
		{
			name: "reference from string",
			attr: scalarAttr("r", TypeReference),
			in:   "https://example.com/Users/1",
			want: ReferenceValue("https://example.com/Users/1"),
		},
		{name: "reference from ReferenceValue", attr: scalarAttr("r", TypeReference), in: ReferenceValue("u"), want: ReferenceValue("u")},
		{name: "reference rejects bool", attr: scalarAttr("r", TypeReference), in: false, wantErr: "expected reference"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coerceValue(tt.attr, tt.in)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCoerceComplex(t *testing.T) {
	nameAttr := &Attribute{
		Name: "name",
		Type: TypeComplex,
		SubAttributes: []Attribute{
			{Name: "givenName", Type: TypeString},
			{Name: "familyName", Type: TypeString},
		},
	}
	nested := &Attribute{
		Name: "outer",
		Type: TypeComplex,
		SubAttributes: []Attribute{
			{Name: "inner", Type: TypeComplex},
		},
	}
	tests := []struct {
		name    string
		attr    *Attribute
		in      any
		want    any
		wantErr string
	}{
		{
			name: "keys fold case-insensitively",
			attr: nameAttr,
			in:   map[string]any{"GIVENNAME": "Barbara", "familyName": "Jensen"},
			want: ComplexValue{"givenname": "Barbara", "familyname": "Jensen"},
		},
		{
			name: "unknown sub-attributes dropped",
			attr: nameAttr,
			in:   map[string]any{"givenName": "B", "nickname": "Babs"},
			want: ComplexValue{"givenname": "B"},
		},
		{
			name: "null sub-values dropped",
			attr: nameAttr,
			in:   map[string]any{"givenName": "B", "familyName": nil},
			want: ComplexValue{"givenname": "B"},
		},
		{
			name: "all-null collapses to unassigned",
			attr: nameAttr,
			in:   map[string]any{"givenName": nil},
			want: nil,
		},
		{
			name: "ComplexValue input accepted",
			attr: nameAttr,
			in:   ComplexValue{"givenname": "B"},
			want: ComplexValue{"givenname": "B"},
		},
		{
			name:    "sub-value type mismatch",
			attr:    nameAttr,
			in:      map[string]any{"givenName": 7},
			wantErr: `sub-attribute "givenName"`,
		},
		{
			name:    "complex within complex rejected",
			attr:    nested,
			in:      map[string]any{"inner": map[string]any{}},
			wantErr: "exceeds max depth",
		},
		{
			name:    "non-map rejected",
			attr:    nameAttr,
			in:      "not a map",
			wantErr: "expected complex",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coerceValue(tt.attr, tt.in)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCoerceValueMulti(t *testing.T) {
	stringsAttr := &Attribute{Name: "tags", Type: TypeString, MultiValued: true}
	tests := []struct {
		name    string
		attr    *Attribute
		in      any
		want    any
		wantErr string
	}{
		{
			name: "empty slice canonicalizes to unassigned",
			attr: stringsAttr,
			in:   []any{},
			want: nil,
		},
		{
			name: "typed string slice",
			attr: stringsAttr,
			in:   []string{"a", "b"},
			want: []string{"a", "b"},
		},
		{
			name: "decoded-JSON slice",
			attr: stringsAttr,
			in:   []any{"a", "b"},
			want: []string{"a", "b"},
		},
		{
			name: "nil elements skipped",
			attr: stringsAttr,
			in:   []any{"a", nil, "b"},
			want: []string{"a", "b"},
		},
		{
			name: "all-nil collapses to unassigned",
			attr: stringsAttr,
			in:   []any{nil},
			want: nil,
		},
		{
			name:    "element type mismatch reports index",
			attr:    stringsAttr,
			in:      []any{"a", 7},
			wantErr: "element 1",
		},
		{
			name:    "scalar rejected for multi-valued",
			attr:    stringsAttr,
			in:      "a",
			wantErr: "expected multi-valued",
		},
		{
			name: "complex elements",
			attr: emailsAttr(),
			in: []any{
				map[string]any{"Value": "b@example.com", "Primary": true},
				map[string]any{"value": "c@example.com"},
			},
			want: MultiComplexValue{
				{"value": "b@example.com", "primary": true},
				{"value": "c@example.com"},
			},
		},
		{
			name: "single primary allowed",
			attr: emailsAttr(),
			in: []any{
				map[string]any{"value": "a", "primary": true},
				map[string]any{"value": "b", "primary": false},
			},
			want: MultiComplexValue{
				{"value": "a", "primary": true},
				{"value": "b", "primary": false},
			},
		},
		{
			name: "two primaries rejected",
			attr: emailsAttr(),
			in: []any{
				map[string]any{"value": "a", "primary": true},
				map[string]any{"value": "b", "primary": true},
			},
			wantErr: "more than one element has primary",
		},
		{
			name: "empty complex elements dropped",
			attr: emailsAttr(),
			in:   []any{map[string]any{}, map[string]any{"value": "a"}},
			want: MultiComplexValue{{"value": "a"}},
		},
		{
			name: "MultiComplexValue input accepted",
			attr: emailsAttr(),
			in:   MultiComplexValue{{"value": "a"}},
			want: MultiComplexValue{{"value": "a"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := coerceValue(tt.attr, tt.in)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCoerceMultiScalarTypes(t *testing.T) {
	utc := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name string
		typ  AttrType
		in   []any
		want any
	}{
		{name: "booleans", typ: TypeBoolean, in: []any{true, false}, want: []bool{true, false}},
		{name: "decimals", typ: TypeDecimal, in: []any{json.Number("1.5")}, want: []float64{1.5}},
		{name: "integers", typ: TypeInteger, in: []any{json.Number("7")}, want: []int64{7}},
		{name: "dateTimes", typ: TypeDateTime, in: []any{"2020-01-02T03:04:05Z"}, want: []time.Time{utc}},
		{name: "binaries", typ: TypeBinary, in: []any{"aGk="}, want: [][]byte{[]byte("hi")}},
		{name: "references", typ: TypeReference, in: []any{"u1"}, want: []ReferenceValue{"u1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := &Attribute{Name: "m", Type: tt.typ, MultiValued: true}
			got, err := coerceValue(attr, tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMultiComplexValueIndexFunc(t *testing.T) {
	emails := MultiComplexValue{
		{"value": "a@example.com"},
		{"value": "b@example.com", "primary": true},
	}
	tests := []struct {
		name string
		m    MultiComplexValue
		pred func(ComplexValue) bool
		want int
	}{
		{
			name: "finds primary",
			m:    emails,
			pred: func(cv ComplexValue) bool { p, _ := cv["primary"].(bool); return p },
			want: 1,
		},
		{
			name: "no match",
			m:    emails,
			pred: func(cv ComplexValue) bool { return cv["value"] == "z" },
			want: -1,
		},
		{
			name: "nil receiver",
			m:    nil,
			pred: func(ComplexValue) bool { return true },
			want: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.m.IndexFunc(tt.pred))
		})
	}
}
