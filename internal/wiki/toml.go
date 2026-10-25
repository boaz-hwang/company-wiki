package wiki

import (
	"fmt"
	"strconv"
	"strings"
)

// parseTOML reads the subset of TOML that wiki.toml uses: [table] and [a.b]
// headers, bare or quoted keys, basic and literal strings, booleans, integers
// and arrays of those values. Anything else is reported as an error.
func parseTOML(text string) (map[string]any, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	root := map[string]any{}
	current := root
	p := &tomlParser{src: []rune(text), line: 1}
	for {
		p.skipSpaceAndComments(true)
		if p.done() {
			return root, nil
		}
		if p.peek() == '[' {
			p.pos++
			keys, err := p.keyPath(']')
			if err != nil {
				return nil, err
			}
			p.pos++
			table := root
			for _, k := range keys {
				next, ok := table[k]
				if !ok {
					m := map[string]any{}
					table[k] = m
					table = m
					continue
				}
				m, ok := next.(map[string]any)
				if !ok {
					return nil, p.errorf("%s is not a table", k)
				}
				table = m
			}
			current = table
			if err := p.endOfLine(); err != nil {
				return nil, err
			}
			continue
		}
		keys, err := p.keyPath('=')
		if err != nil {
			return nil, err
		}
		p.pos++
		p.skipSpaceAndComments(false)
		value, err := p.value()
		if err != nil {
			return nil, err
		}
		table := current
		for _, k := range keys[:len(keys)-1] {
			m, ok := table[k].(map[string]any)
			if !ok {
				m = map[string]any{}
				table[k] = m
			}
			table = m
		}
		last := keys[len(keys)-1]
		if _, exists := table[last]; exists {
			return nil, p.errorf("duplicate key %s", last)
		}
		table[last] = value
		if err := p.endOfLine(); err != nil {
			return nil, err
		}
	}
}

type tomlParser struct {
	src  []rune
	pos  int
	line int
}

func (p *tomlParser) done() bool { return p.pos >= len(p.src) }
func (p *tomlParser) peek() rune {
	if p.done() {
		return 0
	}
	return p.src[p.pos]
}

func (p *tomlParser) errorf(format string, args ...any) error {
	return fmt.Errorf("wiki.toml line %d: %s", p.line, fmt.Sprintf(format, args...))
}

func (p *tomlParser) skipSpaceAndComments(newlines bool) {
	for !p.done() {
		switch c := p.peek(); {
		case c == ' ' || c == '\t' || c == '\r':
			p.pos++
		case c == '\n' && newlines:
			p.pos++
			p.line++
		case c == '#':
			for !p.done() && p.peek() != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

func (p *tomlParser) endOfLine() error {
	p.skipSpaceAndComments(false)
	if p.done() {
		return nil
	}
	if p.peek() != '\n' {
		return p.errorf("unexpected %q", p.peek())
	}
	return nil
}

func (p *tomlParser) keyPath(end rune) ([]string, error) {
	var keys []string
	for {
		p.skipSpaceAndComments(false)
		var key string
		switch c := p.peek(); {
		case c == '"' || c == '\'':
			s, err := p.str()
			if err != nil {
				return nil, err
			}
			key = s
		default:
			start := p.pos
			for !p.done() {
				c := p.peek()
				if c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
					p.pos++
					continue
				}
				break
			}
			key = string(p.src[start:p.pos])
		}
		if key == "" {
			return nil, p.errorf("missing key")
		}
		keys = append(keys, key)
		p.skipSpaceAndComments(false)
		switch p.peek() {
		case '.':
			p.pos++
		case end:
			return keys, nil
		default:
			return nil, p.errorf("expected %q after key %s", end, key)
		}
	}
}

func (p *tomlParser) value() (any, error) {
	switch c := p.peek(); {
	case c == '"' || c == '\'':
		return p.str()
	case c == '[':
		p.pos++
		var out []any
		for {
			p.skipSpaceAndComments(true)
			if p.peek() == ']' {
				p.pos++
				return out, nil
			}
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			p.skipSpaceAndComments(true)
			switch p.peek() {
			case ',':
				p.pos++
			case ']':
			default:
				return nil, p.errorf("expected , or ] in array")
			}
		}
	default:
		start := p.pos
		for !p.done() && !strings.ContainsRune(" \t\r\n#,]", p.peek()) {
			p.pos++
		}
		word := string(p.src[start:p.pos])
		switch word {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		if n, err := strconv.ParseInt(strings.ReplaceAll(word, "_", ""), 10, 64); err == nil {
			return n, nil
		}
		return nil, p.errorf("unsupported value %q", word)
	}
}

func (p *tomlParser) str() (string, error) {
	quote := p.peek()
	p.pos++
	var b strings.Builder
	for !p.done() {
		c := p.peek()
		p.pos++
		switch {
		case c == quote:
			return b.String(), nil
		case c == '\n':
			return "", p.errorf("unterminated string")
		case c == '\\' && quote == '"':
			if p.done() {
				return "", p.errorf("unterminated escape")
			}
			e := p.peek()
			p.pos++
			switch e {
			case 'n':
				b.WriteRune('\n')
			case 't':
				b.WriteRune('\t')
			case 'r':
				b.WriteRune('\r')
			case '"', '\\':
				b.WriteRune(e)
			case 'u', 'U':
				size := 4
				if e == 'U' {
					size = 8
				}
				if p.pos+size > len(p.src) {
					return "", p.errorf("bad unicode escape")
				}
				n, err := strconv.ParseUint(string(p.src[p.pos:p.pos+size]), 16, 32)
				if err != nil {
					return "", p.errorf("bad unicode escape")
				}
				b.WriteRune(rune(n))
				p.pos += size
			default:
				return "", p.errorf("unsupported escape \\%c", e)
			}
		default:
			b.WriteRune(c)
		}
	}
	return "", p.errorf("unterminated string")
}
