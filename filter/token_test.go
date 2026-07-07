package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scanAll(t *testing.T, input string) ([]token, error) {
	t.Helper()
	s := &scanner{input: input}
	var out []token
	for {
		tok, err := s.next()
		if err != nil {
			return nil, err
		}
		if tok.kind == tokEOF {
			return out, nil
		}
		out = append(out, tok)
	}
}

func TestScanner(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []token
		wantErr string
	}{
		{
			name:  "simple comparison",
			input: `userName eq "bjensen"`,
			want: []token{
				{kind: tokWord, text: "userName", pos: 0},
				{kind: tokWord, text: "eq", pos: 9},
				{kind: tokString, text: "bjensen", pos: 12},
			},
		},
		{
			name:  "URN-qualified path stays one word",
			input: `urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber pr`,
			want: []token{
				{kind: tokWord, text: "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber", pos: 0},
				{kind: tokWord, text: "pr", pos: 74},
			},
		},
		{
			name:  "value path brackets",
			input: `emails[type eq "work"]`,
			want: []token{
				{kind: tokWord, text: "emails", pos: 0},
				{kind: tokLBracket, pos: 6},
				{kind: tokWord, text: "type", pos: 7},
				{kind: tokWord, text: "eq", pos: 12},
				{kind: tokString, text: "work", pos: 15},
				{kind: tokRBracket, pos: 21},
			},
		},
		{
			name:  "grouping and keywords",
			input: `not (active eq true)`,
			want: []token{
				{kind: tokWord, text: "not", pos: 0},
				{kind: tokLParen, pos: 4},
				{kind: tokWord, text: "active", pos: 5},
				{kind: tokWord, text: "eq", pos: 12},
				{kind: tokWord, text: "true", pos: 15},
				{kind: tokRParen, pos: 19},
			},
		},
		{
			name:  "numbers keep raw text",
			input: `count gt 3 and ratio le 2.5`,
			want: []token{
				{kind: tokWord, text: "count", pos: 0},
				{kind: tokWord, text: "gt", pos: 6},
				{kind: tokNumber, text: "3", pos: 9},
				{kind: tokWord, text: "and", pos: 11},
				{kind: tokWord, text: "ratio", pos: 15},
				{kind: tokWord, text: "le", pos: 21},
				{kind: tokNumber, text: "2.5", pos: 24},
			},
		},
		{
			name:  "negative and exponent numbers",
			input: `n eq -1.5e3`,
			want: []token{
				{kind: tokWord, text: "n", pos: 0},
				{kind: tokWord, text: "eq", pos: 2},
				{kind: tokNumber, text: "-1.5e3", pos: 5},
			},
		},
		{
			name:  "string escapes decode",
			input: `title eq "a \"quoted\" \\ value"`,
			want: []token{
				{kind: tokWord, text: "title", pos: 0},
				{kind: tokWord, text: "eq", pos: 6},
				{kind: tokString, text: `a "quoted" \ value`, pos: 9},
			},
		},
		{
			name:  "colon inside string is not a path split",
			input: `note eq "a:b"`,
			want: []token{
				{kind: tokWord, text: "note", pos: 0},
				{kind: tokWord, text: "eq", pos: 5},
				{kind: tokString, text: "a:b", pos: 8},
			},
		},
		{
			name:  "dollar ref word",
			input: `members[$ref eq "x"]`,
			want: []token{
				{kind: tokWord, text: "members", pos: 0},
				{kind: tokLBracket, pos: 7},
				{kind: tokWord, text: "$ref", pos: 8},
				{kind: tokWord, text: "eq", pos: 13},
				{kind: tokString, text: "x", pos: 16},
				{kind: tokRBracket, pos: 19},
			},
		},
		{name: "empty input", input: ``, want: nil},
		{name: "unterminated string", input: `a eq "oops`, wantErr: "unterminated string"},
		{name: "bad escape", input: `a eq "\x"`, wantErr: "invalid string literal"},
		{name: "bad number", input: `a eq 1.2.3`, wantErr: "invalid number literal"},
		{name: "unexpected character", input: `a eq @`, wantErr: "unexpected character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := scanAll(t, tt.input)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSplitAttrPath(t *testing.T) {
	tests := []struct {
		name     string
		word     string
		wantURN  string
		wantAttr string
		wantSub  string
		wantErr  string
	}{
		{name: "bare attribute", word: "userName", wantAttr: "userName"},
		{name: "sub-attribute", word: "name.givenName", wantAttr: "name", wantSub: "givenName"},
		{
			name:     "URN qualified",
			word:     "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber",
			wantURN:  "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User",
			wantAttr: "employeeNumber",
		},
		{
			name:     "URN qualified with sub-attribute",
			word:     "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:manager.displayName",
			wantURN:  "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User",
			wantAttr: "manager",
			wantSub:  "displayName",
		},
		{name: "dollar ref", word: "$ref", wantAttr: "$ref"},
		{name: "non-URN scheme rejected", word: "http://x:attr", wantErr: "must be a URN"},
		{name: "empty attribute after URN", word: "urn:x:", wantErr: "empty attribute name"},
		{name: "leading digit", word: "1abc", wantErr: "must start with a letter"},
		{name: "double dot", word: "a.b.c", wantErr: "invalid sub-attribute"},
		{name: "trailing dot", word: "a.", wantErr: "invalid sub-attribute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			urn, attr, sub, err := splitAttrPath(tt.word, 0)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantURN, urn)
			assert.Equal(t, tt.wantAttr, attr)
			assert.Equal(t, tt.wantSub, sub)
		})
	}
}
