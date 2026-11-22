package config

import (
	"strings"
	"testing"
)

func baseEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"SPOOLMAN_URL", "MCP_TRANSPORT", "MCP_AUTH_TOKEN", "MCP_ALLOW_NO_AUTH", "READ_ONLY", "ALLOW_DELETE",
		"SPOOLMAN_BEARER_TOKEN", "SPOOLMAN_BASIC_USER", "SPOOLMAN_BASIC_PASSWORD", "SPOOLMAN_HEADERS", "MCP_HTTP_ENABLE_SSE"} {
		t.Setenv(k, "")
	}
	t.Setenv("SPOOLMAN_URL", "http://spoolman:7912")
}

// Regression: unparseable security switches must fail instead of falling back to the permissive default.
func TestUnparseableBoolFails(t *testing.T) {
	for _, kv := range [][2]string{{"READ_ONLY", "maybe"}, {"ALLOW_DELETE", "nope"}, {"MCP_HTTP_ENABLE_SSE", "disabled"}} {
		baseEnv(t)
		t.Setenv(kv[0], kv[1])
		if _, err := Load(nil); err == nil || !strings.Contains(err.Error(), kv[0]) {
			t.Errorf("%s=%s: expected error naming the variable, got %v", kv[0], kv[1], err)
		}
	}
}

func TestBoolSpellings(t *testing.T) {
	cases := []struct {
		ro, del string
		wantRO  bool
		wantDel bool
	}{
		{"yes", "no", true, false},
		{"on", "off", true, false},
		{"TRUE", "False", true, false},
		{"0", "1", false, true},
		{"", "", false, true},
	}
	for _, c := range cases {
		baseEnv(t)
		t.Setenv("READ_ONLY", c.ro)
		t.Setenv("ALLOW_DELETE", c.del)
		cfg, err := Load(nil)
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if cfg.ReadOnly != c.wantRO || cfg.AllowDelete != c.wantDel {
			t.Errorf("%+v: got ro=%v del=%v", c, cfg.ReadOnly, cfg.AllowDelete)
		}
	}
	baseEnv(t)
	cfg, err := Load([]string{"-read-only=yes", "-allow-delete=off"})
	if err != nil || !cfg.ReadOnly || cfg.AllowDelete {
		t.Errorf("flags yes/off: %+v %v", cfg, err)
	}
	if _, err := Load([]string{"-read-only=maybe"}); err == nil {
		t.Error("flag -read-only=maybe must fail")
	}
}

// Regression: credentials in SPOOLMAN_URL are moved into Basic auth and never kept in the URL.
func TestURLUserinfoMovedToBasic(t *testing.T) {
	baseEnv(t)
	t.Setenv("SPOOLMAN_URL", "https://alice:S3cretPw@spoolman.example:7912/api/v1/")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.SpoolmanURL, "S3cretPw") || strings.Contains(cfg.SpoolmanURL, "alice") {
		t.Errorf("URL still carries credentials: %s", cfg.SpoolmanURL)
	}
	if cfg.SpoolmanURL != "https://spoolman.example:7912" || cfg.SpoolmanBasicUser != "alice" || cfg.SpoolmanBasicPass != "S3cretPw" {
		t.Errorf("unexpected cfg: url=%s user=%s", cfg.SpoolmanURL, cfg.SpoolmanBasicUser)
	}

	baseEnv(t)
	t.Setenv("SPOOLMAN_URL", "https://alice:S3cretPw@spoolman.example")
	t.Setenv("SPOOLMAN_BEARER_TOKEN", "tok")
	if _, err := Load(nil); err == nil {
		t.Error("userinfo + bearer token must be rejected")
	}
}

func TestErrorsNeverContainSecrets(t *testing.T) {
	baseEnv(t)
	t.Setenv("SPOOLMAN_URL", "ftp://alice:S3cretPw@spoolman.example")
	_, err := Load(nil)
	if err == nil || strings.Contains(err.Error(), "S3cretPw") {
		t.Errorf("scheme error leaks password: %v", err)
	}
	baseEnv(t)
	t.Setenv("SPOOLMAN_HEADERS", "CF-Access-Client-SecretTopSecretValue")
	_, err = Load(nil)
	if err == nil || strings.Contains(err.Error(), "TopSecretValue") {
		t.Errorf("header error leaks value: %v", err)
	}
}

func TestURLQueryRejected(t *testing.T) {
	baseEnv(t)
	t.Setenv("SPOOLMAN_URL", "http://spoolman:7912/?x=1")
	if _, err := Load(nil); err == nil {
		t.Error("query in SPOOLMAN_URL must be rejected")
	}
}

func TestAuthorizationHeaderConflict(t *testing.T) {
	baseEnv(t)
	t.Setenv("SPOOLMAN_HEADERS", "Authorization: Bearer x")
	t.Setenv("SPOOLMAN_BEARER_TOKEN", "y")
	if _, err := Load(nil); err == nil {
		t.Error("Authorization header + bearer token must be rejected")
	}
}

func TestLimits(t *testing.T) {
	baseEnv(t)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxResultBytes != 4<<20 || cfg.HTTPMaxConcurrent != 64 {
		t.Errorf("defaults: %d %d", cfg.MaxResultBytes, cfg.HTTPMaxConcurrent)
	}
	t.Setenv("MAX_RESULT_BYTES", "10")
	if _, err := Load(nil); err == nil {
		t.Error("MAX_RESULT_BYTES below 1024 must fail")
	}
}
