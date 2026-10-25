package wiki

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// pyJSON encodes like Python's json.dumps(value, ensure_ascii=False[, sort_keys=True]):
// ", " and ": " separators, no HTML escaping. Review revisions are hashes of this form.
func pyJSON(v any, sortKeys bool) string {
	var b strings.Builder
	writePyJSON(&b, v, sortKeys)
	return b.String()
}

func writePyJSON(b *strings.Builder, v any, sortKeys bool) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writePyString(b, x)
	case json.Number:
		b.WriteString(x.String())
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		if x == float64(int64(x)) {
			b.WriteString(strconv.FormatFloat(x, 'f', 1, 64))
		} else {
			b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
		}
	case *string:
		if x == nil {
			b.WriteString("null")
		} else {
			writePyString(b, *x)
		}
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			writePyJSON(b, item, sortKeys)
		}
		b.WriteByte(']')
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		writePyJSON(b, items, sortKeys)
	case *omap:
		keys := x.keys
		if sortKeys {
			keys = append([]string(nil), keys...)
			sort.Strings(keys)
		}
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writePyString(b, k)
			b.WriteString(": ")
			writePyJSON(b, x.m[k], sortKeys)
		}
		b.WriteByte('}')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		// Go maps have no insertion order; sorted keys are the only stable choice.
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			writePyString(b, k)
			b.WriteString(": ")
			writePyJSON(b, x[k], sortKeys)
		}
		b.WriteByte('}')
	default:
		// Structs and typed maps: round-trip through encoding/json.
		raw, err := json.Marshal(x)
		if err != nil {
			b.WriteString(fmt.Sprintf("%q", fmt.Sprint(x)))
			return
		}
		var generic any
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		_ = dec.Decode(&generic)
		writePyJSON(b, generic, sortKeys)
	}
}

func writePyString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// omap is a JSON object that remembers key order, so output matches Python's insertion order.
type omap struct {
	keys []string
	m    map[string]any
}

// decodeOrdered parses JSON keeping object key order and number literals.
func decodeOrdered(data []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("extra data after JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &omap{m: map[string]any{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k := kt.(string)
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				if _, dup := o.m[k]; !dup {
					o.keys = append(o.keys, k)
				}
				o.m[k] = v
			}
			_, err := dec.Token()
			return o, err
		case '[':
			list := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			_, err := dec.Token()
			return list, err
		}
	}
	return tok, nil
}

// plain converts ordered objects to ordinary maps (for lookups and encoding/json).
func plain(v any) any {
	switch x := v.(type) {
	case *omap:
		out := make(map[string]any, len(x.m))
		for k, val := range x.m {
			out[k] = plain(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = plain(val)
		}
		return out
	}
	return v
}
