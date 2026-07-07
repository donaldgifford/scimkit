package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLenientParse covers every off-grammar shape Lenient accepts. Each
// case names its source per the phase 2 discipline: no lenient acceptance
// without a documented origin.
func TestLenientParse(t *testing.T) {
	tests := []struct {
		name string
		// source documents where the off-grammar shape was observed.
		source string
		input  string
		want   Expr
	}{
		{
			name:   "bare not without parentheses",
			source: "DESIGN-0001 open question decisions: bare-not is the documented IdP deviation",
			input:  `not userName eq "x"`,
			want:   Not{E: Compare{Path: Path{Attr: "userName"}, Op: OpEqual, Value: "x"}},
		},
		{
			name:   "bare not binds one clause",
			source: "DESIGN-0001: bare not applies to the immediately following attribute expression",
			input:  `not a pr and b pr`,
			want: And{
				L: Not{E: Present{Path: Path{Attr: "a"}}},
				R: Present{Path: Path{Attr: "b"}},
			},
		},
		{
			name:   "parenthesized not still parses",
			source: "strict grammar remains a subset of lenient",
			input:  `not (a pr)`,
			want:   Not{E: Present{Path: Path{Attr: "a"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input, Lenient())
			require.NoError(t, err, "source: %s", tt.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestLenientShapesRejectedInStrictMode pins that lenient acceptances stay
// opt-in.
func TestLenientShapesRejectedInStrictMode(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "bare not", input: `not userName eq "x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			require.Error(t, err)
		})
	}
}

// TestLenientStringIsStrict pins that a leniently parsed AST renders the
// strict grammar form, so lenient input never propagates.
func TestLenientStringIsStrict(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "bare not renders parenthesized", input: `not a pr`, want: `not (a pr)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := Parse(tt.input, Lenient())
			require.NoError(t, err)
			assert.Equal(t, tt.want, e.String())

			strict, err := Parse(e.String())
			require.NoError(t, err, "canonical form must parse in strict mode")
			assert.Equal(t, e, strict)
		})
	}
}
