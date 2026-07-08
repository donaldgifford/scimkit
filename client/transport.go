package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/donaldgifford/scimkit/scim"
)

const mediaType = "application/scim+json"

// rawResponse is one completed HTTP exchange.
type rawResponse struct {
	status int
	header http.Header
	body   []byte
}

// requestURL joins path segments onto the base URL, percent-escaping
// each segment so ids with reserved characters round-trip.
func (c *Client) requestURL(segments ...string) string {
	return c.baseURL.JoinPath(segments...).String()
}

// do executes an HTTP request with the retry policy. It returns the
// final non-retryable response (which may carry an error status the
// caller decodes), or a RetryExhaustedError when a retryable condition
// outlasts MaxAttempts, or the context error on cancellation. A fresh
// request is built from body on every attempt, so there is no shared
// request state to reset between retries.
func (c *Client) do(
	ctx context.Context, method, rawURL string, body []byte, headers map[string]string,
) (*rawResponse, error) {
	var last error
	for attempt := 1; attempt <= c.retry.MaxAttempts; attempt++ {
		resp, err := c.attempt(ctx, method, rawURL, body, headers)
		switch {
		case err != nil:
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("scimkit client: request aborted: %w", ctxErr)
			}
			last, resp = err, nil
		case !c.retry.shouldRetry(resp.status):
			return resp, nil
		default:
			last = decodeError(resp.status, resp.body)
		}
		if attempt == c.retry.MaxAttempts {
			break
		}
		if werr := c.waitBeforeRetry(ctx, attempt, resp); werr != nil {
			return nil, werr
		}
	}
	return nil, &RetryExhaustedError{Attempts: c.retry.MaxAttempts, Last: last}
}

// attempt performs one request/response cycle and reads the body.
func (c *Client) attempt(
	ctx context.Context, method, rawURL string, body []byte, headers map[string]string,
) (*rawResponse, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, fmt.Errorf("scimkit client: build request: %w", err)
	}
	c.setHeaders(req, body != nil, headers)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("scimkit client: read response body: %w", err)
	}
	return &rawResponse{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

// setHeaders applies the SCIM media type, optional auth, and any
// per-request headers.
func (c *Client) setHeaders(req *http.Request, hasBody bool, headers map[string]string) {
	req.Header.Set("Accept", mediaType)
	if hasBody {
		req.Header.Set("Content-Type", mediaType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

// waitBeforeRetry sleeps the backoff for the given attempt, letting a
// response's Retry-After header override the computed delay.
func (c *Client) waitBeforeRetry(ctx context.Context, attempt int, resp *rawResponse) error {
	delay := c.retry.backoffDelay(attempt)
	if resp != nil {
		if after, ok := parseRetryAfter(resp.header.Get("Retry-After"), time.Now()); ok {
			delay = min(after, c.retry.MaxDelay)
		}
	}
	c.logger.Debug("scim client retry", "attempt", attempt, "delay", delay)
	return c.clock.sleep(ctx, delay)
}

// doResource runs a request and decodes a successful body into a
// resource. A non-2xx status becomes the mapped error; an empty
// success body (204, or 200 with no content) yields a nil resource so
// the caller can decide whether to follow up.
func (c *Client) doResource(
	ctx context.Context, method, rawURL string, body []byte, rt *scim.ResourceType, headers map[string]string,
) (*scim.Resource, error) {
	resp, err := c.do(ctx, method, rawURL, body, headers)
	if err != nil {
		return nil, err
	}
	if resp.status >= 400 {
		return nil, decodeError(resp.status, resp.body)
	}
	if len(bytes.TrimSpace(resp.body)) == 0 {
		return nil, nil //nolint:nilnil // empty success body: caller decides whether to re-GET
	}
	res, err := scim.DecodeResource(resp.body, rt)
	if err != nil {
		return nil, fmt.Errorf("scimkit client: decode response: %w", err)
	}
	return res, nil
}

// isNotImplemented reports whether err is a 501 protocol error, the
// signal to fall back from PATCH to PUT.
func isNotImplemented(err error) bool {
	var scimErr *scim.Error
	return errors.As(err, &scimErr) && scimErr.Status == http.StatusNotImplemented
}
