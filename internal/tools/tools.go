// Package tools registers the Spoolman MCP tools.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/virtualben89/spoolman-mcp-go/internal/spoolman"
)

// Options controls which tools are exposed.
type Options struct {
	ReadOnly     bool
	AllowDelete  bool
	Prefix       string
	DefaultLimit int
}

// Kind classifies a tool for gating.
type Kind int

const (
	KindRead Kind = iota
	KindWrite
	KindDelete
)

// Registry adds tools to a server.
type Registry struct {
	srv   *mcp.Server
	c     *spoolman.Client
	opts  Options
	names []string
}

// Register adds all enabled tools and returns their names.
func Register(srv *mcp.Server, c *spoolman.Client, opts Options) []string {
	r := &Registry{srv: srv, c: c, opts: opts}
	r.registerRead()
	r.registerWrite()
	r.registerDelete()
	return r.names
}

func ptr[T any](v T) *T { return &v }

type toolDef struct {
	kind        Kind
	name        string
	title       string
	description string
	idempotent  bool
	enums       map[string][]any // property name -> allowed values
}

// add registers a typed tool after applying the read-only / delete gates.
func add[In any](r *Registry, d toolDef, h func(context.Context, In) (*mcp.CallToolResult, error)) {
	switch d.kind {
	case KindWrite:
		if r.opts.ReadOnly {
			return
		}
	case KindDelete:
		if r.opts.ReadOnly || !r.opts.AllowDelete {
			return
		}
	}
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("schema for %s: %v", d.name, err))
	}
	sanitizeSchema(schema)
	for prop, vals := range d.enums {
		s, ok := schema.Properties[prop]
		if !ok {
			panic(fmt.Sprintf("tool %s: enum for unknown property %s", d.name, prop))
		}
		if s.Type == "array" && s.Items != nil {
			s.Items.Enum = vals
		} else {
			s.Enum = vals
		}
	}

	ann := &mcp.ToolAnnotations{Title: d.title, OpenWorldHint: ptr(false)}
	switch d.kind {
	case KindRead:
		ann.ReadOnlyHint = true
		ann.IdempotentHint = true
	case KindWrite:
		ann.DestructiveHint = ptr(false)
		ann.IdempotentHint = d.idempotent
	case KindDelete:
		ann.DestructiveHint = ptr(true)
		ann.IdempotentHint = true
	}

	name := r.opts.Prefix + d.name
	tool := &mcp.Tool{
		Name:        name,
		Title:       d.title,
		Description: d.description,
		InputSchema: schema,
		Annotations: ann,
	}
	mcp.AddTool(r.srv, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		res, err := h(ctx, in)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return res, nil, nil
	})
	r.names = append(r.names, name)
}

// sanitizeSchema makes generated schemas friendly to a wide range of clients
// (e.g. Gemini / OpenAI function calling reject `"type": ["null","string"]`).
func sanitizeSchema(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if len(s.Types) > 0 {
		var nonNull []string
		for _, t := range s.Types {
			if t != "null" {
				nonNull = append(nonNull, t)
			}
		}
		if len(nonNull) == 1 {
			s.Type = nonNull[0]
			s.Types = nil
		}
	}
	for _, p := range s.Properties {
		sanitizeSchema(p)
	}
	sanitizeSchema(s.Items)
	if s.AdditionalProperties != nil && s.Type != "object" {
		sanitizeSchema(s.AdditionalProperties)
	}
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// jsonResult returns compact JSON text.
func jsonResult(raw []byte) *mcp.CallToolResult {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return textResult(string(raw))
	}
	return textResult(buf.String())
}

func valueResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return textResult(string(b)), nil
}

// listResult wraps a list response with pagination metadata.
func listResult(resp *spoolman.Response, limit, offset int) (*mcp.CallToolResult, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(resp.Body, &items); err != nil {
		return jsonResult(resp.Body), nil
	}
	if items == nil {
		items = []json.RawMessage{}
	}
	total := resp.TotalCount
	if total < 0 {
		total = offset + len(items)
	}
	out := map[string]any{
		"total":    total,
		"offset":   offset,
		"count":    len(items),
		"has_more": offset+len(items) < total,
		"items":    items,
	}
	if limit > 0 {
		out["limit"] = limit
	}
	return valueResult(out)
}

// query builds url.Values from struct fields tagged `q:"name"`.
// Nil pointers, empty strings (for non-pointer strings), false bools and zero ints are skipped.
func query(v any) url.Values {
	q := url.Values{}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	rt := rv.Type()
	for i := range rt.NumField() {
		if rt.Field(i).Anonymous && rv.Field(i).Kind() == reflect.Struct {
			for k, v := range query(rv.Field(i).Interface()) {
				q[k] = v
			}
			continue
		}
		tag := rt.Field(i).Tag.Get("q")
		if tag == "" || tag == "-" {
			continue
		}
		f := rv.Field(i)
		if f.Kind() == reflect.Pointer {
			if f.IsNil() {
				continue
			}
			f = f.Elem()
			q.Set(tag, scalarString(f))
			continue
		}
		if f.IsZero() {
			continue
		}
		if f.Kind() == reflect.Slice {
			parts := make([]string, f.Len())
			for j := range f.Len() {
				parts[j] = scalarString(f.Index(j))
			}
			q.Set(tag, strings.Join(parts, ","))
			continue
		}
		q.Set(tag, scalarString(f))
	}
	return q
}

func scalarString(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int64, reflect.Int32:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Float64, reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	default:
		return fmt.Sprint(v.Interface())
	}
}

// paging applies the default limit.
func (r *Registry) paging(limit *int, offset int) (url.Values, int, int) {
	q := url.Values{}
	l := r.opts.DefaultLimit
	if limit != nil {
		l = *limit
	}
	if l > 0 {
		q.Set("limit", strconv.Itoa(l))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	return q, l, offset
}

func merge(a, b url.Values) url.Values {
	for k, v := range b {
		a[k] = v
	}
	return a
}

// encodeExtra converts native values to Spoolman's JSON-encoded-string format.
func encodeExtra(in map[string]any) (map[string]string, error) {
	if in == nil {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("extra.%s: %w", k, err)
		}
		out[k] = string(b)
	}
	return out, nil
}

// buildBody marshals a parameter struct to a map, encodes `extra` and applies clear_fields.
func buildBody(params any, extra map[string]any, clear []string, clearable []string) (map[string]any, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, err
	}
	delete(body, "extra")
	delete(body, "clear_fields")
	delete(body, "id")
	if extra != nil {
		enc, err := encodeExtra(extra)
		if err != nil {
			return nil, err
		}
		body["extra"] = enc
	}
	for _, f := range clear {
		if !slices.Contains(clearable, f) {
			return nil, fmt.Errorf("field %q cannot be cleared; clearable fields: %s", f, strings.Join(clearable, ", "))
		}
		if _, set := body[f]; set {
			return nil, fmt.Errorf("field %q is both set and listed in clear_fields", f)
		}
		body[f] = nil
	}
	return body, nil
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

var errConfirm = errors.New("refusing to delete: set confirm=true to confirm this irreversible deletion")
