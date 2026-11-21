// Package config loads the server configuration from flags and environment variables.
package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the complete runtime configuration.
type Config struct {
	// Spoolman upstream
	SpoolmanURL         string
	SpoolmanTimeout     time.Duration
	SpoolmanBearerToken string
	SpoolmanBasicUser   string
	SpoolmanBasicPass   string
	SpoolmanHeaders     map[string]string
	SpoolmanInsecureTLS bool
	SpoolmanCAFile      string

	// MCP transport
	Transport          string // "stdio" or "http"
	HTTPAddr           string
	HTTPPath           string
	HTTPStateless      bool
	HTTPJSONResponse   bool
	HTTPEnableSSE      bool
	HTTPSessionTimeout time.Duration
	AuthTokens         []string
	AllowNoAuth        bool

	// Behaviour
	ReadOnly     bool
	AllowDelete  bool
	ToolPrefix   string
	DefaultLimit int

	LogLevel  string
	LogFormat string
}

// envOr returns the value of the environment variable or the fallback.
func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return b
}

// secretFromEnv reads KEY or, if KEY_FILE is set, the trimmed file content.
func secretFromEnv(key string) (string, error) {
	if f := os.Getenv(key + "_FILE"); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", fmt.Errorf("reading %s_FILE: %w", key, err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	return os.Getenv(key), nil
}

// ParseHeaders parses "Name: value" pairs separated by newlines or ";".
func ParseHeaders(s string) (map[string]string, error) {
	out := map[string]string{}
	s = strings.ReplaceAll(s, "\r", "")
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == ';' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, ":")
		if !ok {
			name, value, ok = strings.Cut(part, "=")
		}
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid header %q, expected 'Name: value'", part)
		}
		out[name] = strings.TrimSpace(value)
	}
	return out, nil
}

func splitTokens(s string) []string {
	var out []string
	for _, t := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Load parses flags (args without program name) on top of environment defaults.
func Load(args []string) (*Config, error) {
	c := &Config{}
	fs := flag.NewFlagSet("spoolman-mcp", flag.ContinueOnError)

	fs.StringVar(&c.SpoolmanURL, "spoolman-url", envOr("SPOOLMAN_URL", ""), "Base URL of Spoolman, e.g. http://spoolman:7912 (env SPOOLMAN_URL)")
	timeout := fs.String("spoolman-timeout", envOr("SPOOLMAN_TIMEOUT", "30s"), "HTTP timeout for Spoolman requests (env SPOOLMAN_TIMEOUT)")
	fs.BoolVar(&c.SpoolmanInsecureTLS, "spoolman-insecure-skip-verify", envBool("SPOOLMAN_INSECURE_SKIP_VERIFY", false), "Skip TLS verification towards Spoolman (env SPOOLMAN_INSECURE_SKIP_VERIFY)")
	fs.StringVar(&c.SpoolmanCAFile, "spoolman-ca-file", envOr("SPOOLMAN_CA_FILE", ""), "Additional CA bundle (PEM) for Spoolman TLS (env SPOOLMAN_CA_FILE)")

	fs.StringVar(&c.Transport, "transport", envOr("MCP_TRANSPORT", "stdio"), "Transport: stdio or http (env MCP_TRANSPORT)")
	fs.StringVar(&c.HTTPAddr, "http-addr", envOr("MCP_HTTP_ADDR", ":8080"), "Listen address for http transport (env MCP_HTTP_ADDR)")
	fs.StringVar(&c.HTTPPath, "http-path", envOr("MCP_HTTP_PATH", "/mcp"), "Endpoint path for Streamable HTTP (env MCP_HTTP_PATH)")
	fs.BoolVar(&c.HTTPStateless, "http-stateless", envBool("MCP_HTTP_STATELESS", false), "Run Streamable HTTP in stateless mode (env MCP_HTTP_STATELESS)")
	fs.BoolVar(&c.HTTPJSONResponse, "http-json-response", envBool("MCP_HTTP_JSON_RESPONSE", false), "Answer with application/json instead of SSE streams (env MCP_HTTP_JSON_RESPONSE)")
	fs.BoolVar(&c.HTTPEnableSSE, "http-enable-sse", envBool("MCP_HTTP_ENABLE_SSE", true), "Also serve the legacy HTTP+SSE transport on /sse (env MCP_HTTP_ENABLE_SSE)")
	sessTimeout := fs.String("http-session-timeout", envOr("MCP_HTTP_SESSION_TIMEOUT", "30m"), "Idle timeout for HTTP sessions, 0 disables (env MCP_HTTP_SESSION_TIMEOUT)")
	fs.BoolVar(&c.AllowNoAuth, "allow-no-auth", envBool("MCP_ALLOW_NO_AUTH", false), "Allow http transport without bearer token (env MCP_ALLOW_NO_AUTH)")

	fs.BoolVar(&c.ReadOnly, "read-only", envBool("READ_ONLY", false), "Expose only read tools (env READ_ONLY)")
	fs.BoolVar(&c.AllowDelete, "allow-delete", envBool("ALLOW_DELETE", true), "Expose delete tools (env ALLOW_DELETE)")
	fs.StringVar(&c.ToolPrefix, "tool-prefix", envOr("TOOL_PREFIX", ""), "Optional prefix for all tool names, e.g. spoolman_ (env TOOL_PREFIX)")
	defLimit := fs.String("default-limit", envOr("DEFAULT_LIMIT", "50"), "Default page size for list tools, 0 = unlimited (env DEFAULT_LIMIT)")

	fs.StringVar(&c.LogLevel, "log-level", envOr("LOG_LEVEL", "info"), "debug, info, warn, error (env LOG_LEVEL)")
	fs.StringVar(&c.LogFormat, "log-format", envOr("LOG_FORMAT", "text"), "text or json (env LOG_FORMAT)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	var err error
	if c.SpoolmanTimeout, err = time.ParseDuration(*timeout); err != nil {
		return nil, fmt.Errorf("invalid SPOOLMAN_TIMEOUT: %w", err)
	}
	if c.HTTPSessionTimeout, err = time.ParseDuration(*sessTimeout); err != nil {
		return nil, fmt.Errorf("invalid MCP_HTTP_SESSION_TIMEOUT: %w", err)
	}
	if c.DefaultLimit, err = strconv.Atoi(*defLimit); err != nil || c.DefaultLimit < 0 {
		return nil, fmt.Errorf("invalid DEFAULT_LIMIT %q", *defLimit)
	}

	// Secrets only via environment / files, never via flags (would leak in ps).
	if c.SpoolmanBearerToken, err = secretFromEnv("SPOOLMAN_BEARER_TOKEN"); err != nil {
		return nil, err
	}
	c.SpoolmanBasicUser = os.Getenv("SPOOLMAN_BASIC_USER")
	if c.SpoolmanBasicPass, err = secretFromEnv("SPOOLMAN_BASIC_PASSWORD"); err != nil {
		return nil, err
	}
	hdrs, err := secretFromEnv("SPOOLMAN_HEADERS")
	if err != nil {
		return nil, err
	}
	if c.SpoolmanHeaders, err = ParseHeaders(hdrs); err != nil {
		return nil, fmt.Errorf("SPOOLMAN_HEADERS: %w", err)
	}
	tok, err := secretFromEnv("MCP_AUTH_TOKEN")
	if err != nil {
		return nil, err
	}
	c.AuthTokens = splitTokens(tok)

	return c, c.Validate()
}

// Validate checks the configuration for consistency.
func (c *Config) Validate() error {
	if c.SpoolmanURL == "" {
		return errors.New("SPOOLMAN_URL is required")
	}
	u, err := url.Parse(c.SpoolmanURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("SPOOLMAN_URL %q must be an absolute http(s) URL", c.SpoolmanURL)
	}
	c.SpoolmanURL = strings.TrimRight(c.SpoolmanURL, "/")
	c.SpoolmanURL = strings.TrimSuffix(c.SpoolmanURL, "/api/v1")

	if c.SpoolmanBearerToken != "" && c.SpoolmanBasicUser != "" {
		return errors.New("SPOOLMAN_BEARER_TOKEN and SPOOLMAN_BASIC_USER are mutually exclusive")
	}

	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	switch c.Transport {
	case "stdio":
	case "http", "streamable-http", "sse":
		c.Transport = "http"
		if !strings.HasPrefix(c.HTTPPath, "/") {
			c.HTTPPath = "/" + c.HTTPPath
		}
		if len(c.AuthTokens) == 0 && !c.AllowNoAuth {
			return errors.New("http transport requires MCP_AUTH_TOKEN (or set MCP_ALLOW_NO_AUTH=true to explicitly run without authentication)")
		}
		for _, t := range c.AuthTokens {
			if len(t) < 16 {
				return errors.New("MCP_AUTH_TOKEN values must be at least 16 characters long")
			}
		}
	default:
		return fmt.Errorf("unknown transport %q (use stdio or http)", c.Transport)
	}
	return nil
}
