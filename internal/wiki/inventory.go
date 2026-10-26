package wiki

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

var classifications = map[string]bool{"new": true, "update": true, "conflict": true, "duplicate": true, "out_of_scope": true}

// InventoryResult is the coverage verdict.
type InventoryResult struct {
	Complete   bool     `json:"complete"`
	Through    *string  `json:"through"`
	Incomplete []string `json:"incomplete"`
}

func nonempty(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != ""
}

func timestamp(v any) (time.Time, error) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return time.Time{}, errors.New("period timestamps required")
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		if _, e := time.Parse("2006-01-02T15:04:05", s); e == nil {
			return time.Time{}, errors.New("period timestamps need timezone")
		}
		return time.Time{}, fmt.Errorf("invalid timestamp %q", s)
	}
	return t, nil
}

// ValidateInventory checks declared review coverage; it does not discover sources or prove semantic review.
func ValidateInventory(c *Config, data any) (InventoryResult, error) {
	var res InventoryResult
	fail := func(msg string, args ...any) (InventoryResult, error) { return res, fmt.Errorf(msg, args...) }
	d, ok := data.(map[string]any)
	if !ok || fmt.Sprint(d["version"]) != "1" {
		return fail("inventory version must be 1")
	}
	period, ok := d["period"].(map[string]any)
	if !ok {
		return fail("period must be an object")
	}
	start, err := timestamp(period["from"])
	if err != nil {
		return res, err
	}
	end, err := timestamp(period["through"])
	if err != nil {
		return res, err
	}
	if start.After(end) {
		return fail("period.from must not follow through")
	}
	basis, ok := d["basis"].(map[string]any)
	if !ok {
		return fail("basis must be an object")
	}
	// Each boundary is one note: evidence path, time and scope, or "unknown: <reason>".
	for _, key := range []string{"last_apply", "last_full_review"} {
		if !nonempty(basis[key]) {
			return fail("basis.%s must be a non-empty string (evidence path·time·scope, or \"unknown: <reason>\")", key)
		}
	}
	sources, ok := d["sources"].([]any)
	if !ok {
		return fail("sources must be a list")
	}
	kinds, ids := map[string]bool{}, map[string]bool{}
	incomplete := []string{}
	for _, raw := range sources {
		source, ok := raw.(map[string]any)
		if !ok {
			return fail("source must be an object")
		}
		sid, _ := source["id"].(string)
		if !nonempty(source["id"]) || ids[sid] {
			return fail("source id missing or duplicated")
		}
		ids[sid] = true
		if !nonempty(source["kind"]) || !nonempty(source["location"]) || !nonempty(source["reason"]) {
			return fail("%s: kind/location/reason required", sid)
		}
		kind := source["kind"].(string)
		if c.RequiredSources[sid] {
			if s, _ := c.source(sid); kind != s.Kind {
				return fail("%s: configured source kind mismatch", sid)
			}
		}
		kinds[kind] = true
		status, _ := source["status"].(string)
		switch status {
		case "scanned", "partial", "unavailable", "not_applicable":
		default:
			return fail("%s: invalid status", sid)
		}
		items, ok := source["items"].([]any)
		if !ok {
			return fail("%s: items must be a list", sid)
		}
		if status == "not_applicable" && len(items) > 0 {
			return fail("%s: not_applicable cannot contain items", sid)
		}
		if status == "partial" || status == "unavailable" {
			incomplete = append(incomplete, sid)
		}
		itemIDs := map[string]bool{}
		for _, rawItem := range items {
			item, ok := rawItem.(map[string]any)
			if !ok {
				return fail("%s: item must be an object", sid)
			}
			iid, _ := item["id"].(string)
			if !nonempty(item["id"]) || itemIDs[iid] {
				return fail("%s: item id missing or duplicated", sid)
			}
			itemIDs[iid] = true
			if !nonempty(item["reference"]) || !nonempty(item["reason"]) {
				return fail("%s/%s: reference/reason required", sid, iid)
			}
			switch item["status"] {
			case "unread":
				incomplete = append(incomplete, sid+"/"+iid)
			case "reviewed":
				if c, _ := item["classification"].(string); !classifications[c] {
					return fail("%s/%s: classification required", sid, iid)
				}
				if !nonempty(item["comparison"]) {
					return fail("%s/%s: existing-wiki comparison required", sid, iid)
				}
			default:
				return fail("%s/%s: invalid status", sid, iid)
			}
		}
	}
	var missing []string
	for sid := range c.RequiredSources {
		if !ids[sid] {
			missing = append(missing, sid)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fail("missing configured sources: %s", pyList(missing))
	}
	var missingKinds []string
	for k := range c.RequiredKinds {
		if !kinds[k] {
			missingKinds = append(missingKinds, k)
		}
	}
	if len(missingKinds) > 0 {
		sort.Strings(missingKinds)
		return fail("missing source kinds: %s", pyList(missingKinds))
	}
	res.Complete = len(incomplete) == 0
	res.Incomplete = incomplete
	if res.Complete {
		t := period["through"].(string)
		res.Through = &t
	}
	return res, nil
}

func pyList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = "'" + s + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// InventoryCmd exits 0 when the record is complete, 2 when valid but incomplete with --require-complete, 1 on format errors.
func InventoryCmd(c *Config, args []string, out io.Writer) int {
	// Every result, including usage errors, is one compact JSON line from the same encoder.
	report := func(v any) {
		b, _ := jsonLine(v)
		out.Write(b)
	}
	opts, pos, err := parseFlags(args, nil, map[string]bool{"require-complete": true})
	if err != nil || len(pos) != 1 {
		report(map[string]string{"error": "usage: company-wiki inventory <inventory.json> [--require-complete]"})
		return 1
	}
	raw, err := os.ReadFile(pos[0])
	if err != nil {
		report(map[string]string{"error": err.Error()})
		return 1
	}
	var data any
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))))
	dec.UseNumber()
	if err := dec.Decode(&data); err != nil {
		report(map[string]string{"error": err.Error()})
		return 1
	}
	res, err := ValidateInventory(c, data)
	if err != nil {
		report(map[string]string{"error": err.Error()})
		return 1
	}
	report(res)
	if opts["require-complete"] != "" && !res.Complete {
		return 2
	}
	return 0
}
