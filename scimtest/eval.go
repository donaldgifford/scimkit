package scimtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"text/template"
)

// substitute resolves text/template actions in s against vars. A string
// with no action is returned unchanged; referencing an unset variable is
// an error, so a broken capture chain fails loudly instead of sending an
// empty path segment.
func substitute(s string, vars map[string]string) (string, error) {
	if !strings.Contains(s, "{{") {
		return s, nil
	}
	tmpl, err := template.New("step").Option("missingkey=error").Parse(s)
	if err != nil {
		return "", fmt.Errorf("scimtest: parse template %q: %w", s, err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("scimtest: substitute %q: %w", s, err)
	}
	return buf.String(), nil
}

// applyCaptures reads each capture's path from the response body and binds
// it as a string variable for later steps.
func applyCaptures(caps []Capture, body []byte, vars map[string]string) error {
	if len(caps) == 0 {
		return nil
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("scimtest: capture: decode response body: %w", err)
	}
	for _, c := range caps {
		v, ok := lookupPath(doc, c.Path)
		if !ok {
			return fmt.Errorf("scimtest: capture %q: path %q not found in response", c.Var, c.Path)
		}
		vars[c.Var] = fmt.Sprint(v)
	}
	return nil
}

// evaluate checks a response against an expectation, returning one
// human-readable message per failure. An empty result means the step
// passed.
func evaluate(exp Expectation, status int, header http.Header, body []byte) []string {
	var fails []string
	if exp.Status != 0 && status != exp.Status {
		fails = append(fails, fmt.Sprintf("status: got %d, want %d", status, exp.Status))
	}

	var doc any
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &doc); err != nil {
			if exp.ScimType != "" || len(exp.Body) > 0 {
				fails = append(fails, fmt.Sprintf("body: not valid JSON: %v", err))
			}
			return append(fails, checkHeaders(exp.Headers, header)...)
		}
	}

	if exp.ScimType != "" {
		fails = append(fails, checkScimType(exp.ScimType, doc)...)
	}
	fails = append(fails, checkHeaders(exp.Headers, header)...)
	for _, a := range exp.Body {
		fails = append(fails, checkAssertion(doc, a)...)
	}
	return fails
}

func checkScimType(want string, doc any) []string {
	got, _ := lookupPath(doc, "scimType")
	s, ok := got.(string)
	if !ok || !strings.EqualFold(s, want) {
		return []string{fmt.Sprintf("scimType: got %q, want %q", s, want)}
	}
	return nil
}

func checkHeaders(want map[string]string, header http.Header) []string {
	var fails []string
	for k, v := range want {
		if got := header.Get(k); got != v {
			fails = append(fails, fmt.Sprintf("header %s: got %q, want %q", k, got, v))
		}
	}
	return fails
}

func checkAssertion(doc any, a BodyAssertion) []string {
	got, ok := lookupPath(doc, a.Path)
	switch {
	case a.Unassigned:
		if !isUnassigned(got, ok) {
			return []string{fmt.Sprintf("%s: expected unassigned, got %v", a.Path, got)}
		}
	case a.Len != nil:
		arr, isArr := got.([]any)
		if !ok || !isArr {
			return []string{fmt.Sprintf("%s: expected array of length %d, got %v", a.Path, *a.Len, got)}
		}
		if len(arr) != *a.Len {
			return []string{fmt.Sprintf("%s: length got %d, want %d", a.Path, len(arr), *a.Len)}
		}
	default:
		if !ok {
			return []string{fmt.Sprintf("%s: missing, want %v", a.Path, a.Value)}
		}
		if !valueEqual(a.Value, got) {
			return []string{fmt.Sprintf("%s: got %v, want %v", a.Path, got, a.Value)}
		}
	}
	return nil
}

// isUnassigned reports whether a looked-up value counts as having no value
// under SCIM's null/[]/absent equivalence.
func isUnassigned(got any, present bool) bool {
	if !present || got == nil {
		return true
	}
	arr, ok := got.([]any)
	return ok && len(arr) == 0
}
