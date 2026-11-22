package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const extraHelp = "custom extra field values keyed by field key, given as native JSON values (string, number, bool, array); they are encoded for Spoolman automatically. See list_extra_fields."

// ---- vendor ----

type vendorFields struct {
	Comment          *string  `json:"comment,omitempty" jsonschema:"free text comment"`
	EmptySpoolWeight *float64 `json:"empty_spool_weight,omitempty" jsonschema:"weight of an empty spool of this vendor in grams"`
	ExternalID       *string  `json:"external_id,omitempty" jsonschema:"ID in an external database"`
}

type createVendorInput struct {
	Name string `json:"name" jsonschema:"vendor / manufacturer name"`
	vendorFields
	Extra map[string]any `json:"extra,omitempty" jsonschema:"extra fields: map of field key to value"`
}

type updateVendorInput struct {
	ID   int     `json:"id" jsonschema:"vendor ID"`
	Name *string `json:"name,omitempty" jsonschema:"new vendor name"`
	vendorFields
	Extra       map[string]any `json:"extra,omitempty" jsonschema:"extra fields to set: map of field key to value"`
	ClearFields []string       `json:"clear_fields,omitempty" jsonschema:"fields to reset to empty/null"`
}

var vendorClearable = []string{"comment", "empty_spool_weight", "external_id"}

// ---- filament ----

type filamentFields struct {
	Name                 *string  `json:"name,omitempty" jsonschema:"filament name, should contain the color, e.g. 'PLA Basic Black'"`
	VendorID             *int     `json:"vendor_id,omitempty" jsonschema:"vendor ID"`
	Material             *string  `json:"material,omitempty" jsonschema:"material, e.g. PLA, PETG, ABS, TPU"`
	Price                *float64 `json:"price,omitempty" jsonschema:"price of a full spool in the configured currency"`
	Weight               *float64 `json:"weight,omitempty" jsonschema:"net filament weight of a full spool in grams, e.g. 1000"`
	SpoolWeight          *float64 `json:"spool_weight,omitempty" jsonschema:"empty spool (tare) weight in grams"`
	ArticleNumber        *string  `json:"article_number,omitempty" jsonschema:"vendor article number / EAN"`
	Comment              *string  `json:"comment,omitempty" jsonschema:"free text comment"`
	SettingsExtruderTemp *int     `json:"settings_extruder_temp,omitempty" jsonschema:"extruder temperature in °C"`
	SettingsBedTemp      *int     `json:"settings_bed_temp,omitempty" jsonschema:"bed temperature in °C"`
	ColorHex             *string  `json:"color_hex,omitempty" jsonschema:"color as hex without #, e.g. FF0000 (optional alpha suffix)"`
	MultiColorHexes      *string  `json:"multi_color_hexes,omitempty" jsonschema:"comma-separated hex colors for multi-color filaments; set multi_color_direction too"`
	MultiColorDirection  *string  `json:"multi_color_direction,omitempty" jsonschema:"coaxial or longitudinal (multi-color only)"`
	ExternalID           *string  `json:"external_id,omitempty" jsonschema:"ID in the external filament database"`
}

type createFilamentInput struct {
	Density  float64  `json:"density" jsonschema:"density in g/cm³, e.g. PLA 1.24, PETG 1.27, ABS 1.04"`
	Diameter *float64 `json:"diameter,omitempty" jsonschema:"diameter in mm (default 1.75)"`
	filamentFields
	Extra map[string]any `json:"extra,omitempty" jsonschema:"extra fields: map of field key to value"`
}

type updateFilamentInput struct {
	ID       int      `json:"id" jsonschema:"filament ID"`
	Density  *float64 `json:"density,omitempty" jsonschema:"density in g/cm³"`
	Diameter *float64 `json:"diameter,omitempty" jsonschema:"diameter in mm"`
	filamentFields
	Extra       map[string]any `json:"extra,omitempty" jsonschema:"extra fields to set: map of field key to value"`
	ClearFields []string       `json:"clear_fields,omitempty" jsonschema:"fields to reset to empty/null"`
}

var filamentClearable = []string{"name", "vendor_id", "material", "price", "weight", "spool_weight", "article_number",
	"comment", "settings_extruder_temp", "settings_bed_temp", "color_hex", "multi_color_hexes", "multi_color_direction", "external_id"}

// ---- spool ----

type spoolFields struct {
	FirstUsed       *string  `json:"first_used,omitempty" jsonschema:"ISO 8601 timestamp of first use"`
	LastUsed        *string  `json:"last_used,omitempty" jsonschema:"ISO 8601 timestamp of last use"`
	Price           *float64 `json:"price,omitempty" jsonschema:"price paid for this spool"`
	InitialWeight   *float64 `json:"initial_weight,omitempty" jsonschema:"initial net filament weight in grams (defaults to the filament's weight)"`
	SpoolWeight     *float64 `json:"spool_weight,omitempty" jsonschema:"empty spool (tare) weight in grams"`
	RemainingWeight *float64 `json:"remaining_weight,omitempty" jsonschema:"remaining net filament in grams (alternative to used_weight)"`
	UsedWeight      *float64 `json:"used_weight,omitempty" jsonschema:"used filament in grams (alternative to remaining_weight)"`
	Location        *string  `json:"location,omitempty" jsonschema:"storage location, e.g. 'Drybox 1' or 'AMS slot 2'"`
	LotNr           *string  `json:"lot_nr,omitempty" jsonschema:"vendor lot/batch number"`
	Comment         *string  `json:"comment,omitempty" jsonschema:"free text comment"`
	Archived        *bool    `json:"archived,omitempty" jsonschema:"archive (true) or unarchive (false) the spool"`
}

type createSpoolInput struct {
	FilamentID int `json:"filament_id" jsonschema:"ID of the filament type of this spool"`
	spoolFields
	Extra map[string]any `json:"extra,omitempty" jsonschema:"extra fields: map of field key to value"`
}

type updateSpoolInput struct {
	ID         int  `json:"id" jsonschema:"spool ID"`
	FilamentID *int `json:"filament_id,omitempty" jsonschema:"change the filament type"`
	spoolFields
	Extra       map[string]any `json:"extra,omitempty" jsonschema:"extra fields to set: map of field key to value"`
	ClearFields []string       `json:"clear_fields,omitempty" jsonschema:"fields to reset to empty/null"`
}

var spoolClearable = []string{"first_used", "last_used", "price", "initial_weight", "spool_weight", "location", "lot_nr", "comment"}

type useSpoolInput struct {
	ID        int      `json:"id" jsonschema:"spool ID"`
	UseWeight *float64 `json:"use_weight,omitempty" jsonschema:"grams of filament consumed (give either use_weight or use_length)"`
	UseLength *float64 `json:"use_length,omitempty" jsonschema:"millimetres of filament consumed (give either use_weight or use_length)"`
}

type measureSpoolInput struct {
	ID     int     `json:"id" jsonschema:"spool ID"`
	Weight float64 `json:"weight" jsonschema:"current GROSS weight of the spool on a scale (filament + empty spool), in grams"`
}

type renameLocationInput struct {
	Location string `json:"location" jsonschema:"current location name"`
	NewName  string `json:"new_name" jsonschema:"new location name; all spools in the location are moved"`
}

type renameFieldValueInput struct {
	Field    string `json:"field" jsonschema:"spool field: location or extra.<key> for a text/single-choice extra field"`
	Value    string `json:"value" jsonschema:"current value"`
	NewValue string `json:"new_value" jsonschema:"replacement value (merges if it already exists)"`
}

type setExtraFieldInput struct {
	EntityType   string   `json:"entity_type" jsonschema:"entity the field belongs to"`
	Key          string   `json:"key" jsonschema:"stable field key, e.g. nozzle_size (lowercase, no spaces)"`
	Name         string   `json:"name" jsonschema:"display name"`
	FieldType    string   `json:"field_type" jsonschema:"value type of the field"`
	Unit         *string  `json:"unit,omitempty" jsonschema:"unit shown next to the value, e.g. mm"`
	DefaultValue any      `json:"default_value,omitempty" jsonschema:"default value as native JSON value"`
	Choices      []string `json:"choices,omitempty" jsonschema:"allowed values for field_type choice"`
	MultiChoice  *bool    `json:"multi_choice,omitempty" jsonschema:"allow multiple choices (field_type choice)"`
	Order        *int     `json:"order,omitempty" jsonschema:"display order"`
}

type setSettingInput struct {
	Key     string `json:"key" jsonschema:"setting key, e.g. currency (extra field definitions are managed with set_extra_field/delete_extra_field)"`
	Value   any    `json:"value,omitempty" jsonschema:"new value as native JSON value matching the setting type (e.g. \"EUR\", 5, true); omit or null to reset to default"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"must be true to reset a setting to its default (value omitted or null)"`
}

// tagEntityPaths maps the link_tag/unlink_tag entity enum to constant path prefixes.
var tagEntityPaths = map[string]string{"spool": "/spool/", "filament": "/filament/"}

type tagLinkInput struct {
	Entity string  `json:"entity" jsonschema:"what to link the tag to"`
	ID     int     `json:"id" jsonschema:"spool or filament ID"`
	UID    string  `json:"uid" jsonschema:"NFC/RFID tag UID; separators and case are ignored"`
	Format *string `json:"format,omitempty" jsonschema:"tag format, e.g. ntag, openprinttag, bambu"`
}

type tagScanInput struct {
	UID        string  `json:"uid" jsonschema:"scanned tag UID"`
	ReaderID   *string `json:"reader_id,omitempty" jsonschema:"stable reader ID, e.g. device hostname"`
	Name       *string `json:"name,omitempty" jsonschema:"human readable reader name"`
	Format     *string `json:"format,omitempty" jsonschema:"tag format, e.g. openprinttag, bambu, ntag"`
	PayloadB64 *string `json:"payload_b64,omitempty" jsonschema:"raw tag contents, base64"`
	Create     bool    `json:"create,omitempty" jsonschema:"create a spool from the decoded tag contents if the tag is unknown"`
}

type importExternalInput struct {
	ExternalID string `json:"external_id" jsonschema:"ID from search_external_filaments"`
	VendorID   *int   `json:"vendor_id,omitempty" jsonschema:"use this existing vendor instead of matching/creating one by manufacturer name"`
}

// ---- delete ----

type deleteInput struct {
	ID      int  `json:"id" jsonschema:"ID of the object to delete"`
	Confirm bool `json:"confirm" jsonschema:"must be true to confirm the irreversible deletion"`
}

type deleteFieldInput struct {
	EntityType string `json:"entity_type" jsonschema:"entity the field belongs to"`
	Key        string `json:"key" jsonschema:"extra field key"`
	Confirm    bool   `json:"confirm" jsonschema:"must be true; deletes the field and its values from all objects"`
}

type unlinkTagInput struct {
	Entity  string `json:"entity" jsonschema:"what the tag is linked to"`
	ID      int    `json:"id" jsonschema:"spool or filament ID"`
	UID     string `json:"uid" jsonschema:"tag UID"`
	Confirm bool   `json:"confirm" jsonschema:"must be true to confirm"`
}

func (r *Registry) send(ctx context.Context, method, path string, body any) (*mcp.CallToolResult, error) {
	resp, err := r.c.Do(ctx, method, path, nil, body)
	if err != nil {
		return nil, err
	}
	if len(resp.Body) == 0 || string(resp.Body) == "null" {
		return valueResult(map[string]any{"ok": true})
	}
	return jsonResult(resp.Body), nil
}

func (r *Registry) registerWrite() {
	// vendor
	add(r, toolDef{kind: KindWrite, name: "create_vendor", title: "Create vendor",
		description: "Create a vendor (filament manufacturer). Check list_vendors first to avoid duplicates."},
		func(ctx context.Context, in createVendorInput) (*mcp.CallToolResult, error) {
			body, err := buildBody(in, in.Extra, nil, nil)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPost, "/vendor", body)
		})
	add(r, toolDef{kind: KindWrite, name: "update_vendor", title: "Update vendor", destructive: true, idempotent: true,
		description: "Update selected fields of a vendor. Only given fields change; list fields in clear_fields to empty them. Extra: " + extraHelp,
		enums:       map[string][]any{"clear_fields": toAny(vendorClearable)}},
		func(ctx context.Context, in updateVendorInput) (*mcp.CallToolResult, error) {
			body, err := buildBody(in, in.Extra, in.ClearFields, vendorClearable)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPatch, "/vendor/"+strconv.Itoa(in.ID), body)
		})

	// filament
	add(r, toolDef{kind: KindWrite, name: "create_filament", title: "Create filament",
		description: "Create a filament type (product definition). Spools are created separately with create_spool. Tip: import_external_filament fills all data from the SpoolmanDB catalogue. Extra: " + extraHelp,
		enums:       map[string][]any{"multi_color_direction": {"coaxial", "longitudinal"}}},
		func(ctx context.Context, in createFilamentInput) (*mcp.CallToolResult, error) {
			if in.Diameter == nil {
				in.Diameter = ptr(1.75)
			}
			body, err := buildBody(in, in.Extra, nil, nil)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPost, "/filament", body)
		})
	add(r, toolDef{kind: KindWrite, name: "update_filament", title: "Update filament", destructive: true, idempotent: true,
		description: "Update selected fields of a filament type. Only given fields change; list fields in clear_fields to empty them. Extra: " + extraHelp,
		enums: map[string][]any{"multi_color_direction": {"coaxial", "longitudinal"},
			"clear_fields": toAny(filamentClearable)}},
		func(ctx context.Context, in updateFilamentInput) (*mcp.CallToolResult, error) {
			body, err := buildBody(in, in.Extra, in.ClearFields, filamentClearable)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPatch, "/filament/"+strconv.Itoa(in.ID), body)
		})

	// spool
	add(r, toolDef{kind: KindWrite, name: "create_spool", title: "Create spool",
		description: "Register a new physical spool of an existing filament type. Give either remaining_weight or used_weight, not both. Extra: " + extraHelp},
		func(ctx context.Context, in createSpoolInput) (*mcp.CallToolResult, error) {
			if in.RemainingWeight != nil && in.UsedWeight != nil {
				return nil, errors.New("give either remaining_weight or used_weight, not both")
			}
			body, err := buildBody(in, in.Extra, nil, nil)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPost, "/spool", body)
		})
	add(r, toolDef{kind: KindWrite, name: "update_spool", title: "Update spool", destructive: true, idempotent: true,
		description: "Update selected fields of a spool, e.g. location, comment, archived, remaining_weight. Only given fields change; list fields in clear_fields to empty them. For consumption prefer use_spool or measure_spool. Extra: " + extraHelp,
		enums:       map[string][]any{"clear_fields": toAny(spoolClearable)}},
		func(ctx context.Context, in updateSpoolInput) (*mcp.CallToolResult, error) {
			if in.RemainingWeight != nil && in.UsedWeight != nil {
				return nil, errors.New("give either remaining_weight or used_weight, not both")
			}
			body, err := buildBody(in, in.Extra, in.ClearFields, spoolClearable)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPatch, "/spool/"+strconv.Itoa(in.ID), body)
		})
	add(r, toolDef{kind: KindWrite, name: "use_spool", title: "Consume filament", destructive: true,
		description: "Record filament consumption on a spool, by weight (g) or length (mm). Updates used/remaining weight and first/last used timestamps."},
		func(ctx context.Context, in useSpoolInput) (*mcp.CallToolResult, error) {
			if (in.UseWeight == nil) == (in.UseLength == nil) {
				return nil, errors.New("give exactly one of use_weight or use_length")
			}
			body := map[string]any{}
			if in.UseWeight != nil {
				body["use_weight"] = *in.UseWeight
			} else {
				body["use_length"] = *in.UseLength
			}
			return r.send(ctx, http.MethodPut, "/spool/"+strconv.Itoa(in.ID)+"/use", body)
		})
	add(r, toolDef{kind: KindWrite, name: "measure_spool", title: "Update from scale", destructive: true,
		description: "Set a spool's remaining filament from its current gross weight on a scale (filament + empty spool). Spoolman subtracts the spool weight."},
		func(ctx context.Context, in measureSpoolInput) (*mcp.CallToolResult, error) {
			return r.send(ctx, http.MethodPut, "/spool/"+strconv.Itoa(in.ID)+"/measure", map[string]any{"weight": in.Weight})
		})
	add(r, toolDef{kind: KindWrite, name: "rename_location", title: "Rename location", destructive: true,
		description: "Rename a storage location; all spools in it are moved to the new name."},
		func(ctx context.Context, in renameLocationInput) (*mcp.CallToolResult, error) {
			loc, err := seg(in.Location)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPatch, "/location/"+loc, map[string]any{"name": in.NewName})
		})
	add(r, toolDef{kind: KindWrite, name: "rename_spool_field_value", title: "Rename spool field value", destructive: true,
		description: "Replace one value of a spool field (location or extra.<key>) on all spools at once, including archived ones."},
		func(ctx context.Context, in renameFieldValueInput) (*mcp.CallToolResult, error) {
			field, err := seg(in.Field)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPatch, "/spool/field/"+field,
				map[string]any{"value": in.Value, "new_value": in.NewValue})
		})

	// fields & settings
	add(r, toolDef{kind: KindWrite, name: "set_extra_field", title: "Create/update extra field", destructive: true, idempotent: true,
		description: "Create or update a custom (extra) field definition for spools, filaments or vendors.",
		enums: map[string][]any{"entity_type": entityTypes,
			"field_type": {"text", "integer", "integer_range", "float", "float_range", "datetime", "boolean", "choice"}}},
		func(ctx context.Context, in setExtraFieldInput) (*mcp.CallToolResult, error) {
			body := map[string]any{"name": in.Name, "field_type": in.FieldType}
			if in.Unit != nil {
				body["unit"] = *in.Unit
			}
			if in.DefaultValue != nil {
				b, err := json.Marshal(in.DefaultValue)
				if err != nil {
					return nil, err
				}
				body["default_value"] = string(b)
			}
			if in.Choices != nil {
				body["choices"] = in.Choices
			}
			if in.MultiChoice != nil {
				body["multi_choice"] = *in.MultiChoice
			}
			if in.Order != nil {
				body["order"] = *in.Order
			}
			et, err := seg(in.EntityType)
			if err != nil {
				return nil, err
			}
			key, err := seg(in.Key)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodPost, "/field/"+et+"/"+key, body)
		})
	add(r, toolDef{kind: KindWrite, name: "set_setting", title: "Change setting", idempotent: true, destructive: true,
		description: "Change a Spoolman preference setting (e.g. currency). Omit value and set confirm=true to reset it to the default. Extra field definitions (extra_fields_*) cannot be changed here; use set_extra_field / delete_extra_field."},
		func(ctx context.Context, in setSettingInput) (*mcp.CallToolResult, error) {
			key, err := seg(in.Key)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(in.Key)), "extra_fields") {
				return nil, errors.New("extra field definitions are managed with set_extra_field / delete_extra_field, not set_setting")
			}
			if in.Value == nil && !in.Confirm {
				return nil, errors.New("refusing to reset the setting to its default: set confirm=true")
			}
			// Spoolman expects the body to be a JSON string that itself contains the JSON value.
			inner := []byte("null")
			if in.Value != nil {
				if inner, err = json.Marshal(in.Value); err != nil {
					return nil, err
				}
			}
			outer, _ := json.Marshal(string(inner))
			return r.send(ctx, http.MethodPost, "/setting/"+key, outer)
		})
	add(r, toolDef{kind: KindWrite, name: "create_backup", title: "Create backup",
		description: "Trigger a database backup on the Spoolman server and return its path."},
		func(ctx context.Context, _ emptyInput) (*mcp.CallToolResult, error) {
			return r.send(ctx, http.MethodPost, "/backup", nil)
		})

	// tags
	add(r, toolDef{kind: KindWrite, name: "link_tag", title: "Link NFC tag", idempotent: true,
		description: "Link an NFC/RFID tag UID to a spool or filament.",
		enums:       map[string][]any{"entity": {"spool", "filament"}}},
		func(ctx context.Context, in tagLinkInput) (*mcp.CallToolResult, error) {
			body := map[string]any{"uid": in.UID}
			if in.Format != nil {
				body["format"] = *in.Format
			}
			prefix, ok := tagEntityPaths[in.Entity]
			if !ok {
				return nil, fmt.Errorf("invalid entity %q", in.Entity)
			}
			return r.send(ctx, http.MethodPost, prefix+strconv.Itoa(in.ID)+"/tag", body)
		})
	add(r, toolDef{kind: KindWrite, name: "report_tag_scan", title: "Report tag scan",
		description: "Report an NFC tag scan to Spoolman and get the matching spool/filament. With create=true an unknown but decodable tag creates a new spool."},
		func(ctx context.Context, in tagScanInput) (*mcp.CallToolResult, error) {
			return r.send(ctx, http.MethodPost, "/tag/scan", in)
		})

	add(r, toolDef{kind: KindWrite, name: "import_external_filament", title: "Import filament from SpoolmanDB",
		description: "Create a filament type from the external SpoolmanDB catalogue (find the id with search_external_filaments). The vendor is matched by name or created. If the filament was already imported, the existing one is returned."},
		r.importExternal)
}

func (r *Registry) importExternal(ctx context.Context, in importExternalInput) (*mcp.CallToolResult, error) {
	// Already imported?
	exact := strconv.Quote(in.ExternalID)
	resp, err := r.c.Get(ctx, "/filament", url.Values{"external_id": {exact}})
	if err != nil {
		return nil, err
	}
	var existing []json.RawMessage
	if json.Unmarshal(resp.Body, &existing) == nil && len(existing) > 0 {
		return valueResult(map[string]any{"created": false, "filament": existing[0]})
	}

	resp, err = r.c.Get(ctx, "/external/filament", nil)
	if err != nil {
		return nil, err
	}
	var all []map[string]any
	if err := json.Unmarshal(resp.Body, &all); err != nil {
		return nil, fmt.Errorf("decoding external filaments: %w", err)
	}
	var ext map[string]any
	for _, f := range all {
		if f["id"] == in.ExternalID {
			ext = f
			break
		}
	}
	if ext == nil {
		return nil, fmt.Errorf("external filament %q not found; use search_external_filaments", in.ExternalID)
	}

	vendorID := 0
	vendorCreated := false
	if in.VendorID != nil {
		vendorID = *in.VendorID
	} else {
		mfr, _ := ext["manufacturer"].(string)
		vresp, err := r.c.Get(ctx, "/vendor", url.Values{"name": {strconv.Quote(mfr)}})
		if err != nil {
			return nil, err
		}
		var vendors []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal(vresp.Body, &vendors)
		for _, v := range vendors {
			if strings.EqualFold(v.Name, mfr) {
				vendorID = v.ID
				break
			}
		}
		if vendorID == 0 {
			body := map[string]any{"name": mfr, "external_id": mfr}
			if w, ok := ext["spool_weight"].(float64); ok {
				body["empty_spool_weight"] = w
			}
			cr, err := r.c.Do(ctx, http.MethodPost, "/vendor", nil, body)
			if err != nil {
				return nil, err
			}
			var v struct {
				ID int `json:"id"`
			}
			if err := json.Unmarshal(cr.Body, &v); err != nil {
				return nil, err
			}
			vendorID = v.ID
			vendorCreated = true
		}
	}

	body := map[string]any{
		"name":        ext["name"],
		"vendor_id":   vendorID,
		"material":    ext["material"],
		"density":     ext["density"],
		"diameter":    ext["diameter"],
		"weight":      ext["weight"],
		"external_id": in.ExternalID,
	}
	copyIf := func(src, dst string) {
		if v, ok := ext[src]; ok && v != nil {
			body[dst] = v
		}
	}
	copyIf("spool_weight", "spool_weight")
	copyIf("extruder_temp", "settings_extruder_temp")
	copyIf("bed_temp", "settings_bed_temp")
	if hexes, ok := ext["color_hexes"].([]any); ok && len(hexes) > 1 {
		parts := make([]string, 0, len(hexes))
		for _, h := range hexes {
			parts = append(parts, fmt.Sprint(h))
		}
		body["multi_color_hexes"] = strings.Join(parts, ",")
		dir, _ := ext["multi_color_direction"].(string)
		if dir == "" {
			dir = "coaxial"
		}
		body["multi_color_direction"] = dir
	} else {
		copyIf("color_hex", "color_hex")
	}
	cr, err := r.c.Do(ctx, http.MethodPost, "/filament", nil, body)
	if err != nil {
		return nil, err
	}
	return valueResult(map[string]any{"created": true, "vendor_created": vendorCreated, "filament": json.RawMessage(cr.Body)})
}

func (r *Registry) registerDelete() {
	for _, e := range []struct{ name, path, title string }{
		{"delete_spool", "/spool/", "Delete spool"},
		{"delete_filament", "/filament/", "Delete filament"},
		{"delete_vendor", "/vendor/", "Delete vendor"},
	} {
		add(r, toolDef{kind: KindDelete, name: e.name, title: e.title,
			description: e.title + " permanently. Irreversible: requires confirm=true. To retire a spool, prefer update_spool with archived=true."},
			func(ctx context.Context, in deleteInput) (*mcp.CallToolResult, error) {
				if !in.Confirm {
					return nil, errConfirm
				}
				return r.send(ctx, http.MethodDelete, e.path+strconv.Itoa(in.ID), nil)
			})
	}
	add(r, toolDef{kind: KindDelete, name: "delete_extra_field", title: "Delete extra field",
		description: "Delete a custom (extra) field definition and its values on all objects. Irreversible: requires confirm=true.",
		enums:       map[string][]any{"entity_type": entityTypes}},
		func(ctx context.Context, in deleteFieldInput) (*mcp.CallToolResult, error) {
			if !in.Confirm {
				return nil, errConfirm
			}
			et, err := seg(in.EntityType)
			if err != nil {
				return nil, err
			}
			key, err := seg(in.Key)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodDelete, "/field/"+et+"/"+key, nil)
		})
	add(r, toolDef{kind: KindDelete, name: "unlink_tag", title: "Unlink NFC tag",
		description: "Remove the link between an NFC/RFID tag and a spool or filament. Requires confirm=true.",
		enums:       map[string][]any{"entity": {"spool", "filament"}}},
		func(ctx context.Context, in unlinkTagInput) (*mcp.CallToolResult, error) {
			if !in.Confirm {
				return nil, errConfirm
			}
			prefix, ok := tagEntityPaths[in.Entity]
			if !ok {
				return nil, fmt.Errorf("invalid entity %q", in.Entity)
			}
			uid, err := seg(in.UID)
			if err != nil {
				return nil, err
			}
			return r.send(ctx, http.MethodDelete, prefix+strconv.Itoa(in.ID)+"/tag/"+uid, nil)
		})
}
