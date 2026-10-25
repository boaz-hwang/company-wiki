package wiki

import (
	"fmt"
	"sort"
	"strings"
)

type opcode struct {
	tag            string
	i1, i2, j1, j2 int
}

// opcodes follows difflib.SequenceMatcher(None, a, b).get_opcodes(), including autojunk,
// so review documents match the ones the Python tool rendered.
func opcodes(a, b []string) []opcode {
	b2j := map[string][]int{}
	for j, line := range b {
		b2j[line] = append(b2j[line], j)
	}
	if n := len(b); n >= 200 {
		ntest := n/100 + 1
		for line, idxs := range b2j {
			if len(idxs) > ntest {
				delete(b2j, line)
			}
		}
	}
	longest := func(alo, ahi, blo, bhi int) (int, int, int) {
		besti, bestj, bestsize := alo, blo, 0
		j2len := map[int]int{}
		for i := alo; i < ahi; i++ {
			next := map[int]int{}
			for _, j := range b2j[a[i]] {
				if j < blo {
					continue
				}
				if j >= bhi {
					break
				}
				k := j2len[j-1] + 1
				next[j] = k
				if k > bestsize {
					besti, bestj, bestsize = i-k+1, j-k+1, k
				}
			}
			j2len = next
		}
		for besti > alo && bestj > blo && a[besti-1] == b[bestj-1] {
			besti, bestj, bestsize = besti-1, bestj-1, bestsize+1
		}
		for besti+bestsize < ahi && bestj+bestsize < bhi && a[besti+bestsize] == b[bestj+bestsize] {
			bestsize++
		}
		return besti, bestj, bestsize
	}
	type block struct{ i, j, k int }
	var blocks []block
	queue := [][4]int{{0, len(a), 0, len(b)}}
	for len(queue) > 0 {
		q := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		i, j, k := longest(q[0], q[1], q[2], q[3])
		if k == 0 {
			continue
		}
		blocks = append(blocks, block{i, j, k})
		if q[0] < i && q[2] < j {
			queue = append(queue, [4]int{q[0], i, q[2], j})
		}
		if i+k < q[1] && j+k < q[3] {
			queue = append(queue, [4]int{i + k, q[1], j + k, q[3]})
		}
	}
	sort.Slice(blocks, func(x, y int) bool {
		if blocks[x].i != blocks[y].i {
			return blocks[x].i < blocks[y].i
		}
		if blocks[x].j != blocks[y].j {
			return blocks[x].j < blocks[y].j
		}
		return blocks[x].k < blocks[y].k
	})
	var merged []block
	cur := block{}
	for _, bl := range blocks {
		if cur.i+cur.k == bl.i && cur.j+cur.k == bl.j {
			cur.k += bl.k
			continue
		}
		if cur.k > 0 {
			merged = append(merged, cur)
		}
		cur = bl
	}
	if cur.k > 0 {
		merged = append(merged, cur)
	}
	merged = append(merged, block{len(a), len(b), 0})
	var ops []opcode
	i, j := 0, 0
	for _, bl := range merged {
		switch {
		case i < bl.i && j < bl.j:
			ops = append(ops, opcode{"replace", i, bl.i, j, bl.j})
		case i < bl.i:
			ops = append(ops, opcode{"delete", i, bl.i, j, bl.j})
		case j < bl.j:
			ops = append(ops, opcode{"insert", i, bl.i, j, bl.j})
		}
		i, j = bl.i+bl.k, bl.j+bl.k
		if bl.k > 0 {
			ops = append(ops, opcode{"equal", bl.i, i, bl.j, j})
		}
	}
	return ops
}

// groupedOpcodes follows difflib.SequenceMatcher.get_grouped_opcodes.
func groupedOpcodes(codes []opcode, n int) [][]opcode {
	if len(codes) == 0 {
		codes = []opcode{{"equal", 0, 1, 0, 1}}
	}
	codes = append([]opcode(nil), codes...)
	if c := &codes[0]; c.tag == "equal" {
		c.i1, c.j1 = max(c.i1, c.i2-n), max(c.j1, c.j2-n)
	}
	if c := &codes[len(codes)-1]; c.tag == "equal" {
		c.i2, c.j2 = min(c.i2, c.i1+n), min(c.j2, c.j1+n)
	}
	var groups [][]opcode
	var group []opcode
	for _, c := range codes {
		if c.tag == "equal" && c.i2-c.i1 > 2*n {
			group = append(group, opcode{c.tag, c.i1, min(c.i2, c.i1+n), c.j1, min(c.j2, c.j1+n)})
			groups = append(groups, group)
			group = nil
			c.i1, c.j1 = max(c.i1, c.i2-n), max(c.j1, c.j2-n)
		}
		group = append(group, c)
	}
	if len(group) > 0 && !(len(group) == 1 && group[0].tag == "equal") {
		groups = append(groups, group)
	}
	return groups
}

func formatRange(start, stop int) string {
	beginning, length := start+1, stop-start
	if length == 1 {
		return fmt.Sprint(beginning)
	}
	if length == 0 {
		beginning--
	}
	return fmt.Sprintf("%d,%d", beginning, length)
}

// unifiedDiff matches difflib.unified_diff for lines that keep their endings.
func unifiedDiff(a, b []string, from, to string) string {
	var out strings.Builder
	for k, group := range groupedOpcodes(opcodes(a, b), 3) {
		if k == 0 {
			out.WriteString("--- " + from + "\n+++ " + to + "\n")
		}
		first, last := group[0], group[len(group)-1]
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", formatRange(first.i1, last.i2), formatRange(first.j1, last.j2))
		for _, c := range group {
			if c.tag == "equal" {
				for _, line := range a[c.i1:c.i2] {
					out.WriteString(" " + line)
				}
				continue
			}
			if c.tag == "replace" || c.tag == "delete" {
				for _, line := range a[c.i1:c.i2] {
					out.WriteString("-" + line)
				}
			}
			if c.tag == "replace" || c.tag == "insert" {
				for _, line := range b[c.j1:c.j2] {
					out.WriteString("+" + line)
				}
			}
		}
	}
	return out.String()
}

// textLines splits text like Python str.splitlines(keepends=True) for \n, \r\n and \r.
func textLines(s string) []string {
	var out []string
	for _, l := range splitLines([]byte(s)) {
		out = append(out, string(l))
	}
	return out
}
