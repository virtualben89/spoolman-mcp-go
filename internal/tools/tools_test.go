package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/virtualben89/spoolman-mcp-go/internal/spoolman"
	"github.com/virtualben89/spoolman-mcp-go/internal/tools"
)

type recorded struct {
	Method, Path, Query, Body string
}

type fakeSpoolman struct {
	mu   sync.Mutex
	reqs []recorded
	srv  *httptest.Server
}

func newFake(t *testing.T) *fakeSpoolman {
	f := &fakeSpoolman{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, recorded{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/spool" && r.Method == http.MethodGet:
			w.Header().Set("X-Total-Count", "3")
			_, _ = io.WriteString(w, `[{"id":1},{"id":2}]`)
		case r.URL.Path == "/api/v1/spool/404":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"No spool with ID 404 found."}`)
		case r.Method == http.MethodDelete:
			_, _ = io.WriteString(w, `{"message":"Success!"}`)
		default:
			_, _ = io.WriteString(w, `{"id":7}`)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSpoolman) last() recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) == 0 {
		return recorded{}
	}
	return f.reqs[len(f.reqs)-1]
}

func (f *fakeSpoolman) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func connect(t *testing.T, baseURL string, opts tools.Options) *mcp.ClientSession {
	t.Helper()
	c, err := spoolman.New(spoolman.Options{BaseURL: baseURL})
	if err != nil {
		t.Fatal(err)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	tools.Register(srv, c, opts)
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	return names
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestGating(t *testing.T) {
	f := newFake(t)
	all := toolNames(t, connect(t, f.srv.URL, tools.Options{AllowDelete: true}))
	ro := toolNames(t, connect(t, f.srv.URL, tools.Options{ReadOnly: true, AllowDelete: true}))
	noDel := toolNames(t, connect(t, f.srv.URL, tools.Options{AllowDelete: false}))
	pref := toolNames(t, connect(t, f.srv.URL, tools.Options{AllowDelete: true, Prefix: "spoolman_"}))

	if !slices.Contains(all, "delete_spool") || !slices.Contains(all, "create_spool") {
		t.Fatalf("full set incomplete: %v", all)
	}
	for _, n := range ro {
		if strings.HasPrefix(n, "create_") || strings.HasPrefix(n, "update_") || strings.HasPrefix(n, "delete_") {
			t.Errorf("read-only exposes %s", n)
		}
	}
	if !slices.Contains(ro, "list_spools") {
		t.Error("read-only misses list_spools")
	}
	if slices.Contains(noDel, "delete_spool") || !slices.Contains(noDel, "update_spool") {
		t.Errorf("allow_delete=false wrong: %v", noDel)
	}
	if !slices.Contains(pref, "spoolman_list_spools") {
		t.Errorf("prefix missing: %v", pref)
	}
	t.Logf("tools: all=%d read-only=%d no-delete=%d", len(all), len(ro), len(noDel))
}

// Schemas must stay simple so that all clients (OpenAI, Gemini, Claude, ...) accept them.
func TestSchemasPortable(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{AllowDelete: true})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range res.Tools {
		b, _ := json.Marshal(tl.InputSchema)
		s := string(b)
		if strings.Contains(s, `"type":[`) {
			t.Errorf("%s: type array in schema: %s", tl.Name, s)
		}
		if strings.Contains(s, `"$ref"`) || strings.Contains(s, `"$defs"`) {
			t.Errorf("%s: $ref in schema", tl.Name)
		}
		if len(tl.Name) > 48 {
			t.Errorf("%s: name too long", tl.Name)
		}
		if tl.Description == "" || tl.Annotations == nil {
			t.Errorf("%s: missing description/annotations", tl.Name)
		}
	}
	// Spot-check enum injection and flattening of embedded structs.
	for _, tl := range res.Tools {
		if tl.Name == "update_filament" {
			s, _ := json.Marshal(tl.InputSchema)
			for _, want := range []string{`"coaxial"`, `"clear_fields"`, `"settings_bed_temp"`} {
				if !strings.Contains(string(s), want) {
					t.Errorf("update_filament schema lacks %s: %s", want, s)
				}
			}
		}
	}
}

func TestListSpoolsQueryAndPaging(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{DefaultLimit: 50, AllowDelete: true})
	out, isErr := call(t, cs, "list_spools", map[string]any{"material": "PLA", "location": "", "sort": "id:asc", "allow_archived": true})
	if isErr {
		t.Fatal(out)
	}
	q := f.last().Query
	for _, want := range []string{"filament.material=PLA", "location=", "sort=id%3Aasc", "limit=50", "allow_archived=true"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q lacks %q", q, want)
		}
	}
	var res struct {
		Total   int  `json:"total"`
		Count   int  `json:"count"`
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 || res.Count != 2 || !res.HasMore {
		t.Errorf("paging wrong: %+v", res)
	}
}

func TestUpdateBody(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{AllowDelete: true})
	out, isErr := call(t, cs, "update_spool", map[string]any{
		"id": 5, "location": "Drybox", "clear_fields": []string{"comment"},
		"extra": map[string]any{"nozzle": 0.4, "note": "hi"},
	})
	if isErr {
		t.Fatal(out)
	}
	r := f.last()
	if r.Method != http.MethodPatch || r.Path != "/api/v1/spool/5" {
		t.Fatalf("unexpected request %+v", r)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(r.Body), &body)
	if body["location"] != "Drybox" || body["comment"] != nil || len(body) != 3 {
		t.Errorf("body wrong: %s", r.Body)
	}
	if _, ok := body["comment"]; !ok {
		t.Errorf("comment should be explicitly null: %s", r.Body)
	}
	extra := body["extra"].(map[string]any)
	if extra["nozzle"] != "0.4" || extra["note"] != `"hi"` {
		t.Errorf("extra encoding wrong: %v", extra)
	}

	// clearing a non-clearable field is rejected before hitting Spoolman
	n := f.count()
	_, isErr = call(t, cs, "update_spool", map[string]any{"id": 5, "clear_fields": []string{"used_weight"}})
	if !isErr || f.count() != n {
		t.Error("clearing used_weight should fail locally")
	}
}

func TestDeleteRequiresConfirm(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{AllowDelete: true})
	n := f.count()
	out, isErr := call(t, cs, "delete_spool", map[string]any{"id": 1, "confirm": false})
	if !isErr || !strings.Contains(out, "confirm=true") || f.count() != n {
		t.Fatalf("delete without confirm must be refused: %s", out)
	}
	out, isErr = call(t, cs, "delete_spool", map[string]any{"id": 1, "confirm": true})
	if isErr || f.last().Method != http.MethodDelete || f.last().Path != "/api/v1/spool/1" {
		t.Fatalf("delete failed: %s %+v", out, f.last())
	}
}

func TestSetSettingEncoding(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{})
	if out, isErr := call(t, cs, "set_setting", map[string]any{"key": "currency", "value": "EUR"}); isErr {
		t.Fatal(out)
	}
	if got := f.last().Body; got != `"\"EUR\""` {
		t.Errorf("body = %s", got)
	}
}

func TestUseSpoolValidation(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{})
	if _, isErr := call(t, cs, "use_spool", map[string]any{"id": 1}); !isErr {
		t.Error("use_spool without amount must fail")
	}
	if _, isErr := call(t, cs, "use_spool", map[string]any{"id": 1, "use_weight": 1, "use_length": 2}); !isErr {
		t.Error("use_spool with both must fail")
	}
	if out, isErr := call(t, cs, "use_spool", map[string]any{"id": 1, "use_weight": 12.5}); isErr || f.last().Body != `{"use_weight":12.5}` {
		t.Errorf("use_spool body wrong: %s %s", out, f.last().Body)
	}
}

func TestAPIErrorSurfaced(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{})
	out, isErr := call(t, cs, "get_spool", map[string]any{"id": 404})
	if !isErr || !strings.Contains(out, "404") || !strings.Contains(out, "No spool with ID 404") {
		t.Errorf("error not surfaced: %s", out)
	}
}

func TestInvalidArgumentsRejected(t *testing.T) {
	f := newFake(t)
	cs := connect(t, f.srv.URL, tools.Options{})
	if _, isErr := call(t, cs, "list_distinct_values", map[string]any{"kind": "nope"}); !isErr {
		t.Error("enum violation must be rejected")
	}
	if _, isErr := call(t, cs, "get_spool", map[string]any{}); !isErr {
		t.Error("missing required id must be rejected")
	}
}
