package spoolman

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Regression: credentials must never follow a redirect to another origin
// (or be downgraded to plain HTTP), for any auth type and any 3xx status.
func TestNoRedirectFollow(t *testing.T) {
	var hits atomic.Int32
	var leaked atomic.Bool
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("CF-Access-Client-Secret") != "" || r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer sink.Close()
	target := strings.Replace(sink.URL, "127.0.0.1", "localhost", 1)

	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target+"/leak", code)
		}))
		for _, o := range []Options{
			{BaseURL: up.URL, BearerToken: "dummy-bearer", Headers: map[string]string{"CF-Access-Client-Secret": "dummy-key"}},
			{BaseURL: up.URL, BasicUser: "u", BasicPassword: "dummy-pass"},
		} {
			c, err := New(o)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range []string{http.MethodGet, http.MethodDelete} {
				_, err = c.Do(context.Background(), m, "/spool/1/tag/x", nil, nil)
				var ae *APIError
				if !errors.As(err, &ae) || ae.Status != code || !strings.Contains(err.Error(), "redirect not followed") {
					t.Errorf("%d %s: expected redirect APIError, got %v", code, m, err)
				}
			}
		}
		up.Close()
	}
	if hits.Load() != 0 || leaked.Load() {
		t.Fatalf("redirect target contacted %d times (credentials leaked: %v)", hits.Load(), leaked.Load())
	}
}

// Regression: dot segments and slashes must never reach an upstream path.
func TestPathSegment(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "../", "a/b", `a\b`, "..%2F", "x\n"} {
		if bad == "..%2F" {
			// encoded slash is literal text here; PathSegment escapes the '%', which is safe
			if s, err := PathSegment(bad); err != nil || s != "..%252F" {
				t.Errorf("%q -> %q, %v", bad, s, err)
			}
			continue
		}
		if _, err := PathSegment(bad); err == nil {
			t.Errorf("PathSegment(%q) must fail", bad)
		}
	}
	for in, want := range map[string]string{"currency": "currency", "04:A2:B3": "04:A2:B3", "Drybox 1": "Drybox%201", "a?b#c": "a%3Fb%23c"} {
		if got, err := PathSegment(in); err != nil || got != want {
			t.Errorf("PathSegment(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

// Regression: oversized responses are an error, not silently truncated.
func TestOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("a", 1<<20)
		for i := 0; i < 33; i++ {
			_, _ = io.WriteString(w, chunk)
		}
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL})
	if _, err := c.Get(context.Background(), "/export/spools", nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected size error, got %v", err)
	}
}

func TestErrorBodyCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, strings.Repeat("e", 1<<20))
	}))
	defer srv.Close()
	c, _ := New(Options{BaseURL: srv.URL})
	_, err := c.Get(context.Background(), "/x", nil)
	ae, ok := err.(*APIError)
	if !ok || len(ae.Body) > maxErrorBodyBytes || len(err.Error()) > 2200 {
		t.Errorf("error body not capped: ok=%v len=%d", ok, len(err.Error()))
	}
}
