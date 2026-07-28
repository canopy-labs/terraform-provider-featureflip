// Package client is a minimal hand-written client for the Featureflip
// public Management API (/api/v1).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const apiPrefix = "/api/v1"

type Client struct {
	baseURL     *url.URL
	token       string
	Org         string
	httpClient  *http.Client
	userAgent   string
	maxRetries  int
	backoffBase time.Duration
}

func New(baseURL, token, org, version string) (*Client, error) {
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid base_url %q", baseURL)
	}
	return &Client{
		baseURL:     u,
		token:       token,
		Org:         org,
		httpClient:  &http.Client{Timeout: 60 * time.Second},
		userAgent:   "terraform-provider-featureflip/" + version,
		maxRetries:  4,
		backoffBase: 500 * time.Millisecond,
	}, nil
}

// APIError is the public API's snake_case error envelope plus HTTP status.
type APIError struct {
	StatusCode int                 `json:"-"`
	Code       string              `json:"error"`
	Message    string              `json:"message"`
	DocsURL    string              `json:"docs_url"`
	Fields     map[string][]string `json:"fields"`
	RetryAfter *int                `json:"retry_after"`
	DidYouMean []string            `json:"did_you_mean"`
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("Featureflip API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
	if len(e.DidYouMean) > 0 {
		msg += fmt.Sprintf(" — did you mean: %s?", strings.Join(e.DidYouMean, ", "))
	}
	if len(e.Fields) > 0 {
		keys := make([]string, 0, len(e.Fields))
		for k := range e.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s: %s", k, strings.Join(e.Fields[k], "; ")))
		}
		msg += " — " + strings.Join(parts, " | ")
	}
	return msg
}

func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusNotFound
}

// HasCode reports whether err is an APIError whose error code, message, or
// any field-validation message contains the given upstream guard code
// (e.g. "LAST_ENVIRONMENT"). Guard codes are embedded in validation-failed
// field messages, not in the top-level message.
func HasCode(err error, code string) bool {
	var ae *APIError
	if !errors.As(err, &ae) {
		return false
	}
	if strings.Contains(ae.Code, code) || strings.Contains(ae.Message, code) {
		return true
	}
	for _, msgs := range ae.Fields {
		for _, m := range msgs {
			if strings.Contains(m, code) {
				return true
			}
		}
	}
	return false
}

type reqOpts struct{ idempotent bool }

type Option func(*reqOpts)

// Idempotent marks a request as a create POST: a stable Idempotency-Key
// header is generated once and reused across retries.
func Idempotent() Option { return func(o *reqOpts) { o.idempotent = true } }

func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any, opts ...Option) error {
	var o reqOpts
	for _, fn := range opts {
		fn(&o)
	}

	var payload []byte
	if in != nil {
		var err error
		if payload, err = json.Marshal(in); err != nil {
			return err
		}
	}

	reqURL := c.baseURL.String() + apiPrefix + path
	if query != nil {
		reqURL += "?" + query.Encode()
	}

	idemKey := ""
	if o.idempotent {
		idemKey = uuid.NewString()
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, c.backoff(attempt, lastErr)); err != nil {
				return err
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		if in != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("User-Agent", c.userAgent)
		if idemKey != "" {
			req.Header.Set("Idempotency-Key", idemKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil && len(body) > 0 {
				return json.Unmarshal(body, out)
			}
			return nil
		}

		apiErr := &APIError{StatusCode: resp.StatusCode, Code: "unknown", Message: strings.TrimSpace(string(body))}
		_ = json.Unmarshal(body, apiErr) // best-effort: non-JSON bodies keep the raw-text fallback
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(resp.StatusCode)
		}
		lastErr = apiErr
		if !retryable(resp.StatusCode) {
			return apiErr
		}
	}
	return lastErr
}

func retryable(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable:
		return true
	}
	return false
}

const maxBackoff = 60 * time.Second

func (c *Client) backoff(attempt int, lastErr error) time.Duration {
	var ae *APIError
	if errors.As(lastErr, &ae) && ae.RetryAfter != nil {
		d := time.Duration(*ae.RetryAfter) * time.Second
		if d > maxBackoff {
			d = maxBackoff
		}
		return d
	}
	d := c.backoffBase * time.Duration(1<<(attempt-1))
	d += time.Duration(rand.Int64N(int64(d/2) + 1))
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func listAll[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var all []T
	cursor := ""
	for {
		q := url.Values{"limit": {"100"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var p page[T]
		if err := c.do(ctx, http.MethodGet, path, q, nil, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Items...)
		if p.NextCursor == nil || *p.NextCursor == "" {
			return all, nil
		}
		cursor = *p.NextCursor
	}
}

// orgPath builds /orgs/{org}/<parts…> with each part path-escaped.
func (c *Client) orgPath(parts ...string) string {
	segs := []string{"", "orgs", url.PathEscape(c.Org)}
	for _, p := range parts {
		segs = append(segs, url.PathEscape(p))
	}
	return strings.Join(segs, "/")
}

func pathEscape(s string) string { return url.PathEscape(s) }
