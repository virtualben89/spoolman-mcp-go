//go:build integration

// Integration tests against a real, disposable Spoolman instance.
// Run with: SPOOLMAN_TEST_URL=http://localhost:7912 go test -tags integration ./internal/tools/
// WARNING: creates and deletes data. Never point this at a production instance.
package tools_test

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/virtualben89/spoolman-mcp-go/internal/tools"
)

func mustJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
}

func TestIntegrationFullLifecycle(t *testing.T) {
	base := os.Getenv("SPOOLMAN_TEST_URL")
	if base == "" {
		t.Skip("SPOOLMAN_TEST_URL not set")
	}
	cs := connect(t, base, tools.Options{AllowDelete: true, DefaultLimit: 50})
	ok := func(name string, args map[string]any) string {
		t.Helper()
		out, isErr := call(t, cs, name, args)
		if isErr {
			t.Fatalf("%s(%v) failed: %s", name, args, out)
		}
		return out
	}
	suffix := fmt.Sprint(time.Now().UnixNano())

	// info
	var info struct {
		Info    struct{ Version string } `json:"info"`
		Healthy bool                     `json:"healthy"`
	}
	mustJSON(t, ok("get_info", nil), &info)
	if !info.Healthy || info.Info.Version == "" {
		t.Fatalf("info: %+v", info)
	}
	t.Logf("Spoolman %s", info.Info.Version)

	// settings
	ok("set_setting", map[string]any{"key": "currency", "value": "EUR"})
	if out := ok("get_settings", map[string]any{"key": "currency"}); !strings.Contains(out, "EUR") {
		t.Errorf("currency not set: %s", out)
	}

	// extra field on spools
	ok("set_extra_field", map[string]any{"entity_type": "spool", "key": "it_dryer", "name": "Dryer", "field_type": "text"})
	ok("set_extra_field", map[string]any{"entity_type": "spool", "key": "it_nozzle", "name": "Nozzle", "field_type": "float", "unit": "mm", "default_value": 0.4})
	if out := ok("list_extra_fields", map[string]any{"entity_type": "spool"}); !strings.Contains(out, "it_nozzle") {
		t.Errorf("extra field missing: %s", out)
	}

	// vendor
	var vendor struct{ ID int }
	mustJSON(t, ok("create_vendor", map[string]any{"name": "ITVendor " + suffix, "empty_spool_weight": 200, "comment": "tmp"}), &vendor)
	var v2 struct {
		Name    string
		Comment *string
	}
	mustJSON(t, ok("update_vendor", map[string]any{"id": vendor.ID, "name": "ITVendor2 " + suffix, "clear_fields": []string{"comment"}}), &v2)
	if !strings.HasPrefix(v2.Name, "ITVendor2") || v2.Comment != nil {
		t.Errorf("vendor update: %+v", v2)
	}

	// filament (diameter default 1.75)
	var fil struct {
		ID       int
		Diameter float64
		Vendor   struct{ ID int }
	}
	mustJSON(t, ok("create_filament", map[string]any{
		"name": "IT PLA Red " + suffix, "vendor_id": vendor.ID, "material": "PLA", "density": 1.24,
		"weight": 1000, "spool_weight": 200, "color_hex": "FF0000", "settings_extruder_temp": 210,
	}), &fil)
	if fil.Diameter != 1.75 || fil.Vendor.ID != vendor.ID {
		t.Errorf("filament: %+v", fil)
	}
	ok("update_filament", map[string]any{"id": fil.ID, "price": 19.99, "clear_fields": []string{"settings_extruder_temp"}})
	ok("get_filament", map[string]any{"id": fil.ID})

	// spool
	var sp struct {
		ID              int
		RemainingWeight float64 `json:"remaining_weight"`
		Location        string
		Extra           map[string]string
	}
	mustJSON(t, ok("create_spool", map[string]any{
		"filament_id": fil.ID, "location": "IT-Shelf " + suffix, "lot_nr": "L1",
		"extra": map[string]any{"it_dryer": "Box A", "it_nozzle": 0.6},
	}), &sp)
	if sp.RemainingWeight != 1000 || sp.Extra["it_dryer"] != `"Box A"` {
		t.Errorf("spool create: %+v", sp)
	}
	mustJSON(t, ok("use_spool", map[string]any{"id": sp.ID, "use_weight": 100}), &sp)
	if sp.RemainingWeight != 900 {
		t.Errorf("after use: %v", sp.RemainingWeight)
	}
	// gross 200 tare + 500 filament
	mustJSON(t, ok("measure_spool", map[string]any{"id": sp.ID, "weight": 700}), &sp)
	if sp.RemainingWeight != 500 {
		t.Errorf("after measure: %v", sp.RemainingWeight)
	}
	ok("update_spool", map[string]any{"id": sp.ID, "comment": "hello", "archived": false})

	// reads
	var list struct {
		Total int
		Items []struct{ ID int }
	}
	mustJSON(t, ok("list_spools", map[string]any{"filament_id": fmt.Sprint(fil.ID)}), &list)
	if list.Total != 1 || list.Items[0].ID != sp.ID {
		t.Errorf("list_spools: %+v", list)
	}
	ok("list_spool_groups", map[string]any{"group_by": "material", "preview": true})
	ok("list_filaments", map[string]any{"material": "PLA", "color_hex": "FF0000"})
	ok("list_vendors", map[string]any{"name": "ITVendor2"})
	if out := ok("search", map[string]any{"query": "IT PLA Red " + suffix}); !strings.Contains(out, fmt.Sprintf(`"id":%d`, fil.ID)) {
		t.Errorf("search did not find filament: %s", out)
	}
	if out := ok("list_distinct_values", map[string]any{"kind": "locations"}); !strings.Contains(out, "IT-Shelf "+suffix) {
		t.Errorf("locations: %s", out)
	}
	ok("list_extra_field_values", map[string]any{"entity_type": "spool", "key": "it_dryer"})
	ok("list_tag_readers", nil)
	if out := ok("export", map[string]any{"entity": "spools", "format": "csv"}); !strings.Contains(out, "id") {
		t.Errorf("export: %s", out)
	}

	// renames
	ok("rename_location", map[string]any{"location": "IT-Shelf " + suffix, "new_name": "IT-Shelf2 " + suffix})
	ok("rename_spool_field_value", map[string]any{"field": "location", "value": "IT-Shelf2 " + suffix, "new_value": "IT-Shelf3 " + suffix})
	mustJSON(t, ok("get_spool", map[string]any{"id": sp.ID}), &sp)
	if sp.Location != "IT-Shelf3 "+suffix {
		t.Errorf("location rename: %q", sp.Location)
	}

	// tags
	uid := "04A2B3C4" + suffix[len(suffix)-6:]
	ok("link_tag", map[string]any{"entity": "spool", "id": sp.ID, "uid": uid, "format": "ntag"})
	mustJSON(t, ok("list_spools", map[string]any{"tag": uid}), &list)
	if list.Total != 1 {
		t.Errorf("tag lookup: %+v", list)
	}
	if out := ok("report_tag_scan", map[string]any{"uid": uid, "reader_id": "it-reader"}); !strings.Contains(out, fmt.Sprintf(`"matched_spool_id":%d`, sp.ID)) {
		t.Errorf("tag scan: %s", out)
	}
	ok("unlink_tag", map[string]any{"entity": "spool", "id": sp.ID, "uid": uid, "confirm": true})

	// backup
	if out := ok("create_backup", nil); !strings.Contains(out, "path") {
		t.Errorf("backup: %s", out)
	}

	// external db (network-dependent; tolerate an empty catalogue)
	if out, isErr := call(t, cs, "search_external_filaments", map[string]any{"query": "pla black", "limit": 1}); !isErr {
		var ext []struct{ ID string }
		if json.Unmarshal([]byte(out), &ext) == nil && len(ext) > 0 {
			var imp struct {
				Created  bool
				Filament struct{ ID int }
			}
			mustJSON(t, ok("import_external_filament", map[string]any{"external_id": ext[0].ID}), &imp)
			var again struct{ Created bool }
			mustJSON(t, ok("import_external_filament", map[string]any{"external_id": ext[0].ID}), &again)
			if again.Created {
				t.Error("second import must reuse the filament")
			}
			if imp.Created {
				ok("delete_filament", map[string]any{"id": imp.Filament.ID, "confirm": true})
			}
		} else {
			t.Logf("external catalogue empty/unavailable: %.200s", out)
		}
	} else {
		t.Logf("external search unavailable: %.200s", out)
	}

	// deletes: confirm required
	if _, isErr := call(t, cs, "delete_spool", map[string]any{"id": sp.ID, "confirm": false}); !isErr {
		t.Fatal("delete without confirm succeeded")
	}
	ok("delete_spool", map[string]any{"id": sp.ID, "confirm": true})
	if _, isErr := call(t, cs, "get_spool", map[string]any{"id": sp.ID}); !isErr {
		t.Error("spool still exists")
	}
	ok("delete_filament", map[string]any{"id": fil.ID, "confirm": true})
	ok("delete_vendor", map[string]any{"id": vendor.ID, "confirm": true})
	ok("delete_extra_field", map[string]any{"entity_type": "spool", "key": "it_dryer", "confirm": true})
	ok("delete_extra_field", map[string]any{"entity_type": "spool", "key": "it_nozzle", "confirm": true})
	ok("set_setting", map[string]any{"key": "currency", "confirm": true}) // reset
}
