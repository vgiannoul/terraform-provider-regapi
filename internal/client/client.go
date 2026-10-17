// Package client is a small client for the RegAPI REST API.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// DefaultEndpoint is the production API.
const DefaultEndpoint = "https://api.regapi.dev"

const (
	maxAttempts = 3
	baseDelay   = 500 * time.Millisecond
	maxDelay    = 30 * time.Second
	maxBody     = 1 << 20
)

// Usage says what still uses a resource (contact_in_use).
type Usage struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Role string `json:"role"`
}

// APIError is an error response of the RegAPI.
type APIError struct {
	Status  int
	Code    string
	Message string
	UsedBy  []Usage
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

// IsCode reports whether err is an API error with the given code.
func IsCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == code
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}

// Option customises a Client.
type Option func(*Client)

// WithSleep replaces the wait between retries; tests use it to avoid real waiting.
func WithSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(c *Client) { c.sleep = sleep }
}

// Client calls the RegAPI. It is safe for concurrent use.
type Client struct {
	endpoint, apiKey, userAgent string
	http                        *http.Client
	sleep                       func(context.Context, time.Duration) error
}

// New returns a Client for endpoint using apiKey; version goes into the User-Agent.
func New(endpoint, apiKey, version string, opts ...Option) *Client {
	c := &Client{
		endpoint: endpoint, apiKey: apiKey,
		userAgent: "terraform-provider-regapi/" + version,
		http:      &http.Client{Timeout: 60 * time.Second},
		sleep: func(ctx context.Context, d time.Duration) error {
			select {
			case <-time.After(d):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func newIdempotencyKey() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Sprintf("read random bytes: %v", err))
	}
	return "tf-" + hex.EncodeToString(raw)
}

// do sends one logical request, retrying 429, 5xx and network errors with the same
// idempotency key. in and out may be nil.
func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	payload, err := encodeBody(method, path, in)
	if err != nil {
		return err
	}
	key := ""
	if method == http.MethodPost || method == http.MethodPatch {
		key = newIdempotencyKey()
	}
	for attempt := 1; ; attempt++ {
		wait, err := c.attempt(ctx, attempt, method, path, key, payload, out)
		if err == nil || wait < 0 || attempt == maxAttempts {
			return err
		}
		if err := c.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func encodeBody(method, path string, in any) ([]byte, error) {
	if in == nil {
		return nil, nil
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("encode %s %s: %w", method, path, err)
	}
	return payload, nil
}

func (c *Client) newRequest(
	ctx context.Context, method, path, key string, payload []byte,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req, nil
}

// attempt sends the request once. A negative wait means the error is final.
func (c *Client) attempt(
	ctx context.Context, attempt int, method, path, key string, payload []byte, out any,
) (time.Duration, error) {
	req, err := c.newRequest(ctx, method, path, key, payload)
	if err != nil {
		return -1, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return -1, ctx.Err()
		}
		return backoff(attempt), fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return handleResponse(resp, attempt, method, path, out)
}

func handleResponse(
	resp *http.Response, attempt int, method, path string, out any,
) (time.Duration, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return backoff(attempt), fmt.Errorf("read %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 300 {
		return 0, decodeInto(method, path, raw, out)
	}
	apiErr := parseError(resp.StatusCode, raw)
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return retryDelay(resp, attempt), apiErr
	}
	return -1, apiErr
}

func decodeInto(method, path string, raw []byte, out any) error {
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}

func backoff(attempt int) time.Duration {
	return min(time.Duration(attempt)*baseDelay, maxDelay)
}

func retryDelay(resp *http.Response, attempt int) time.Duration {
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs >= 0 {
		return min(time.Duration(secs)*time.Second, maxDelay)
	}
	return backoff(attempt)
}

func parseError(status int, raw []byte) *APIError {
	var body struct {
		Error struct {
			Code    string  `json:"code"`
			Message string  `json:"message"`
			UsedBy  []Usage `json:"used_by"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Error.Code == "" {
		return &APIError{Status: status, Code: "http_" + strconv.Itoa(status),
			Message: "unexpected response from the RegAPI"}
	}
	return &APIError{Status: status, Code: body.Error.Code, Message: body.Error.Message,
		UsedBy: body.Error.UsedBy}
}
