// Package spoolman is a small HTTP client for the Spoolman REST API (v1).
package spoolman

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// maxResponseBytes caps the size of responses read from Spoolman.
const maxResponseBytes = 32 << 20

// Options configures a Client.
type Options struct {
	BaseURL            string
	Timeout            time.Duration
	BearerToken        string
	BasicUser          string
	BasicPassword      string
	Headers            map[string]string
	InsecureSkipVerify bool
	CAFile             string
	UserAgent          string
}

// Client talks to Spoolman's /api/v1 endpoints.
type Client struct {
	base string
	http *http.Client
	opts Options
}

// APIError is returned for non-2xx responses.
type APIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	msg := strings.TrimSpace(e.Body)
	// Prefer FastAPI's {"message": ...} / {"detail": ...} if present.
	var m map[string]any
	if json.Unmarshal([]byte(e.Body), &m) == nil {
		if v, ok := m["message"]; ok {
			msg = fmt.Sprint(v)
		} else if v, ok := m["detail"]; ok {
			b, _ := json.Marshal(v)
			msg = string(b)
		}
	}
	if len(msg) > 2000 {
		msg = msg[:2000] + "…"
	}
	return fmt.Sprintf("spoolman %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, msg)
}

// IsNotFound reports whether err is a 404 from Spoolman.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Status == http.StatusNotFound
}

// New creates a client.
func New(o Options) (*Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.InsecureSkipVerify || o.CAFile != "" {
		tc := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: o.InsecureSkipVerify} //nolint:gosec // opt-in
		if o.CAFile != "" {
			pem, err := os.ReadFile(o.CAFile)
			if err != nil {
				return nil, fmt.Errorf("reading CA file: %w", err)
			}
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("CA file contains no valid certificates")
			}
			tc.RootCAs = pool
		}
		tr.TLSClientConfig = tc
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	if o.UserAgent == "" {
		o.UserAgent = "spoolman-mcp-go"
	}
	return &Client{
		base: strings.TrimRight(o.BaseURL, "/") + "/api/v1",
		http: &http.Client{Timeout: o.Timeout, Transport: tr},
		opts: o,
	}, nil
}

// Response is a raw API response.
type Response struct {
	Status     int
	Body       []byte
	TotalCount int // from X-Total-Count, -1 if absent
	Header     http.Header
}

// Do executes a request. body may be nil, a []byte (sent verbatim as JSON) or any JSON-marshalable value.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) (*Response, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		var b []byte
		switch v := body.(type) {
		case []byte:
			b = v
		case json.RawMessage:
			b = v
		default:
			var err error
			if b, err = json.Marshal(v); err != nil {
				return nil, fmt.Errorf("encoding request body: %w", err)
			}
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.opts.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.opts.Headers {
		req.Header.Set(k, v)
	}
	switch {
	case c.opts.BearerToken != "":
		req.Header.Set("Authorization", "Bearer "+c.opts.BearerToken)
	case c.opts.BasicUser != "":
		req.SetBasicAuth(c.opts.BasicUser, c.opts.BasicPassword)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("spoolman %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading spoolman response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &APIError{Method: method, Path: path, Status: resp.StatusCode, Body: string(data)}
	}
	total := -1
	if tc := resp.Header.Get("X-Total-Count"); tc != "" {
		if n, err := strconv.Atoi(tc); err == nil {
			total = n
		}
	}
	return &Response{Status: resp.StatusCode, Body: data, TotalCount: total, Header: resp.Header}, nil
}

// Get is a convenience wrapper.
func (c *Client) Get(ctx context.Context, path string, q url.Values) (*Response, error) {
	return c.Do(ctx, http.MethodGet, path, q, nil)
}

// Health checks Spoolman's health endpoint.
func (c *Client) Health(ctx context.Context) error {
	_, err := c.Get(ctx, "/health", nil)
	return err
}

// PathEscape escapes a single path segment.
func PathEscape(s string) string { return url.PathEscape(s) }
