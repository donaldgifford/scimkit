package filter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/donaldgifford/scimkit/scim"
)

// tokenKind classifies scanner output.
type tokenKind int

const (
	tokEOF tokenKind = iota
	// tokWord covers attribute paths, operator keywords, and the JSON
	// true/false/null literals — the parser disambiguates by position.
	tokWord
	// tokString is a JSON string literal; text holds the decoded value.
	tokString
	// tokNumber is a JSON number literal; text holds the raw text so
	// integer and decimal literals stay distinct.
	tokNumber
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
)

func (k tokenKind) String() string {
	switch k {
	case tokEOF:
		return "end of input"
	case tokWord:
		return "word"
	case tokString:
		return "string literal"
	case tokNumber:
		return "number literal"
	case tokLParen:
		return "'('"
	case tokRParen:
		return "')'"
	case tokLBracket:
		return "'['"
	case tokRBracket:
		return "']'"
	default:
		return "unknown token"
	}
}

type token struct {
	kind tokenKind
	text string
	// pos is the byte offset of the token's first character, for error
	// detail.
	pos int
}

// scanner turns a filter string into tokens. String literals and brackets
// become their own tokens, so word tokens can never contain quoted or
// bracketed colons — the last-colon attrPath split stays purely lexical.
type scanner struct {
	input string
	pos   int
}

// isWordChar reports characters legal inside an attrPath word: ATTRNAME
// nameChars plus ':' (URN qualification), '.' (subAttr), and '$' (the $ref
// cross-RFC inconsistency, accepted in the base grammar).
func isWordChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_' || c == '$' || c == '.' || c == ':':
		return true
	default:
		return false
	}
}

func (s *scanner) next() (token, error) {
	for s.pos < len(s.input) && (s.input[s.pos] == ' ' || s.input[s.pos] == '\t') {
		s.pos++
	}
	if s.pos >= len(s.input) {
		return token{kind: tokEOF, pos: s.pos}, nil
	}
	start := s.pos
	c := s.input[s.pos]
	if kind, ok := punctKind(c); ok {
		s.pos++
		return token{kind: kind, pos: start}, nil
	}
	switch {
	case c == '"':
		return s.scanString()
	case c == '-' || (c >= '0' && c <= '9'):
		return s.scanNumber()
	case isWordChar(c):
		for s.pos < len(s.input) && isWordChar(s.input[s.pos]) {
			s.pos++
		}
		return token{kind: tokWord, text: s.input[start:s.pos], pos: start}, nil
	default:
		return token{}, scim.ErrInvalidFilter(
			fmt.Sprintf("position %d: unexpected character %q", start, c),
		)
	}
}

func punctKind(c byte) (tokenKind, bool) {
	switch c {
	case '(':
		return tokLParen, true
	case ')':
		return tokRParen, true
	case '[':
		return tokLBracket, true
	case ']':
		return tokRBracket, true
	default:
		return tokEOF, false
	}
}

// scanString consumes a JSON string literal and decodes it via
// encoding/json, so escape handling is correct by construction.
func (s *scanner) scanString() (token, error) {
	start := s.pos
	s.pos++ // opening quote
	for s.pos < len(s.input) {
		switch s.input[s.pos] {
		case '\\':
			s.pos += 2
		case '"':
			s.pos++
			raw := s.input[start:s.pos]
			var decoded string
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				return token{}, scim.ErrInvalidFilter(
					fmt.Sprintf("position %d: invalid string literal: %v", start, err),
				)
			}
			return token{kind: tokString, text: decoded, pos: start}, nil
		default:
			s.pos++
		}
	}
	return token{}, scim.ErrInvalidFilter(
		fmt.Sprintf("position %d: unterminated string literal", start),
	)
}

// scanNumber consumes a JSON number literal, validating it through
// encoding/json so the grammar's strict-JSON compValue rule holds.
func (s *scanner) scanNumber() (token, error) {
	start := s.pos
	for s.pos < len(s.input) {
		c := s.input[s.pos]
		if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
			s.pos++
			continue
		}
		break
	}
	raw := s.input[start:s.pos]
	var n json.Number
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		return token{}, scim.ErrInvalidFilter(
			fmt.Sprintf("position %d: invalid number literal %q", start, raw),
		)
	}
	return token{kind: tokNumber, text: raw, pos: start}, nil
}

// splitAttrPath splits an attrPath word into URN, attribute name, and
// sub-attribute name. ATTRNAME cannot contain ':', so for any grammatical
// URN-qualified path the boundary is exactly the last colon — the split is
// structurally exact, not heuristic. Classification of the URN (core,
// declared extension, unknown) is evaluation's job, not the tokenizer's.
func splitAttrPath(word string, pos int) (urn, attr, sub string, err error) {
	rest := word
	if i := strings.LastIndexByte(word, ':'); i >= 0 {
		urn, rest = word[:i], word[i+1:]
		if !strings.HasPrefix(scim.FoldName(urn), "urn:") {
			return "", "", "", scim.ErrInvalidFilter(
				fmt.Sprintf("position %d: attribute path %q: schema URI must be a URN", pos, word),
			)
		}
	}
	attr, sub, found := strings.Cut(rest, ".")
	if err := validAttrName(attr); err != nil {
		return "", "", "", scim.ErrInvalidFilter(
			fmt.Sprintf("position %d: attribute path %q: %v", pos, word, err),
		)
	}
	if found {
		if err := validAttrName(sub); err != nil || strings.Contains(sub, ".") {
			return "", "", "", scim.ErrInvalidFilter(
				fmt.Sprintf("position %d: attribute path %q: invalid sub-attribute %q", pos, word, sub),
			)
		}
	}
	return urn, attr, sub, nil
}

// validAttrName checks the RFC 7644 ATTRNAME production, extended with a
// leading '$' to accept $ref (the grammar's nameChar omits '$' although
// RFC 7643 defines the $ref attribute — a documented cross-RFC
// inconsistency).
func validAttrName(name string) error {
	if name == "" {
		return fmt.Errorf("empty attribute name")
	}
	if !isNameStart(name[0]) {
		return fmt.Errorf("attribute name %q must start with a letter", name)
	}
	for i := 1; i < len(name); i++ {
		if !isNameChar(name[i]) {
			return fmt.Errorf("attribute name %q contains invalid character %q", name, name[i])
		}
	}
	return nil
}

func isNameStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '$'
}

func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c == '-' || c == '_'
}
