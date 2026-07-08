package scimtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Simulate runs a scenario against a target SCIM server and returns a
// report. It uses raw HTTP, not the client package, so scenarios can send
// the deliberately non-compliant shapes real IdPs emit and assert on exact
// wire responses. Steps run in order and stop at the first failure — later
// steps depend on variables captured from earlier ones. The returned error
// is non-nil only for a malformed scenario, never for a step that fails
// (that is reported, not returned).
func Simulate(ctx context.Context, target, token string, scenario Scenario) (Report, error) {
	if err := validateScenario(scenario); err != nil {
		return Report{}, err
	}
	tr := newSimTransport(target, token)
	vars := make(map[string]string)
	report := Report{Scenario: scenario.Name, Target: target}

	for i := range scenario.Steps {
		step := &scenario.Steps[i]
		result := runStep(ctx, tr, step, vars)
		report.Steps = append(report.Steps, result)
		if !result.Passed {
			report.Aborted = true
			report.AbortedAt = step.Name
			break
		}
	}
	report.Passed = !report.Aborted
	return report, nil
}

// runStep resolves templates, sends the request, evaluates the response,
// and applies captures. Any failure short-circuits the rest of the step.
func runStep(ctx context.Context, tr *simTransport, step *Step, vars map[string]string) StepResult {
	res := StepResult{Name: step.Name, Method: step.Method}

	path, err := substitute(step.Path, vars)
	if err != nil {
		markFailed(&res, err.Error())
		return res
	}
	res.Path = path

	bodyStr, err := substitute(string(step.Body), vars)
	if err != nil {
		markFailed(&res, err.Error())
		return res
	}
	body := []byte(bodyStr)
	headers := stepHeaders(step, body)
	res.Request = newRecordedIO(headers, body)

	status, respHeaders, respBody, err := tr.do(ctx, step.Method, path, headers, body)
	if err != nil {
		markFailed(&res, "transport: "+err.Error())
		return res
	}
	res.Status = status
	res.Response = newRecordedIO(flattenHeaders(respHeaders), respBody)

	fails := evaluate(step.Expect, status, respHeaders, respBody)
	if len(fails) == 0 {
		if cerr := applyCaptures(step.Capture, respBody, vars); cerr != nil {
			fails = append(fails, "capture: "+cerr.Error())
		}
	}
	res.Failures = fails
	res.Passed = len(fails) == 0
	return res
}

// markFailed records one failure message on a step.
func markFailed(res *StepResult, msg string) {
	res.Passed = false
	res.Failures = append(res.Failures, msg)
}

// stepHeaders copies a step's headers, defaulting Content-Type for a body.
func stepHeaders(step *Step, body []byte) map[string]string {
	h := make(map[string]string, len(step.Headers)+1)
	for k, v := range step.Headers {
		h[k] = v
	}
	if len(body) > 0 && !hasHeader(h, "Content-Type") {
		h["Content-Type"] = mediaType
	}
	return h
}

func hasHeader(h map[string]string, key string) bool {
	for k := range h {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// mediaType is the SCIM content type sent by default on bodied requests.
const mediaType = "application/scim+json"

// simTransport issues single-shot SCIM requests: no retry, no redirect
// surprises, so a scenario observes exactly what the target returned.
type simTransport struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

func newSimTransport(target, token string) *simTransport {
	return &simTransport{
		httpClient: &http.Client{},
		baseURL:    strings.TrimRight(target, "/"),
		token:      token,
	}
}

func (t *simTransport) do(
	ctx context.Context, method, path string, headers map[string]string, body []byte,
) (int, http.Header, []byte, error) {
	reader := io.Reader(http.NoBody)
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.baseURL+path, reader)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", mediaType)
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, resp.Header, nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, resp.Header, respBody, nil
}
