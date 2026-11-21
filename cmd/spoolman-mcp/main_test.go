package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/virtualben89/spoolman-mcp-go/internal/config"
	"github.com/virtualben89/spoolman-mcp-go/internal/spoolman"
	"github.com/virtualben89/spoolman-mcp-go/internal/tools"
)

const testToken = "test-token-0123456789abcdef"

type headerRT struct {
	token string
	next  http.RoundTripper
}

func (h headerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if h.token != "" {
		r.Header.Set("Authorization", "Bearer "+h.token)
	}
	return h.next.RoundTrip(r)
}

func setup(t *testing.T, cfg *config.Config) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/health":
			_, _ = io.WriteString(w, `{"status":"healthy"}`)
		case "/api/v1/info":
			_, _ = io.WriteString(w, `{"version":"test"}`)
		default:
			_, _ = io.WriteString(w, `[]`)
		}
	}))
	t.Cleanup(upstream.Close)
	client, err := spoolman.New(spoolman.Options{BaseURL: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server, _ := NewServer(client, tools.Options{AllowDelete: true}, log)
	if cfg.HTTPPath == "" {
		cfg.HTTPPath = "/mcp"
	}
	ts := httptest.NewServer(buildHandler(cfg, server, client, log))
	t.Cleanup(ts.Close)
	return ts
}

func connectHTTP(ctx context.Context, url, token string, sse bool) (*mcp.ClientSession, error) {
	hc := &http.Client{Transport: headerRT{token: token, next: http.DefaultTransport}, Timeout: 10 * time.Second}
	var tr mcp.Transport
	if sse {
		tr = &mcp.SSEClientTransport{Endpoint: url, HTTPClient: hc}
	} else {
		tr = &mcp.StreamableClientTransport{Endpoint: url, HTTPClient: hc}
	}
	return mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, tr, nil)
}

func TestHTTPAuth(t *testing.T) {
	ts := setup(t, &config.Config{AuthTokens: []string{testToken}, HTTPEnableSSE: true})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Health endpoints are public.
	for _, p := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %v %v", p, err, resp)
		}
		resp.Body.Close()
	}

	// Raw request without/with wrong token -> 401.
	for _, tok := range []string{"", "wrong-token-xxxxxxxxxxxx"} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", strings.NewReader(`{}`))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("token %q: got %d, want 401", tok, resp.StatusCode)
		}
	}
	if _, err := connectHTTP(ctx, ts.URL+"/mcp", "", false); err == nil {
		t.Error("client without token connected")
	}

	// Streamable HTTP and legacy SSE with valid token.
	for _, tc := range []struct {
		path string
		sse  bool
	}{{"/mcp", false}, {"/sse", true}} {
		cs, err := connectHTTP(ctx, ts.URL+tc.path, testToken, tc.sse)
		if err != nil {
			t.Fatalf("%s: connect: %v", tc.path, err)
		}
		res, err := cs.ListTools(ctx, nil)
		if err != nil || len(res.Tools) < 30 {
			t.Fatalf("%s: list tools: %v (%d)", tc.path, err, len(res.Tools))
		}
		cr, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_info"})
		if err != nil || cr.IsError {
			t.Fatalf("%s: get_info: %v %+v", tc.path, err, cr)
		}
		_ = cs.Close()
	}
}

func TestHTTPStatelessJSON(t *testing.T) {
	ts := setup(t, &config.Config{AuthTokens: []string{testToken}, HTTPStateless: true, HTTPJSONResponse: true})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cs, err := connectHTTP(ctx, ts.URL+"/mcp", testToken, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if _, err := cs.ListTools(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

func TestConfigValidation(t *testing.T) {
	t.Setenv("SPOOLMAN_URL", "http://spoolman:7912/api/v1/")
	t.Setenv("MCP_TRANSPORT", "http")
	t.Setenv("MCP_AUTH_TOKEN", "")
	if _, err := config.Load(nil); err == nil || !strings.Contains(err.Error(), "MCP_AUTH_TOKEN") {
		t.Errorf("http without token must fail: %v", err)
	}
	t.Setenv("MCP_AUTH_TOKEN", "short")
	if _, err := config.Load(nil); err == nil {
		t.Error("short token must fail")
	}
	t.Setenv("MCP_AUTH_TOKEN", testToken+","+testToken+"2")
	cfg, err := config.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpoolmanURL != "http://spoolman:7912" || len(cfg.AuthTokens) != 2 {
		t.Errorf("cfg: %+v", cfg)
	}
	t.Setenv("MCP_AUTH_TOKEN", "")
	t.Setenv("MCP_ALLOW_NO_AUTH", "true")
	if _, err := config.Load(nil); err != nil {
		t.Errorf("explicit no-auth must be allowed: %v", err)
	}
	t.Setenv("MCP_TRANSPORT", "stdio")
	t.Setenv("MCP_ALLOW_NO_AUTH", "")
	if _, err := config.Load(nil); err != nil {
		t.Errorf("stdio needs no token: %v", err)
	}
	h, err := config.ParseHeaders("X-Api-Key: abc; CF-Access-Client-Id: id")
	if err != nil || h["X-Api-Key"] != "abc" || h["CF-Access-Client-Id"] != "id" {
		t.Errorf("headers: %v %v", h, err)
	}
}

func TestUpstreamAuthHeaders(t *testing.T) {
	var got http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = io.WriteString(w, `{}`)
	}))
	defer up.Close()
	c, _ := spoolman.New(spoolman.Options{BaseURL: up.URL, BearerToken: "up-token", Headers: map[string]string{"X-Extra": "1"}})
	if _, err := c.Get(context.Background(), "/info", nil); err != nil {
		t.Fatal(err)
	}
	if got.Get("Authorization") != "Bearer up-token" || got.Get("X-Extra") != "1" {
		t.Errorf("headers: %v", got)
	}
	c, _ = spoolman.New(spoolman.Options{BaseURL: up.URL, BasicUser: "u", BasicPassword: "p"})
	_, _ = c.Get(context.Background(), "/info", nil)
	if !strings.HasPrefix(got.Get("Authorization"), "Basic ") {
		t.Errorf("basic auth missing: %v", got)
	}
}
