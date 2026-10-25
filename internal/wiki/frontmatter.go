package wiki

import (
	"regexp"
	"strings"
)

// Frontmatter values are string, bool, []any or map[string]any.
//
// The parser supports the subset this wiki uses:
//   - `key: value` scalars (quoted or not)
//   - `key: [a, b, c]` inline lists
//   - `key: { a: 1, b: 2 }` one-level inline mappings
//   - `key:` followed by `  - item` block lists (scalars or `- k: v` mappings)
type Frontmatter map[string]any

var (
	fmRE      = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n?`)
	mapItemRE = regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_-]*:(?:\s|$)`)
)

func scalar(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) >= 2 && s[0] == s[len(s)-1] && (s[0] == '"' || s[0] == '\'') {
		return s[1 : len(s)-1]
	}
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	}
	if s == "[]" {
		return []any{}
	}
	if s == "{}" {
		return map[string]any{}
	}
	return s
}

func splitTop(s string, sep rune) []string {
	var out []string
	depth := 0
	var quote rune
	var cur strings.Builder
	for _, ch := range s {
		if quote != 0 {
			cur.WriteRune(ch)
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch {
		case ch == '"' || ch == '\'':
			quote = ch
			cur.WriteRune(ch)
		case ch == '[' || ch == '{':
			depth++
			cur.WriteRune(ch)
		case ch == ']' || ch == '}':
			depth--
			cur.WriteRune(ch)
		case ch == sep && depth == 0:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(ch)
		}
	}
	if strings.TrimSpace(cur.String()) != "" {
		out = append(out, cur.String())
	}
	return out
}

func inline(s string) any {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		out := []any{}
		if inner == "" {
			return out
		}
		for _, x := range splitTop(inner, ',') {
			out = append(out, inline(x))
		}
		return out
	}
	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		d := map[string]any{}
		for _, part := range splitTop(inner, ',') {
			if k, v, ok := strings.Cut(part, ":"); ok {
				d[strings.TrimSpace(k)] = inline(v)
			}
		}
		return d
	}
	return scalar(s)
}

// ParseFrontmatter returns nil when the page has no frontmatter.
func ParseFrontmatter(text string) (Frontmatter, string) {
	text = strings.ReplaceAll(strings.TrimPrefix(text, "\ufeff"), "\r\n", "\n")
	m := fmRE.FindStringSubmatchIndex(text)
	if m == nil {
		return nil, text
	}
	block, body := text[m[2]:m[3]], text[m[1]:]
	lines := strings.Split(block, "\n")
	data := Frontmatter{}
	for i := 0; i < len(lines); {
		line := lines[i]
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") ||
			strings.HasPrefix(line, " ") || !strings.Contains(line, ":") {
			i++
			continue
		}
		key, rest, _ := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if t := strings.TrimSpace(rest); !strings.HasPrefix(t, `"`) && !strings.HasPrefix(t, "'") {
			rest, _, _ = strings.Cut(rest, " #")
		}
		rest = strings.TrimSpace(rest)
		if rest != "" {
			data[key] = inline(rest)
			i++
			continue
		}
		// Block list or nested mapping.
		items := []any{}
		var curMap map[string]any
		j := i + 1
		for j < len(lines) && (strings.HasPrefix(lines[j], "  ") || strings.TrimSpace(lines[j]) == "") {
			stripped := strings.TrimSpace(lines[j])
			j++
			if stripped == "" {
				continue
			}
			if strings.HasPrefix(stripped, "- ") {
				content := strings.TrimSpace(stripped[2:])
				// `- key: value` starts a mapping; citations and URLs (`^[/a.md:1]`, `https://`) stay scalars.
				if mapItemRE.MatchString(content) {
					k, v, _ := strings.Cut(content, ":")
					curMap = map[string]any{strings.TrimSpace(k): inline(v)}
					items = append(items, curMap)
				} else {
					curMap = nil
					items = append(items, inline(content))
				}
			} else if curMap != nil && strings.Contains(stripped, ":") {
				k, v, _ := strings.Cut(stripped, ":")
				curMap[strings.TrimSpace(k)] = inline(v)
			}
		}
		data[key] = items
		i = j
	}
	return data, body
}

// truthy follows Python truthiness for frontmatter values.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// str renders a value the way Python's str() would for the cases pages use.
func str(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmtAny(v), "\n", " "), "  ", " "))
}

func (f Frontmatter) str(key string) string {
	if v, ok := f[key]; ok {
		return str(v)
	}
	return ""
}

// Relation is one related entry: a slug, or {target, type, evidence}.
type Relation struct {
	Target, Type, Evidence string
	HasType, HasEvidence   bool
}

func relations(fm Frontmatter) []Relation {
	var out []Relation
	list, _ := fm["related"].([]any)
	for _, r := range list {
		switch x := r.(type) {
		case string:
			if strings.TrimSpace(x) != "" {
				out = append(out, Relation{Target: strings.TrimSpace(x)})
			}
		case map[string]any:
			rel := Relation{Target: str(x["target"])}
			if x["target"] == nil {
				rel.Target = ""
			}
			if t, ok := x["type"]; ok {
				rel.Type, rel.HasType = str(t), true
			}
			if e, ok := x["evidence"]; ok {
				rel.Evidence, rel.HasEvidence = str(e), true
			}
			out = append(out, rel)
		}
	}
	return out
}

func asList(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			if s := str(item); strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	case string:
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}
