// Package filter implements the RFC 7644 SCIM filter and PATCH path
// grammars: parse, evaluate, and build.
//
// [Parse] turns a §3.4.2.2 filter into a typed AST ([Compare], [Present],
// [AndExpr], [OrExpr], [NotExpr], [ValuePath]) with precedence
// not > and > or; [ParsePath] handles the §3.5.2 Figure 7 PATCH path
// grammar. Both are context-free — URN-qualified attribute paths split at
// the last colon, which the ATTRNAME grammar makes structurally exact —
// and both accept the [Lenient] option for documented off-grammar IdP
// shapes. Expr.String() renders a re-parseable filter: Parse(e.String())
// yields an equal AST.
//
// [Match] evaluates an expression against a [scim.Resource] with the
// schema deciding semantics: caseExact string comparison, chronological
// dateTime, numeric compare, existential multi-valued matching, and
// per-element valuePath correlation. Undefined attributes evaluate as
// no-value rather than erroring; the only Match errors are ordering
// operators on boolean or binary attributes.
//
// The builder functions ([Eq], [And], [Not], [HasValue], …) construct
// expressions for the client direction with correct quoting and escaping.
//
// The tokenizer and Path type here are the shared attrPath machinery the
// patch and server packages reuse.
package filter
