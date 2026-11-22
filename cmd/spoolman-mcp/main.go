// Command spoolman-mcp is a Model Context Protocol server for Spoolman.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/virtualben89/spoolman-mcp-go/internal/config"
	"github.com/virtualben89/spoolman-mcp-go/internal/spoolman"
	"github.com/virtualben89/spoolman-mcp-go/internal/tools"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "none"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("spoolman-mcp %s (%s)\n", version, commit)
			return
		case "healthcheck":
			os.Exit(healthcheck())
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// healthcheck probes the local /healthz endpoint (for Docker HEALTHCHECK in scratch images).
func healthcheck() int {
	if t := strings.ToLower(os.Getenv("MCP_TRANSPORT")); t == "" || t == "stdio" {
		// Nothing to probe for stdio; the process lifetime is the health signal.
		return 0
	}
	addr := os.Getenv("MCP_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort(host, port) + "/healthz") // #nosec G704 -- fixed loopback health probe built from operator config
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func newLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	// Always log to stderr: stdout belongs to the stdio transport.
	if strings.EqualFold(format, "json") {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func run() error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(log)

	client, err := spoolman.New(spoolman.Options{
		BaseURL:            cfg.SpoolmanURL,
		Timeout:            cfg.SpoolmanTimeout,
		BearerToken:        cfg.SpoolmanBearerToken,
		BasicUser:          cfg.SpoolmanBasicUser,
		BasicPassword:      cfg.SpoolmanBasicPass,
		Headers:            cfg.SpoolmanHeaders,
		InsecureSkipVerify: cfg.SpoolmanInsecureTLS,
		CAFile:             cfg.SpoolmanCAFile,
		UserAgent:          "spoolman-mcp-go/" + version,
	})
	if err != nil {
		return err
	}

	toolOpts := tools.Options{
		ReadOnly:       cfg.ReadOnly,
		AllowDelete:    cfg.AllowDelete,
		Prefix:         cfg.ToolPrefix,
		DefaultLimit:   cfg.DefaultLimit,
		MaxResultBytes: cfg.MaxResultBytes,
	}
	server, names := NewServer(client, toolOpts, log)
	log.Info("spoolman-mcp starting", "version", version, "transport", cfg.Transport,
		"spoolman", redactURL(cfg.SpoolmanURL), "tools", len(names), "read_only", cfg.ReadOnly, "allow_delete", cfg.AllowDelete && !cfg.ReadOnly)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Non-fatal connectivity probe.
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := client.Health(pctx); err != nil {
		log.Warn("spoolman not reachable at startup", "err", err)
	}
	cancel()

	if cfg.Transport == "stdio" {
		err := server.Run(ctx, &mcp.StdioTransport{})
		if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, io.EOF)) {
			return nil
		}
		return err
	}
	return serveHTTP(ctx, cfg, server, client, log)
}

// NewServer builds the MCP server with all enabled tools.
func NewServer(client *spoolman.Client, opts tools.Options, log *slog.Logger) (*mcp.Server, []string) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "spoolman-mcp",
		Title:   "Spoolman",
		Version: version,
	}, &mcp.ServerOptions{
		Instructions: "Tools for Spoolman, a 3D printing filament inventory. Concepts: vendor (manufacturer) -> filament (product type: material, color, density, weight) -> spool (physical roll with remaining weight and location). " +
			"Start with search or list_spool_groups to find IDs. Weights are grams, lengths millimetres. Use use_spool to book consumption and measure_spool for scale readings; prefer archiving spools over deleting them.",
		Logger: log,
	})
	names := tools.Register(server, client, opts)
	return server, names
}

func bearerVerifier(tokens []string) auth.TokenVerifier {
	hashes := make([][32]byte, len(tokens))
	for i, t := range tokens {
		hashes[i] = sha256.Sum256([]byte(t))
	}
	return func(_ context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		h := sha256.Sum256([]byte(token))
		match := 0
		for _, ref := range hashes {
			match |= subtle.ConstantTimeCompare(h[:], ref[:])
		}
		if match != 1 {
			return nil, auth.ErrInvalidToken
		}
		// Static tokens do not expire; give the middleware a rolling expiry.
		// UserID binds Streamable HTTP sessions to the token that created them.
		return &auth.TokenInfo{Expiration: time.Now().Add(time.Hour), UserID: tokenID(h)}, nil
	}
}

// tokenID derives a non-secret identifier from a token hash.
func tokenID(h [32]byte) string { return "token-" + hex.EncodeToString(h[:6]) }

// redactURL hides a userinfo password in a URL before logging it.
func redactURL(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return "<invalid url>"
	}
	return u.Redacted()
}

// limitConcurrency rejects requests beyond max concurrent in-flight MCP requests
// (including long-lived SSE streams) with 503.
func limitConcurrency(max int, next http.Handler) http.Handler {
	if max <= 0 {
		return next
	}
	sem := make(chan struct{}, max)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "5")
			http.Error(w, "too many concurrent requests", http.StatusServiceUnavailable)
		}
	})
}

func serveHTTP(ctx context.Context, cfg *config.Config, server *mcp.Server, client *spoolman.Client, log *slog.Logger) error {
	if len(cfg.AuthTokens) == 0 {
		log.Warn("HTTP transport running WITHOUT authentication (MCP_ALLOW_NO_AUTH=true)")
	}
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           buildHandler(cfg, server, client, log),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		// No ReadTimeout/WriteTimeout: SSE responses are long-lived streams.
		// Request bodies are capped by the SDK (4 MiB) and in-flight requests
		// by MCP_HTTP_MAX_CONCURRENT.
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "path", cfg.HTTPPath, "sse", cfg.HTTPEnableSSE, "auth", len(cfg.AuthTokens) > 0)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}

// buildHandler assembles the HTTP routes: MCP endpoints (auth-protected) and health probes.
func buildHandler(cfg *config.Config, server *mcp.Server, client *spoolman.Client, log *slog.Logger) http.Handler {
	getServer := func(*http.Request) *mcp.Server { return server }

	authn := func(h http.Handler) http.Handler { return h }
	if len(cfg.AuthTokens) > 0 {
		authn = auth.RequireBearerToken(bearerVerifier(cfg.AuthTokens), &auth.RequireBearerTokenOptions{})
	}
	// Auth first, then the concurrency limit, so unauthenticated clients cannot use up slots.
	protect := func(h http.Handler) http.Handler { return authn(limitConcurrency(cfg.HTTPMaxConcurrent, h)) }

	mux := http.NewServeMux()
	streamable := mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{
		Stateless:      cfg.HTTPStateless,
		JSONResponse:   cfg.HTTPJSONResponse,
		SessionTimeout: cfg.HTTPSessionTimeout,
		Logger:         log,
	})
	mux.Handle(cfg.HTTPPath, protect(streamable))
	if !strings.HasSuffix(cfg.HTTPPath, "/") {
		mux.Handle(cfg.HTTPPath+"/", protect(streamable))
	}
	if cfg.HTTPEnableSSE {
		sse := mcp.NewSSEHandler(getServer, nil)
		mux.Handle("/sse", protect(sse))
		mux.Handle("/sse/", protect(sse))
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := client.Health(c); err != nil {
			http.Error(w, "spoolman unreachable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "ok\n")
	})

	return logRequests(log, mux)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		lvl := slog.LevelDebug
		if sw.status == http.StatusUnauthorized || sw.status == http.StatusForbidden {
			lvl = slog.LevelWarn
		}
		path := r.URL.Path
		if len(path) > 200 {
			path = path[:200] + "…"
		}
		log.Log(r.Context(), lvl, "http", "method", r.Method, "path", path, "status", sw.status,
			"remote", r.RemoteAddr, "duration", time.Since(start).Round(time.Millisecond))
	})
}
