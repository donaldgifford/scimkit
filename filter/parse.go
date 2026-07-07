package filter

import (
	"encoding/json"
	"fmt"

	"github.com/donaldgifford/scimkit/scim"
)

// ParseOption configures Parse and ParsePath.
type ParseOption func(*parseConfig)

type parseConfig struct {
	lenient bool
}

// Parse parses an RFC 7644 §3.4.2.2 filter with precedence not > and > or
// (from the section's prose; the ABNF alone is ambiguous). Operators and
// logical keywords fold case; compValue is a strict JSON literal. Syntax
// errors are scim.ErrInvalidFilter with byte-position detail.
func Parse(s string, opts ...ParseOption) (Expr, error) {
	p, err := newParser(s, opts...)
	if err != nil {
		return nil, err
	}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tokEOF {
		return nil, p.errorf("expected end of filter, found %s", p.tok.kind)
	}
	return e, nil
}

// parser is a recursive-descent parser with one token of lookahead.
type parser struct {
	s       scanner
	tok     token
	lenient bool
}

func newParser(s string, opts ...ParseOption) (*parser, error) {
	var cfg parseConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	p := &parser{s: scanner{input: s}, lenient: cfg.lenient}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *parser) advance() error {
	tok, err := p.s.next()
	if err != nil {
		return err
	}
	p.tok = tok
	return nil
}

func (p *parser) errorf(format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)
	return scim.ErrInvalidFilter(fmt.Sprintf("position %d: %s", p.tok.pos, detail))
}

// word reports whether the current token is the given keyword,
// case-insensitively.
func (p *parser) word(keyword string) bool {
	return p.tok.kind == tokWord && scim.FoldName(p.tok.text) == keyword
}

func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.word("or") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = Or{L: left, R: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.word("and") {
		if err := p.advance(); err != nil {
			return nil, err
		}
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = And{L: left, R: right}
	}
	return left, nil
}

// parseUnary handles the not operator. The grammar requires not's operand
// parenthesized ("not" "(" FILTER ")"); the Lenient option additionally
// accepts a bare clause, a shape some IdPs emit.
func (p *parser) parseUnary() (Expr, error) {
	if !p.word("not") {
		return p.parsePrimary()
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.tok.kind != tokLParen && !p.lenient {
		return nil, p.errorf("not requires a parenthesized filter")
	}
	// parsePrimary consumes the parenthesized group; under Lenient it also
	// accepts a bare clause as not's operand.
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	return Not{E: e}, nil
}

func (p *parser) parsePrimary() (Expr, error) {
	switch p.tok.kind {
	case tokLParen:
		if err := p.advance(); err != nil {
			return nil, err
		}
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.tok.kind != tokRParen {
			return nil, p.errorf("expected ')', found %s", p.tok.kind)
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return e, nil
	case tokWord:
		return p.parseAttrExpr()
	case tokEOF, tokString, tokNumber, tokRParen, tokLBracket, tokRBracket:
		return nil, p.errorf("expected attribute expression, found %s", p.tok.kind)
	default:
		return nil, p.errorf("expected attribute expression, found %s", p.tok.kind)
	}
}

// parseAttrExpr parses attrExp (attrPath pr / attrPath compareOp compValue)
// and valuePath (attrPath "[" valFilter "]").
func (p *parser) parseAttrExpr() (Expr, error) {
	word := p.tok
	urn, attr, sub, err := splitAttrPath(word.text, word.pos)
	if err != nil {
		return nil, err
	}
	path := Path{URN: urn, Attr: attr, SubAttr: sub}
	if err := p.advance(); err != nil {
		return nil, err
	}
	if p.tok.kind == tokLBracket {
		return p.parseValuePath(path)
	}
	if p.tok.kind != tokWord {
		return nil, p.errorf("expected operator after %q, found %s", word.text, p.tok.kind)
	}
	op := Op(scim.FoldName(p.tok.text))
	if op == OpPresent {
		if err := p.advance(); err != nil {
			return nil, err
		}
		return Present{Path: path}, nil
	}
	if !op.comparison() {
		return nil, p.errorf("unknown operator %q", p.tok.text)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	value, err := p.parseCompValue()
	if err != nil {
		return nil, err
	}
	return Compare{Path: path, Op: op, Value: value}, nil
}

func (p *parser) parseValuePath(path Path) (Expr, error) {
	if err := p.advance(); err != nil { // consume '['
		return nil, err
	}
	f, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.tok.kind != tokRBracket {
		return nil, p.errorf("expected ']' to close value filter, found %s", p.tok.kind)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return ValuePath{Path: path, Filter: f}, nil
}

// parseCompValue parses a strict JSON compValue literal: string, number,
// true, false, or null. The keyword literals are lowercase-exact per JSON.
func (p *parser) parseCompValue() (any, error) {
	var v any
	switch p.tok.kind {
	case tokString:
		v = p.tok.text
	case tokNumber:
		v = json.Number(p.tok.text)
	case tokWord:
		switch p.tok.text {
		case "true":
			v = true
		case "false":
			v = false
		case litNull:
			v = nil
		default:
			return nil, p.errorf("expected JSON literal, found %q", p.tok.text)
		}
	default:
		return nil, p.errorf("expected comparison value, found %s", p.tok.kind)
	}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return v, nil
}

// comparison reports whether the operator is a binary comparison operator.
func (o Op) comparison() bool {
	switch o {
	case OpEqual, OpNotEqual, OpContains, OpStartsWith, OpEndsWith,
		OpGreaterThan, OpGreaterThanOrEqual, OpLessThan, OpLessThanOrEqual:
		return true
	case OpPresent:
		return false
	default:
		return false
	}
}
