package tools_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/virtualben89/spoolman-mcp-go/internal/tools"
)

// Regression: unlink_tag with dot-segment / slash UIDs must be rejected before any upstream request.
func TestUnlinkTagRejectsPathTricks(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{AllowDelete: true})
	for _, uid := range []string{"../", "..", ".", "../../3/", "a/b", `a\b`} {
		n := f.count()
		out, isErr := call(t, cs, "unlink_tag", map[string]any{"entity": "spool", "id": 1, "uid": uid, "confirm": true})
		if !isErr || f.count() != n {
			t.Errorf("uid %q: expected local rejection, got isErr=%v upstream=%d out=%s", uid, isErr, f.count()-n, out)
		}
	}
	out, isErr := call(t, cs, "unlink_tag", map[string]any{"entity": "spool", "id": 1, "uid": "04:A2:B3:C4", "confirm": true})
	if isErr || f.last().Path != "/api/v1/spool/1/tag/04:A2:B3:C4" || f.last().Method != http.MethodDelete {
		t.Errorf("valid uid: %s %+v", out, f.last())
	}
	call(t, cs, "link_tag", map[string]any{"entity": "filament", "id": 3, "uid": "abc"})
	if f.last().Path != "/api/v1/filament/3/tag" {
		t.Errorf("link_tag path: %+v", f.last())
	}
}

func TestPathSegmentToolsReject(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{AllowDelete: true})
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"get_settings", map[string]any{"key": ".."}},
		{"set_setting", map[string]any{"key": "..", "value": 1}},
		{"rename_location", map[string]any{"location": "../x", "new_name": "y"}},
		{"rename_spool_field_value", map[string]any{"field": "..", "value": "a", "new_value": "b"}},
		{"set_extra_field", map[string]any{"entity_type": "spool", "key": ".", "name": "n", "field_type": "text"}},
		{"delete_extra_field", map[string]any{"entity_type": "spool", "key": "..", "confirm": true}},
		{"list_extra_field_values", map[string]any{"entity_type": "spool", "key": "a/b"}},
	}
	for _, c := range cases {
		n := f.count()
		if out, isErr := call(t, cs, c.tool, c.args); !isErr || f.count() != n {
			t.Errorf("%s %v: expected local rejection (isErr=%v, upstream=%d): %s", c.tool, c.args, isErr, f.count()-n, out)
		}
	}
}

// Regression: set_setting must not touch extra field definitions and needs confirm for resets.
func TestSetSettingGuards(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{AllowDelete: false})
	for _, args := range []map[string]any{
		{"key": "extra_fields_spool"},
		{"key": "extra_fields_filament", "value": []any{}},
		{"key": " Extra_Fields_vendor", "confirm": true},
	} {
		n := f.count()
		if _, isErr := call(t, cs, "set_setting", args); !isErr || f.count() != n {
			t.Errorf("%v must be rejected locally", args)
		}
	}
	n := f.count()
	if out, isErr := call(t, cs, "set_setting", map[string]any{"key": "currency"}); !isErr || f.count() != n || !strings.Contains(out, "confirm") {
		t.Errorf("reset without confirm must be refused: %s", out)
	}
	if out, isErr := call(t, cs, "set_setting", map[string]any{"key": "currency", "confirm": true}); isErr || f.last().Body != `"null"` {
		t.Errorf("reset with confirm: %s %q", out, f.last().Body)
	}
}

// Regression: non-additive write tools must be annotated destructive.
func TestDestructiveAnnotations(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{AllowDelete: true})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"rename_location": true, "rename_spool_field_value": true, "update_spool": true, "update_filament": true,
		"update_vendor": true, "set_setting": true, "set_extra_field": true, "use_spool": true, "measure_spool": true,
		"delete_spool": true, "unlink_tag": true,
		"create_spool": false, "create_vendor": false, "create_filament": false, "link_tag": false,
	}
	seen := 0
	for _, tl := range res.Tools {
		w, ok := want[tl.Name]
		if !ok {
			continue
		}
		seen++
		if tl.Annotations == nil || tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint != w {
			t.Errorf("%s: destructiveHint=%v, want %v", tl.Name, tl.Annotations.DestructiveHint, w)
		}
		if tl.Name == "rename_spool_field_value" && tl.Annotations.IdempotentHint {
			t.Errorf("%s must not be idempotent", tl.Name)
		}
	}
	if seen != len(want) {
		t.Errorf("checked %d of %d tools", seen, len(want))
	}
}

func upstream(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestResultCapAndNoHTMLEscaping(t *testing.T) {
	big := `[{"comment":"` + strings.Repeat("x", 10000) + `"}]`
	cs := connect(t, upstream(t, big), tools.Options{MaxResultBytes: 2048})
	out, isErr := call(t, cs, "list_spools", map[string]any{})
	if isErr || len(out) > 2048+200 || !strings.Contains(out, "[truncated") {
		t.Errorf("result not capped: isErr=%v len=%d", isErr, len(out))
	}

	cs = connect(t, upstream(t, `[{"comment":"<b>&</b>"}]`), tools.Options{})
	out, _ = call(t, cs, "list_spools", map[string]any{})
	if !strings.Contains(out, `<b>&</b>`) {
		t.Errorf("HTML escaped output: %s", out)
	}
	_ = mcp.TextContent{}
}
