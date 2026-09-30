package internalauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cortexai/internal/platform/httpx"
)

// Client calls another internal service with the shared token and the current
// request id attached.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClient returns a Client with a sensible timeout.
func NewClient(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Do sends a JSON request and decodes a JSON response into out (if non-nil).
// Non-2xx responses are returned as *httpx.Error so callers can pass them
// straight through to their own clients.
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set(httpx.HeaderInternalToken, c.Token)
	if id := httpx.RequestID(ctx); id != "" {
		req.Header.Set(httpx.HeaderRequestID, id)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer httpx.DrainAndClose(resp.Body)

	if resp.StatusCode >= 300 {
		apiErr := &httpx.Error{}
		if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(apiErr) != nil || apiErr.Code == "" {
			apiErr = &httpx.Error{Code: "upstream_error", Message: "Upstream service error."}
		}
		apiErr.Status = resp.StatusCode
		return apiErr
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("%s %s: decode: %w", method, path, err)
		}
	}
	return nil
}

// IsStatus reports whether err is an *httpx.Error with the given status.
func IsStatus(err error, status int) bool {
	var apiErr *httpx.Error
	return errors.As(err, &apiErr) && apiErr.Status == status
}
