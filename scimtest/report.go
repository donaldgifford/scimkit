package scimtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Report is the result of running a scenario against a target. Passed is
// true only when every step passed and the run was not aborted.
type Report struct {
	Scenario  string       `json:"scenario"`
	Target    string       `json:"target"`
	Passed    bool         `json:"passed"`
	Aborted   bool         `json:"aborted,omitempty"`
	AbortedAt string       `json:"abortedAt,omitempty"`
	Steps     []StepResult `json:"steps"`
}

// StepResult records one step's request, response, and verdict.
type StepResult struct {
	Name     string     `json:"name"`
	Method   string     `json:"method"`
	Path     string     `json:"path"`
	Status   int        `json:"status"`
	Passed   bool       `json:"passed"`
	Failures []string   `json:"failures,omitempty"`
	Request  RecordedIO `json:"request"`
	Response RecordedIO `json:"response"`
}

// RecordedIO is one side of a step's HTTP exchange. Body is always valid
// JSON or nil: a non-JSON body is stored as a JSON string.
type RecordedIO struct {
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// JSON renders the report as indented JSON.
func (r *Report) JSON() ([]byte, error) {
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("scimtest: render report JSON: %w", err)
	}
	return out, nil
}

// Text renders the report as an aligned, human-readable summary with a
// PASS/FAIL line per step and the failure detail beneath the table.
func (r *Report) Text() string {
	var b strings.Builder
	verdict := "PASS"
	if !r.Passed {
		verdict = "FAIL"
	}
	passed := 0
	for i := range r.Steps {
		if r.Steps[i].Passed {
			passed++
		}
	}
	fmt.Fprintf(&b, "Scenario: %s\n", r.Scenario)
	fmt.Fprintf(&b, "Target:   %s\n", r.Target)
	fmt.Fprintf(&b, "Result:   %s (%d/%d steps)\n", verdict, passed, len(r.Steps))
	if r.Aborted {
		fmt.Fprintf(&b, "Aborted:  at step %q\n", r.AbortedAt)
	}
	b.WriteString("\n")
	writeStepTable(&b, r.Steps)
	writeFailures(&b, r.Steps)
	return b.String()
}

func writeStepTable(b *strings.Builder, steps []StepResult) {
	nameWidth := 0
	for i := range steps {
		if n := len(steps[i].Name); n > nameWidth {
			nameWidth = n
		}
	}
	for i := range steps {
		s := &steps[i]
		glyph := "PASS"
		if !s.Passed {
			glyph = "FAIL"
		}
		fmt.Fprintf(b, "  [%s] %-*s  %-6s %s  (%d)\n", glyph, nameWidth, s.Name, s.Method, s.Path, s.Status)
	}
}

func writeFailures(b *strings.Builder, steps []StepResult) {
	for i := range steps {
		s := &steps[i]
		for _, f := range s.Failures {
			fmt.Fprintf(b, "      %s: %s\n", s.Name, f)
		}
	}
}

// newRecordedIO buffers one side of an exchange for the report.
func newRecordedIO(headers map[string]string, body []byte) RecordedIO {
	return RecordedIO{Headers: headers, Body: jsonBody(body)}
}

// jsonBody normalizes a body to valid JSON: nil for empty, verbatim when
// already JSON, or a JSON string otherwise, so the report always marshals.
func jsonBody(b []byte) json.RawMessage {
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	if json.Valid(b) {
		return json.RawMessage(b)
	}
	quoted, err := json.Marshal(string(b))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return json.RawMessage(quoted)
}

// flattenHeaders reduces an http.Header to its first value per key.
func flattenHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k := range h {
		out[k] = h.Get(k)
	}
	return out
}
