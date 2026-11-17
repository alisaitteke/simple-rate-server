// Package provider defines the upstream source contract and a small HTTP
// helper shared by all providers.
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alisaitteke/simple-rate-server/internal/model"
)

// Provider fetches quotes from one upstream source.
type Provider interface {
	Name() string
	Class() model.Class
	Interval() time.Duration
	Fetch(ctx context.Context) ([]model.Edge, error)
}

// RetryAfterError tells the scheduler to back off for at least Duration.
type RetryAfterError struct {
	Duration time.Duration
	Err      error
}

func (e *RetryAfterError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("retry after %s", e.Duration)
	}
	return fmt.Sprintf("%s (retry after %s)", e.Err, e.Duration)
}

func (e *RetryAfterError) Unwrap() error { return e.Err }

// RetryDelay returns the requested delay when err carries one.
func RetryDelay(err error) time.Duration {
	var ra *RetryAfterError
	if errors.As(err, &ra) {
		return ra.Duration
	}
	return 0
}

const (
	maxBody       = 32 << 20 // 32 MiB
	maxErrSnippet = 200
)

// HTTPClient is a thin wrapper around net/http used by every provider.
type HTTPClient struct {
	Client    *http.Client
	UserAgent string
}

// NewHTTPClient builds a client with a hard request timeout.
func NewHTTPClient(timeout time.Duration, userAgent string) *HTTPClient {
	return &HTTPClient{
		Client:    &http.Client{Timeout: timeout},
		UserAgent: userAgent,
	}
}

// Get fetches url and returns the body. Non-2xx responses become errors;
// 429/503 with a Retry-After header become RetryAfterError.
func (c *HTTPClient) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json, application/xml, text/xml, */*")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		if d := parseRetryAfter(resp.Header.Get("Retry-After")); d > 0 {
			return nil, &RetryAfterError{
				Duration: d,
				Err:      fmt.Errorf("upstream %s: %s", url, resp.Status),
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("upstream %s: %s: %s", url, resp.Status, snippet(body))
	}
	return body, nil
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > maxErrSnippet {
		s = s[:maxErrSnippet] + "..."
	}
	return s
}
