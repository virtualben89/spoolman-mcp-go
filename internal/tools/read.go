package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/virtualben89/spoolman-mcp-go/internal/spoolman"
)

const filterHelp = " Text filters are partial and case-insensitive; separate multiple terms with commas, wrap a term in double quotes for an exact match, or pass an empty string to match entries where the field is empty."

type emptyInput struct{}

type idInput struct {
	ID int `json:"id" jsonschema:"numeric ID of the object"`
}

type PageInput struct {
	Limit  *int   `json:"limit,omitempty" jsonschema:"maximum number of items to return (server default applies when omitted, 0 = unlimited)"`
	Offset int    `json:"offset,omitempty" jsonschema:"number of items to skip, for pagination"`
	Sort   string `json:"sort,omitempty" q:"sort" jsonschema:"sort order as comma-separated field:direction items, e.g. 'remaining_weight:asc,id:desc'"`
}

type listSpoolsInput struct {
	FilamentName        *string `json:"filament_name,omitempty" q:"filament.name" jsonschema:"filter by filament name"`
	FilamentID          string  `json:"filament_id,omitempty" q:"filament.id" jsonschema:"exact filament ID(s), comma-separated"`
	Material            *string `json:"material,omitempty" q:"filament.material" jsonschema:"filter by filament material, e.g. PLA"`
	VendorName          *string `json:"vendor_name,omitempty" q:"filament.vendor.name" jsonschema:"filter by vendor/manufacturer name"`
	VendorID            string  `json:"vendor_id,omitempty" q:"filament.vendor.id" jsonschema:"exact vendor ID(s), comma-separated; -1 matches spools without vendor"`
	Location            *string `json:"location,omitempty" q:"location" jsonschema:"filter by storage location"`
	LotNr               *string `json:"lot_nr,omitempty" q:"lot_nr" jsonschema:"filter by lot/batch number"`
	Tag                 string  `json:"tag,omitempty" q:"tag" jsonschema:"find the spool linked to this NFC/RFID tag UID"`
	MultiColorDirection *string `json:"multi_color_direction,omitempty" q:"filament.multi_color_direction" jsonschema:"coaxial or longitudinal; empty string for single-color"`
	FirstUsed           string  `json:"first_used,omitempty" q:"first_used" jsonschema:"ISO 8601 timestamp or inclusive range '<start>|<end>' (either side may be empty)"`
	LastUsed            string  `json:"last_used,omitempty" q:"last_used" jsonschema:"ISO 8601 timestamp or inclusive range '<start>|<end>'"`
	Registered          string  `json:"registered,omitempty" q:"registered" jsonschema:"ISO 8601 timestamp or inclusive range '<start>|<end>'"`
	AllowArchived       bool    `json:"allow_archived,omitempty" q:"allow_archived" jsonschema:"include archived spools"`
	PageInput
}

type spoolGroupsInput struct {
	GroupBy       string  `json:"group_by" q:"group_by" jsonschema:"axis to group by: filament, vendor, material, location or extra.<key> for a spool text/choice extra field"`
	FilamentName  *string `json:"filament_name,omitempty" q:"filament.name" jsonschema:"filter by filament name"`
	FilamentID    string  `json:"filament_id,omitempty" q:"filament.id" jsonschema:"exact filament ID(s), comma-separated"`
	Material      *string `json:"material,omitempty" q:"filament.material" jsonschema:"filter by filament material"`
	VendorName    *string `json:"vendor_name,omitempty" q:"filament.vendor.name" jsonschema:"filter by vendor name"`
	VendorID      string  `json:"vendor_id,omitempty" q:"filament.vendor.id" jsonschema:"exact vendor ID(s), comma-separated; -1 = no vendor"`
	Location      *string `json:"location,omitempty" q:"location" jsonschema:"filter by location"`
	LotNr         *string `json:"lot_nr,omitempty" q:"lot_nr" jsonschema:"filter by lot number"`
	AllowArchived bool    `json:"allow_archived,omitempty" q:"allow_archived" jsonschema:"include archived spools in the aggregates"`
	IncludeEmpty  bool    `json:"include_empty,omitempty" q:"include_empty" jsonschema:"also return filaments/vendors without matching spools (group_by filament or vendor)"`
	Preview       bool    `json:"preview,omitempty" q:"preview" jsonschema:"attach a few member spools/filaments to each group"`
	PageInput
}

type listFilamentsInput struct {
	Name                     *string  `json:"name,omitempty" q:"name" jsonschema:"filter by filament name"`
	Material                 *string  `json:"material,omitempty" q:"material" jsonschema:"filter by material, e.g. PETG"`
	VendorName               *string  `json:"vendor_name,omitempty" q:"vendor.name" jsonschema:"filter by vendor name"`
	VendorID                 string   `json:"vendor_id,omitempty" q:"vendor.id" jsonschema:"exact vendor ID(s), comma-separated; -1 = no vendor"`
	ArticleNumber            *string  `json:"article_number,omitempty" q:"article_number" jsonschema:"filter by article number / EAN"`
	ColorHex                 string   `json:"color_hex,omitempty" q:"color_hex" jsonschema:"find filaments with a similar color, hex without # e.g. FF0000"`
	ColorSimilarityThreshold *float64 `json:"color_similarity_threshold,omitempty" q:"color_similarity_threshold" jsonschema:"0-100, 0 = exact color only (default 20)"`
	ExternalID               *string  `json:"external_id,omitempty" q:"external_id" jsonschema:"filter by external database ID"`
	Tag                      string   `json:"tag,omitempty" q:"tag" jsonschema:"find the filament linked to this NFC/RFID tag UID"`
	PageInput
}

type listVendorsInput struct {
	Name       *string `json:"name,omitempty" q:"name" jsonschema:"filter by vendor name"`
	ExternalID *string `json:"external_id,omitempty" q:"external_id" jsonschema:"filter by external database ID"`
	PageInput
}

type searchInput struct {
	Query                    string   `json:"query" q:"q" jsonschema:"search terms; every whitespace-separated term must match a text field. A hex color like FF0000 searches by color"`
	Limit                    *int     `json:"limit,omitempty" q:"limit" jsonschema:"max results per category (default 20)"`
	AllowArchived            bool     `json:"allow_archived,omitempty" q:"allow_archived" jsonschema:"include archived spools"`
	SpoolsPerFilament        *int     `json:"spools_per_filament,omitempty" q:"spools_per_filament" jsonschema:"include up to N spools for each matching filament"`
	ColorSimilarityThreshold *float64 `json:"color_similarity_threshold,omitempty" q:"color_similarity_threshold" jsonschema:"0-100 for color queries (default 20)"`
}

type distinctInput struct {
	Kind string `json:"kind" jsonschema:"which distinct values to list"`
}

type entityTypeInput struct {
	EntityType string `json:"entity_type" jsonschema:"entity the extra fields belong to"`
}

type fieldValuesInput struct {
	EntityType string `json:"entity_type" jsonschema:"entity the extra field belongs to"`
	Key        string `json:"key" jsonschema:"key of the extra field"`
}

type settingsInput struct {
	Key string `json:"key,omitempty" jsonschema:"setting key, e.g. currency; omit to list all settings"`
}

type externalSearchInput struct {
	Query  string `json:"query" q:"query" jsonschema:"search terms, e.g. 'bambu pla basic black'"`
	Limit  *int   `json:"limit,omitempty" q:"limit" jsonschema:"max results (default 20)"`
	Offset int    `json:"offset,omitempty" q:"offset" jsonschema:"pagination offset"`
}

type exportInput struct {
	Entity        string `json:"entity" jsonschema:"what to export"`
	Format        string `json:"format,omitempty" jsonschema:"csv or json (default csv)"`
	AllowArchived bool   `json:"allow_archived,omitempty" jsonschema:"include archived spools (spools only)"`
}

var entityTypes = []any{"spool", "filament", "vendor"}

func (r *Registry) registerRead() {
	c := r.c

	add(r, toolDef{kind: KindRead, name: "get_info", title: "Spoolman info",
		description: "Show Spoolman version, database type, health and available update. Use this to check connectivity."},
		func(ctx context.Context, _ emptyInput) (*mcp.CallToolResult, error) {
			info, err := c.Get(ctx, "/info", nil)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"info": json.RawMessage(info.Body), "healthy": c.Health(ctx) == nil}
			return valueResult(out)
		})

	add(r, toolDef{kind: KindRead, name: "search", title: "Search everything",
		description: "Full-text search across spools, filaments and vendors in one request (e.g. 'bambu petg black'). Best first step to find IDs. A hex color query (e.g. FF0000) finds similar colors."},
		func(ctx context.Context, in searchInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/search", query(in))
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "list_spools", title: "List spools",
		description: "List spools (physical rolls) with their filament, vendor, remaining/used weight and location. Archived spools are excluded unless allow_archived=true. Returns {total, count, offset, has_more, items}." + filterHelp},
		func(ctx context.Context, in listSpoolsInput) (*mcp.CallToolResult, error) {
			pq, l, o := r.paging(in.Limit, in.Offset)
			resp, err := c.Get(ctx, "/spool", merge(pq, query(in)))
			if err != nil {
				return nil, err
			}
			return listResult(resp, l, o)
		})

	add(r, toolDef{kind: KindRead, name: "get_spool", title: "Get spool",
		description: "Get one spool by ID, including its filament and vendor."},
		func(ctx context.Context, in idInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/spool/"+strconv.Itoa(in.ID), nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "list_spool_groups", title: "Spool inventory summary",
		description: "Aggregate spools by filament, vendor, material, location or extra.<key>: spool count, in-use count, total remaining weight and last use per group. Ideal for inventory questions like 'how much PLA do I have left?'." + filterHelp},
		func(ctx context.Context, in spoolGroupsInput) (*mcp.CallToolResult, error) {
			pq, l, o := r.paging(in.Limit, in.Offset)
			resp, err := c.Get(ctx, "/spool/group", merge(pq, query(in)))
			if err != nil {
				return nil, err
			}
			return listResult(resp, l, o)
		})

	add(r, toolDef{kind: KindRead, name: "list_filaments", title: "List filaments",
		description: "List filament types (product definitions: vendor, material, color, density, diameter, weight, temperatures). Returns {total, count, offset, has_more, items}." + filterHelp},
		func(ctx context.Context, in listFilamentsInput) (*mcp.CallToolResult, error) {
			pq, l, o := r.paging(in.Limit, in.Offset)
			resp, err := c.Get(ctx, "/filament", merge(pq, query(in)))
			if err != nil {
				return nil, err
			}
			return listResult(resp, l, o)
		})

	add(r, toolDef{kind: KindRead, name: "get_filament", title: "Get filament",
		description: "Get one filament type by ID, including its vendor."},
		func(ctx context.Context, in idInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/filament/"+strconv.Itoa(in.ID), nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "list_vendors", title: "List vendors",
		description: "List vendors (filament manufacturers). Returns {total, count, offset, has_more, items}." + filterHelp},
		func(ctx context.Context, in listVendorsInput) (*mcp.CallToolResult, error) {
			pq, l, o := r.paging(in.Limit, in.Offset)
			resp, err := c.Get(ctx, "/vendor", merge(pq, query(in)))
			if err != nil {
				return nil, err
			}
			return listResult(resp, l, o)
		})

	add(r, toolDef{kind: KindRead, name: "get_vendor", title: "Get vendor",
		description: "Get one vendor by ID."},
		func(ctx context.Context, in idInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/vendor/"+strconv.Itoa(in.ID), nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	distinctPaths := map[string]string{
		"materials": "/material", "locations": "/location",
		"article_numbers": "/article-number", "lot_numbers": "/lot-number",
	}
	add(r, toolDef{kind: KindRead, name: "list_distinct_values", title: "List distinct values",
		description: "List all distinct values in use for materials, spool locations, article numbers or lot numbers.",
		enums:       map[string][]any{"kind": {"materials", "locations", "article_numbers", "lot_numbers"}}},
		func(ctx context.Context, in distinctInput) (*mcp.CallToolResult, error) {
			p, ok := distinctPaths[in.Kind]
			if !ok {
				return nil, fmt.Errorf("unknown kind %q", in.Kind)
			}
			resp, err := c.Get(ctx, p, nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "list_extra_fields", title: "List extra fields",
		description: "List the custom (extra) field definitions for spools, filaments or vendors: key, name, type, unit, choices, default.",
		enums:       map[string][]any{"entity_type": entityTypes}},
		func(ctx context.Context, in entityTypeInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/field/"+spoolman.PathEscape(in.EntityType), nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "list_extra_field_values", title: "List extra field values",
		description: "List the distinct values used for one custom (extra) field.",
		enums:       map[string][]any{"entity_type": entityTypes}},
		func(ctx context.Context, in fieldValuesInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/field/"+spoolman.PathEscape(in.EntityType)+"/"+spoolman.PathEscape(in.Key)+"/values", nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "get_settings", title: "Get settings",
		description: "Get one Spoolman setting (e.g. currency) or all settings when key is omitted. Values are JSON-encoded strings."},
		func(ctx context.Context, in settingsInput) (*mcp.CallToolResult, error) {
			p := "/setting/"
			if in.Key != "" {
				p += spoolman.PathEscape(in.Key)
			}
			resp, err := c.Get(ctx, p, nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "search_external_filaments", title: "Search external filament database",
		description: "Search the external SpoolmanDB catalogue of commercial filaments (manufacturer, material, color, density, weight, temperatures). Use the returned id with import_external_filament."},
		func(ctx context.Context, in externalSearchInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/external/filament/search", query(in))
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "list_external_materials", title: "List external materials",
		description: "List materials from the external SpoolmanDB with typical density and temperatures."},
		func(ctx context.Context, _ emptyInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/external/material", nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})

	add(r, toolDef{kind: KindRead, name: "export", title: "Export data",
		description: "Export all spools, filaments or vendors as CSV or JSON text. Can be large; prefer list tools for targeted questions.",
		enums:       map[string][]any{"entity": {"spools", "filaments", "vendors"}, "format": {"csv", "json"}}},
		func(ctx context.Context, in exportInput) (*mcp.CallToolResult, error) {
			f := in.Format
			if f == "" {
				f = "csv"
			}
			q := url.Values{"fmt": {f}}
			if in.AllowArchived && in.Entity == "spools" {
				q.Set("allow_archived", "true")
			}
			resp, err := c.Get(ctx, "/export/"+spoolman.PathEscape(in.Entity), q)
			if err != nil {
				return nil, err
			}
			return textResult(string(resp.Body)), nil
		})

	add(r, toolDef{kind: KindRead, name: "list_tag_readers", title: "List NFC tag readers",
		description: "List NFC/RFID tag readers that recently reported scans to Spoolman."},
		func(ctx context.Context, _ emptyInput) (*mcp.CallToolResult, error) {
			resp, err := c.Get(ctx, "/tag/reader", nil)
			if err != nil {
				return nil, err
			}
			return jsonResult(resp.Body), nil
		})
}
