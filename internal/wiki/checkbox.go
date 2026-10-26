package wiki

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"
)

const (
	keyUp = iota + 1
	keyDown
	keySpace
	keyEnter
	keyAbort
)

var errCancelled = errors.New("취소했습니다.")

type checkState struct {
	cursor   int
	selected map[int]bool
	count    int
}

// applyKey moves, toggles or confirms; enter needs at least one selection.
func applyKey(s checkState, key int) (checkState, bool, error) {
	switch key {
	case keyUp:
		s.cursor = (s.cursor - 1 + s.count) % s.count
	case keyDown:
		s.cursor = (s.cursor + 1) % s.count
	case keySpace:
		next := map[int]bool{}
		for k, v := range s.selected {
			next[k] = v
		}
		next[s.cursor] = !next[s.cursor]
		if !next[s.cursor] {
			delete(next, s.cursor)
		}
		s.selected = next
	case keyEnter:
		if len(s.selected) > 0 {
			return s, true, nil
		}
	case keyAbort:
		return s, false, errCancelled
	}
	return s, false, nil
}

// readChunk reads whatever the terminal has in raw mode; keys typed or pasted together arrive in one chunk.
func readChunk(in *os.File) ([]byte, error) {
	old, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, err
	}
	defer term.Restore(int(in.Fd()), old)
	buf := make([]byte, 256)
	n, err := in.Read(buf)
	return buf[:n], err
}

// parseKeys splits a chunk into keys in order: ESC [ A / ESC [ B or k/j, space, enter (\r, \n or \r\n),
// q or Ctrl-C. Other bytes are ignored. ends[i] is the offset just after keys[i].
func parseKeys(b []byte) (keys, ends []int) {
	for i := 0; i < len(b); i++ {
		key := 0
		switch c := b[i]; {
		case c == 0x1b && i+2 < len(b) && b[i+1] == '[' && (b[i+2] == 'A' || b[i+2] == 'B'):
			key = keyUp
			if b[i+2] == 'B' {
				key = keyDown
			}
			i += 2
		case c == ' ':
			key = keySpace
		case c == '\r' || c == '\n':
			key = keyEnter
			if c == '\r' && i+1 < len(b) && b[i+1] == '\n' {
				i++
			}
		case c == 3 || c == 'q':
			key = keyAbort
		case c == 'k':
			key = keyUp
		case c == 'j':
			key = keyDown
		}
		if key != 0 {
			keys, ends = append(keys, key), append(ends, i+1)
		}
	}
	return keys, ends
}

// runKeys applies a chunk's keys in order. When enter confirms, rest holds the bytes after it,
// which belong to the prompts that follow.
func runKeys(s checkState, chunk []byte) (next checkState, done bool, rest []byte, err error) {
	keys, ends := parseKeys(chunk)
	for i, key := range keys {
		if s, done, err = applyKey(s, key); err != nil || done {
			return s, done, chunk[ends[i]:], err
		}
	}
	return s, false, nil, nil
}

type item struct{ key, label string }

func renderChecklist(out io.Writer, title string, items []item, s checkState, first bool) {
	if !first {
		fmt.Fprintf(out, "\x1b[%dA", len(items)+1) // back to the top of the list
	}
	fmt.Fprintf(out, "\x1b[2K%s\n", title)
	for i, it := range items {
		mark, pointer := "[ ]", " "
		if s.selected[i] {
			mark = "[x]"
		}
		if i == s.cursor {
			pointer = "❯"
		}
		fmt.Fprintf(out, "\x1b[2K %s %s %-11s %s\n", pointer, mark, it.key, it.label)
	}
}

// checkbox returns the selected keys in list order and any bytes read after the confirming enter.
func checkbox(in *os.File, out io.Writer, title string, items []item) ([]string, []byte, error) {
	enableVT(out)
	s := checkState{selected: map[int]bool{}, count: len(items)}
	hint := title + "  (↑↓ 이동 · space 선택 · enter 확인 · q 취소)"
	renderChecklist(out, hint, items, s, true)
	for {
		chunk, err := readChunk(in)
		if err != nil {
			return nil, nil, err
		}
		var done bool
		var rest []byte
		if s, done, rest, err = runKeys(s, chunk); err != nil {
			return nil, nil, err
		}
		renderChecklist(out, hint, items, s, false)
		if done {
			var idx []int
			for i := range s.selected {
				idx = append(idx, i)
			}
			sort.Ints(idx)
			keys := make([]string, len(idx))
			for i, n := range idx {
				keys[i] = items[n].key
			}
			return keys, rest, nil
		}
	}
}

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func trimLine(s string) string { return strings.TrimRight(s, "\r\n") }

// lineReader reads answers first from bytes that arrived with the checklist keys, then from the terminal.
// Raw mode did not echo those bytes, so it echoes them; their raw enter (\r) ends a line.
type lineReader struct {
	pending *bufio.Reader
	term    *bufio.Reader
	echo    io.Writer
}

func newLineReader(pending []byte, in io.Reader, echo io.Writer) *lineReader {
	r := &lineReader{term: bufio.NewReader(in), echo: echo}
	if len(pending) > 0 {
		text := strings.ReplaceAll(strings.ReplaceAll(string(pending), "\r\n", "\n"), "\r", "\n")
		r.pending = bufio.NewReader(strings.NewReader(text))
	}
	return r
}

func (r *lineReader) ReadString(delim byte) (string, error) {
	head := ""
	if r.pending != nil {
		s, err := r.pending.ReadString(delim)
		fmt.Fprint(r.echo, s)
		if err == nil {
			return s, nil
		}
		r.pending, head = nil, s
	}
	s, err := r.term.ReadString(delim)
	return head + s, err
}
