//go:build integration

package tools_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/virtualben89/spoolman-mcp-go/internal/tools"
)

// Re-run of the audit exploits against a real Spoolman: both must now be blocked.
func TestIntegrationAuditExploitsBlocked(t *testing.T) {
	base := os.Getenv("SPOOLMAN_TEST_URL")
	if base == "" {
		t.Skip("SPOOLMAN_TEST_URL not set")
	}
	cs := connect(t, base, tools.Options{AllowDelete: true})
	ok := func(name string, args map[string]any) string {
		t.Helper()
		out, isErr := call(t, cs, name, args)
		if isErr {
			t.Fatalf("%s: %s", name, out)
		}
		return out
	}
	sfx := fmt.Sprint(time.Now().UnixNano())
	var fil, sp struct{ ID int }
	mustJSON(t, ok("create_filament", map[string]any{"name": "AUDIT " + sfx, "material": "PLA", "density": 1.24, "weight": 1000}), &fil)
	mustJSON(t, ok("create_spool", map[string]any{"filament_id": fil.ID}), &sp)
	ok("link_tag", map[string]any{"entity": "spool", "id": sp.ID, "uid": "AABBCC" + sfx[len(sfx)-4:]})

	// Exploit 1: unlink_tag uid "../" deleted the parent spool in v0.1.1.
	if out, isErr := call(t, cs, "unlink_tag", map[string]any{"entity": "spool", "id": sp.ID, "uid": "../", "confirm": true}); !isErr {
		t.Errorf("unlink_tag ../ was accepted: %s", out)
	}
	if _, isErr := call(t, cs, "get_spool", map[string]any{"id": sp.ID}); isErr {
		t.Fatal("spool was deleted by unlink_tag ../")
	}

	// Exploit 2: set_setting extra_fields_spool wiped extra field definitions in v0.1.1.
	ok("set_extra_field", map[string]any{"entity_type": "spool", "key": "audit_x", "name": "Audit", "field_type": "text"})
	if out, isErr := call(t, cs, "set_setting", map[string]any{"key": "extra_fields_spool"}); !isErr {
		t.Errorf("set_setting extra_fields_spool accepted: %s", out)
	}
	var fields []struct{ Key string }
	if err := json.Unmarshal([]byte(ok("list_extra_fields", map[string]any{"entity_type": "spool"})), &fields); err != nil || len(fields) == 0 {
		t.Errorf("extra fields gone: %v %v", fields, err)
	}

	ok("delete_extra_field", map[string]any{"entity_type": "spool", "key": "audit_x", "confirm": true})
	ok("delete_spool", map[string]any{"id": sp.ID, "confirm": true})
	ok("delete_filament", map[string]any{"id": fil.ID, "confirm": true})
}
