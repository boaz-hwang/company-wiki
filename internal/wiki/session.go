package wiki

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type roleText struct{ role, text string }

func obj(v any) map[string]any {
	if o, ok := v.(*omap); ok {
		return o.m
	}
	m, _ := v.(map[string]any)
	return m
}

func sget(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func blocks(content any) string {
	switch x := content.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, item := range x {
			if m := obj(item); m != nil {
				parts = append(parts, sget(m, "text"))
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// sessionMessages yields the visible messages of one Claude or Codex JSONL record.
func sessionMessages(record map[string]any, tools bool) []roleText {
	var out []roleText
	switch kind := sget(record, "type"); kind {
	case "attachment":
		item := obj(record["attachment"])
		if sget(item, "type") == "queued_command" {
			role := "queued_context"
			if sget(obj(item["origin"]), "kind") == "human" {
				role = "user"
			}
			out = append(out, roleText{role, sget(item, "prompt")})
		}
	case "user", "assistant":
		content := obj(record["message"])["content"]
		if s, ok := content.(string); ok {
			return append(out, roleText{kind, s})
		}
		list, _ := content.([]any)
		for _, raw := range list {
			item := obj(raw)
			switch sget(item, "type") {
			case "text":
				out = append(out, roleText{kind, sget(item, "text")})
			case "tool_use":
				if tools {
					input, ok := item["input"]
					if !ok {
						input = map[string]any{}
					}
					out = append(out, roleText{"tool " + sget(item, "name"), pyJSON(input, false)})
				}
			case "tool_result":
				if tools {
					out = append(out, roleText{"tool_result", blocks(item["content"])})
				}
			}
		}
	case "response_item":
		payload := obj(record["payload"])
		ptype, role := sget(payload, "type"), sget(payload, "role")
		if ptype == "message" && (role == "user" || role == "assistant") {
			out = append(out, roleText{role, blocks(payload["content"])})
		} else if tools && (ptype == "function_call" || ptype == "function_call_output" || ptype == "custom_tool_call" || ptype == "custom_tool_call_output") {
			v := payload["arguments"]
			if v == nil {
				v = payload["input"]
			}
			if v == nil {
				v = payload["output"]
			}
			out = append(out, roleText{ptype, pyStr(v)})
		}
	}
	return out
}

// pyStr renders a value like Python's str() for session payloads.
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	}
	return fmt.Sprint(plain(v))
}

func decodeRecord(line []byte) (map[string]any, error) {
	v, err := decodeOrdered(line)
	if err != nil {
		return nil, err
	}
	m := obj(v)
	if m == nil {
		return nil, fmt.Errorf("record is not an object")
	}
	return m, nil
}

// SessionCmd prints Claude/Codex messages with original JSONL line numbers, without truncation.
func SessionCmd(args []string, x IO) int {
	opts, pos, err := parseFlags(args, map[string]bool{"start": true, "end": true, "max-chars": true}, map[string]bool{"tools": true})
	if err != nil || len(pos) != 1 {
		fmt.Fprintln(x.Stderr, "usage: company-wiki session <path or /claude-sessions/...> [--start N] [--end M] [--tools] [--max-chars N]")
		return 2
	}
	num := func(name string, fallback int) (int, error) {
		if opts[name] == "" {
			return fallback, nil
		}
		return strconv.Atoi(opts[name])
	}
	start, e1 := num("start", 1)
	end, e2 := num("end", 0)
	maxChars, e3 := num("max-chars", 0)
	if e1 != nil || e2 != nil || e3 != nil || start < 1 || (opts["end"] != "" && end < start) || maxChars < 0 {
		fmt.Fprintln(x.Stderr, "invalid line range or character limit")
		return 2
	}
	path := expandUser(pos[0])
	head, _, _ := strings.Cut(strings.TrimLeft(pos[0], "/"), "/")
	if c, err := LoadConfig(x.Env, x.Dir); err == nil && c.ExtraRoots[head] != "" {
		if path, err = c.resolveResource(pos[0]); err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
			return 1
		}
	} else {
		path = resolvePath(path, x.Dir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
		return 1
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var records []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 1<<30)
	for scanner.Scan() {
		r, err := decodeRecord(scanner.Bytes())
		if err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: line %d: %v\n", len(records)+1, err)
			return 1
		}
		records = append(records, r)
	}
	// Codex stores the same visible messages in event_msg and response_item.
	// Prefer response_item when present; older event-only logs still work.
	hasMessages := false
	for _, r := range records {
		p := obj(r["payload"])
		if sget(r, "type") == "response_item" && sget(p, "type") == "message" && (sget(p, "role") == "user" || sget(p, "role") == "assistant") {
			hasMessages = true
			break
		}
	}
	for i, r := range records {
		number := i + 1
		if number < start {
			continue
		}
		if opts["end"] != "" && number > end {
			break
		}
		output := sessionMessages(r, opts["tools"] != "")
		if !hasMessages && sget(r, "type") == "event_msg" {
			p := obj(r["payload"])
			role := map[string]string{"user_message": "user", "agent_message": "assistant"}[sget(p, "type")]
			if role != "" {
				output = append(output, roleText{role, sget(p, "message")})
			}
		}
		for _, m := range output {
			if strings.TrimSpace(m.text) == "" {
				continue
			}
			text := m.text
			if runes := []rune(text); maxChars > 0 && len(runes) > maxChars {
				text = string(runes[:maxChars]) + "\n[TRUNCATED: reread without --max-chars before citing]"
			}
			fmt.Fprintf(x.Stdout, "L%d [%s]\n%s\n\n", number, m.role, text)
		}
	}
	return 0
}
