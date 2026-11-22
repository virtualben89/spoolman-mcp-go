package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/virtualben89/spoolman-mcp-go/internal/config"
)

const otherToken = "other-token-abcdef0123456789"

func rawPost(t *testing.T, url, token, session, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp, resp.Header.Get("Mcp-Session-Id")
}

// Regression/hardening: a Streamable HTTP session is bound to the token that created it.
func TestSessionBoundToToken(t *testing.T) {
	ts := setup(t, &config.Config{AuthTokens: []string{testToken, otherToken}})
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	resp, sid := rawPost(t, ts.URL+"/mcp", testToken, "", init)
	if resp.StatusCode != http.StatusOK || sid == "" {
		t.Fatalf("initialize: %d sid=%q", resp.StatusCode, sid)
	}
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	if resp, _ := rawPost(t, ts.URL+"/mcp", otherToken, sid, list); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign token on session: got %d, want 403", resp.StatusCode)
	}
	if resp, _ := rawPost(t, ts.URL+"/mcp", testToken, sid, list); resp.StatusCode != http.StatusOK {
		t.Errorf("own token on session: got %d", resp.StatusCode)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	h := limitConcurrency(1, slow)
	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		rec := &nopWriter{h: http.Header{}}
		h.ServeHTTP(rec, (&http.Request{}).WithContext(context.Background()))
	}()
	time.Sleep(50 * time.Millisecond)
	rec := &nopWriter{h: http.Header{}}
	h.ServeHTTP(rec, (&http.Request{}).WithContext(context.Background()))
	if rec.code != http.StatusServiceUnavailable {
		t.Errorf("second request: got %d, want 503", rec.code)
	}
	once.Do(func() { close(release) })
	<-srvDone
}

type nopWriter struct {
	h    http.Header
	code int
}

func (n *nopWriter) Header() http.Header         { return n.h }
func (n *nopWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *nopWriter) WriteHeader(c int)           { n.code = c }

func TestRedactURL(t *testing.T) {
	if got := redactURL("https://alice:S3cret@h:1"); strings.Contains(got, "S3cret") {
		t.Errorf("not redacted: %s", got)
	}
}
