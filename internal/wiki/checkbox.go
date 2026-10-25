package wiki

import (
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

// readKey reads one key in raw mode: arrows (or j/k), space, enter, q or Ctrl-C.
func readKey(in *os.File) (int, error) {
	old, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return 0, err
	}
	defer term.Restore(int(in.Fd()), old)
	buf := make([]byte, 8)
	n, err := in.Read(buf)
	if err != nil {
		return 0, err
	}
	b := buf[:n]
	switch {
	case len(b) >= 3 && b[0] == 0x1b && b[1] == '[' && b[2] == 'A':
		return keyUp, nil
	case len(b) >= 3 && b[0] == 0x1b && b[1] == '[' && b[2] == 'B':
		return keyDown, nil
	case len(b) == 0:
		return 0, nil
	}
	switch b[0] {
	case ' ':
		return keySpace, nil
	case '\r', '\n':
		return keyEnter, nil
	case 3, 'q':
		return keyAbort, nil
	case 'k':
		return keyUp, nil
	case 'j':
		return keyDown, nil
	}
	return 0, nil
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

// checkbox returns the selected keys in list order.
func checkbox(in *os.File, out io.Writer, title string, items []item) ([]string, error) {
	enableVT(out)
	s := checkState{selected: map[int]bool{}, count: len(items)}
	hint := title + "  (↑↓ 이동 · space 선택 · enter 확인 · q 취소)"
	renderChecklist(out, hint, items, s, true)
	for {
		key, err := readKey(in)
		if err != nil {
			return nil, err
		}
		var done bool
		if s, done, err = applyKey(s, key); err != nil {
			return nil, err
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
			return keys, nil
		}
	}
}

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func trimLine(s string) string { return strings.TrimRight(s, "\r\n") }
