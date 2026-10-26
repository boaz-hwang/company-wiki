package wiki

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	companywiki "github.com/boaz-hwang/company-wiki"
)

func inventoryConfig() *Config {
	return &Config{RequiredSources: map[string]bool{}, RequiredKinds: map[string]bool{"documents": true, "claude": true}}
}

func inventoryRecord(c *Config) map[string]any {
	var sources []any
	for _, kind := range sortedKeys(c.RequiredKinds) {
		sources = append(sources, map[string]any{"id": kind, "kind": kind, "location": "/fixture/" + kind, "status": "scanned",
			"reason": "enumerated and compared all period records; no candidates", "items": []any{}})
	}
	return map[string]any{"version": json.Number("1"),
		"period":  map[string]any{"from": "2026-09-01T00:00:00+09:00", "through": "2026-09-16T00:00:00+09:00"},
		"basis":   map[string]any{"last_apply": "log.md: partial client update", "last_full_review": "unknown; reviewed from initial wiki creation"},
		"sources": sources}
}

func clone(v map[string]any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	_ = dec.Decode(&out)
	return out
}

func source0(d map[string]any) map[string]any { return d["sources"].([]any)[0].(map[string]any) }

func TestInventoryRules(t *testing.T) {
	c := inventoryConfig()
	res, err := ValidateInventory(c, inventoryRecord(c))
	if err != nil || res.Through == nil || *res.Through != "2026-09-16T00:00:00+09:00" {
		t.Fatal(res, err)
	}
	for _, status := range []string{"partial", "unavailable"} {
		d := clone(inventoryRecord(c))
		source0(d)["status"] = status
		if res, _ := ValidateInventory(c, d); res.Complete || res.Through != nil {
			t.Fatal(status)
		}
	}
	d := clone(inventoryRecord(c))
	source0(d)["items"] = []any{map[string]any{"id": "a", "reference": "/a", "status": "unread", "reason": "cannot read"}}
	if res, _ := ValidateInventory(c, d); res.Complete {
		t.Fatal("unread counted complete")
	}
	item := map[string]any{"id": "a", "reference": "/a", "status": "reviewed", "reason": "already ingested", "classification": "duplicate", "comparison": "sources/a"}
	d = clone(inventoryRecord(c))
	source0(d)["items"] = []any{item}
	if res, err := ValidateInventory(c, d); err != nil || !res.Complete {
		t.Fatal(err)
	}
	for _, key := range []string{"classification", "comparison"} {
		broken := clone(d)
		delete(source0(broken)["items"].([]any)[0].(map[string]any), key)
		if _, err := ValidateInventory(c, broken); err == nil {
			t.Fatal(key)
		}
	}
	d = clone(inventoryRecord(c))
	d["sources"] = d["sources"].([]any)[:1]
	if _, err := ValidateInventory(c, d); err == nil || !strings.Contains(err.Error(), "missing source kinds") {
		t.Fatal(err)
	}
	d = clone(inventoryRecord(c))
	d["sources"] = append(d["sources"].([]any), d["sources"].([]any)[0])
	if _, err := ValidateInventory(c, d); err == nil {
		t.Fatal("duplicate source accepted")
	}
	for _, period := range []map[string]any{{"from": "2026-09-17T00:00:00Z", "through": "2026-09-16T00:00:00Z"}, {"from": "2026-09-01", "through": "2026-09-16"}} {
		d = clone(inventoryRecord(c))
		d["period"] = period
		if _, err := ValidateInventory(c, d); err == nil {
			t.Fatal(period)
		}
	}
	for key, value := range map[string]any{"last_apply": map[string]any{"path": "applied.jsonl", "at": "2026-09-01T00:00:00Z"}, "last_full_review": "  "} {
		d = clone(inventoryRecord(c))
		d["basis"].(map[string]any)[key] = value
		if _, err := ValidateInventory(c, d); err == nil || !strings.Contains(err.Error(), "basis."+key+" must be a non-empty string") {
			t.Fatal(key, err)
		}
	}
	d = clone(inventoryRecord(c))
	delete(d["basis"].(map[string]any), "last_full_review")
	if _, err := ValidateInventory(c, d); err == nil || !strings.Contains(err.Error(), "basis.last_full_review") {
		t.Fatal("missing basis field", err)
	}
	if _, err := ValidateInventory(c, map[string]any{"complete": true, "applied": []any{"client-a"}}); err == nil {
		t.Fatal("legacy record accepted")
	}
}

func TestInventoryExitCodes(t *testing.T) {
	c := inventoryConfig()
	p := filepath.Join(t.TempDir(), "inventory.json")
	d := clone(inventoryRecord(c))
	source0(d)["status"] = "unavailable"
	b, _ := json.Marshal(d)
	must(t, os.WriteFile(p, b, 0o644))
	var out bytes.Buffer
	if InventoryCmd(c, []string{p, "--require-complete"}, &out) != 2 || InventoryCmd(c, []string{p}, &out) != 0 {
		t.Fatal(out.String())
	}
	must(t, os.WriteFile(p, []byte("{}"), 0o644))
	if InventoryCmd(c, []string{p}, &out) != 1 {
		t.Fatal("invalid record accepted")
	}
}

// Session provenance: original line numbers, full text, and Codex deduplication.
func runSession(t *testing.T, records []any, args ...string) result {
	dir := t.TempDir()
	var lines []string
	for _, r := range records {
		b, _ := json.Marshal(r)
		lines = append(lines, string(b))
	}
	must(t, os.WriteFile(filepath.Join(dir, "sample.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	var out, errOut bytes.Buffer
	env := func(k string) string {
		if k == "WIKI_CLAUDE_SESSIONS_DIR" {
			return dir
		}
		return ""
	}
	target := filepath.Join(dir, "sample.jsonl")
	if len(args) > 0 && args[0] == "alias" {
		target, args = "/claude-sessions/sample.jsonl", args[1:]
	}
	code := SessionCmd(append([]string{target}, args...), IO{Env: env, Dir: dir, Stdout: &out, Stderr: &errOut})
	return result{code, out.String(), errOut.String()}
}

func TestSessionReader(t *testing.T) {
	body := strings.Repeat("근거", 2000) + "끝"
	r := runSession(t, []any{map[string]any{"type": "metadata"}, map[string]any{"type": "user", "message": map[string]any{"content": body}}}, "alias", "--start", "2", "--end", "2")
	if r.code != 0 || !strings.Contains(r.stdout, "L2 [user]\n"+body) {
		t.Fatal(r.stderr)
	}
	tool := []any{map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": "proof"}}}}}
	if runSession(t, tool).stdout != "" || !strings.Contains(runSession(t, tool, "--tools").stdout, "L1 [tool_result]\nproof") {
		t.Fatal("tools must be opt-in")
	}
	queued := []any{
		map[string]any{"type": "queue-operation", "operation": "enqueue", "content": "scope correction"},
		map[string]any{"type": "attachment", "attachment": map[string]any{"type": "queued_command", "origin": map[string]any{"kind": "human"}, "prompt": "scope correction"}},
	}
	if out := runSession(t, queued).stdout; strings.Count(out, "scope correction") != 1 || !strings.Contains(out, "L2 [user]") {
		t.Fatal(out)
	}
	codex := []any{
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": "decision"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "decision"}}}},
	}
	if out := runSession(t, codex).stdout; strings.Count(out, "decision") != 1 || !strings.Contains(out, "L2 [user]") {
		t.Fatal(out)
	}
	if out := runSession(t, []any{map[string]any{"type": "event_msg", "payload": map[string]any{"type": "agent_message", "message": "proposal"}}}).stdout; !strings.Contains(out, "L1 [assistant]\nproposal") {
		t.Fatal(out)
	}
	if out := runSession(t, []any{map[string]any{"type": "user", "message": map[string]any{"content": "abcdef"}}}, "--max-chars", "3").stdout; !strings.Contains(out, "abc\n[TRUNCATED:") || strings.Contains(out, "abcdef") {
		t.Fatal(out)
	}
	if runSession(t, nil, "--start", "3", "--end", "1").code == 0 {
		t.Fatal("invalid range accepted")
	}
}

func TestLockExcludesSecondHolderAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".lock")
	unlock, err := fileLock(path)
	must(t, err)
	acquired := make(chan struct{})
	go func() {
		second, err := fileLock(path)
		if err == nil {
			close(acquired)
			second()
		}
	}()
	select {
	case <-acquired:
		t.Fatal("second holder acquired a held lock")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("lock not released")
	}
}

func TestSkillCopyRefreshesAndPreservesEdits(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "installed")
	var out bytes.Buffer
	must(t, installSkill(fstest.MapFS{"SKILL.md": {Data: []byte("first")}}, dest, &out))
	must(t, installSkill(fstest.MapFS{"SKILL.md": {Data: []byte("updated")}}, dest, &out))
	if string(readBytes(filepath.Join(dest, "SKILL.md"))) != "updated" {
		t.Fatal("managed copy not refreshed")
	}
	must(t, os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("human edit"), 0o644))
	must(t, installSkill(fstest.MapFS{"SKILL.md": {Data: []byte("newer")}}, dest, &out))
	if string(readBytes(filepath.Join(dest, "SKILL.md"))) != "human edit" {
		t.Fatal("human edit overwritten")
	}
	unmanaged := filepath.Join(t.TempDir(), "unmanaged")
	must(t, os.MkdirAll(unmanaged, 0o755))
	must(t, os.WriteFile(filepath.Join(unmanaged, "SKILL.md"), []byte("existing"), 0o644))
	must(t, installSkill(fstest.MapFS{"SKILL.md": {Data: []byte("tool")}}, unmanaged, &out))
	if string(readBytes(filepath.Join(unmanaged, "SKILL.md"))) != "existing" {
		t.Fatal("unmanaged skill overwritten")
	}
}

func TestCheckboxKeys(t *testing.T) {
	run := func(keys ...int) []int {
		s := checkState{selected: map[int]bool{}, count: 4}
		for _, k := range keys {
			var done bool
			var err error
			if s, done, err = applyKey(s, k); err != nil {
				return []int{-1}
			}
			if done {
				var out []int
				for i := 0; i < 4; i++ {
					if s.selected[i] {
						out = append(out, i)
					}
				}
				return out
			}
		}
		return nil
	}
	eq := func(a, b []int) bool {
		return strings.Trim(strings.Join(strings.Fields(fmtAny(a)), ","), "[]") == strings.Trim(strings.Join(strings.Fields(fmtAny(b)), ","), "[]")
	}
	if got := run(keySpace, keyDown, keyDown, keySpace, keyEnter); !eq(got, []int{0, 2}) {
		t.Fatal(got)
	}
	if got := run(keySpace, keySpace, keyDown, keySpace, keyEnter); !eq(got, []int{1}) {
		t.Fatal(got)
	}
	if got := run(keyUp, keySpace, keyEnter); !eq(got, []int{3}) {
		t.Fatal(got)
	}
	if got := run(keyEnter); got != nil {
		t.Fatal("enter without selection")
	}
	if got := run(keyAbort); !eq(got, []int{-1}) {
		t.Fatal("abort")
	}
}

func TestInventoryDocExampleIsComplete(t *testing.T) {
	doc, err := fs.ReadFile(companywiki.Assets, "skills/wiki-update/references/inventory.md")
	must(t, err)
	_, rest, ok := strings.Cut(string(doc), "```json\n")
	block, _, closed := strings.Cut(rest, "```")
	if !ok || !closed {
		t.Fatal("no json example")
	}
	var data any
	dec := json.NewDecoder(strings.NewReader(block))
	dec.UseNumber()
	must(t, dec.Decode(&data))
	c := &Config{Sources: []Source{{Alias: "documents", Kind: "documents", Enabled: true}, {Alias: "claude-sessions", Kind: "claude", Enabled: true}},
		RequiredSources: map[string]bool{"documents": true, "claude-sessions": true}, RequiredKinds: map[string]bool{"documents": true, "claude": true}}
	if res, err := ValidateInventory(c, data); err != nil || !res.Complete {
		t.Fatal(res, err)
	}
}

func TestParseKeysSplitsChunks(t *testing.T) {
	for _, c := range []struct {
		chunk string
		keys  []int
	}{
		{" ", []int{keySpace}},
		{"\x1b[A", []int{keyUp}},
		{"\x1b[B\x1b[B \r", []int{keyDown, keyDown, keySpace, keyEnter}},
		{"j k\n", []int{keyDown, keySpace, keyUp, keyEnter}},
		{"\r\n", []int{keyEnter}},
		{"x\x1b[C\x1b\xec\x95\x88 q\x03", []int{keySpace, keyAbort, keyAbort}},
		{"\x1b[", nil},
	} {
		keys, ends := parseKeys([]byte(c.chunk))
		if fmtAny(keys) != fmtAny(c.keys) || len(ends) != len(keys) {
			t.Fatalf("%q: %v", c.chunk, keys)
		}
	}
	if _, ends := parseKeys([]byte("\x1b[B \r\ndocs")); fmtAny(ends) != fmtAny([]int{3, 4, 6}) {
		t.Fatal(ends)
	}
}

func TestRunKeysKeepsBytesAfterConfirm(t *testing.T) {
	start := checkState{selected: map[int]bool{}, count: 4}
	s, done, rest, err := runKeys(start, []byte(" \rdocs\r"))
	if err != nil || !done || !s.selected[0] || len(s.selected) != 1 || string(rest) != "docs\r" {
		t.Fatal(s, done, string(rest), err)
	}
	// Enter with nothing selected is ignored; the keys after it still count.
	s, done, rest, _ = runKeys(start, []byte("\r\x1b[B \r\n"))
	if !done || !s.selected[1] || len(rest) != 0 {
		t.Fatal(s, done, rest)
	}
	if s, done, _, _ = runKeys(start, []byte("\r")); done || len(s.selected) != 0 {
		t.Fatal("enter without selection")
	}
	// Split across reads, the state carries over.
	s, done, _, _ = runKeys(start, []byte("j"))
	if s, done, _, _ = runKeys(s, []byte(" \r")); !done || !s.selected[1] {
		t.Fatal(s)
	}
	if _, _, _, err := runKeys(start, []byte(" q\r")); err != errCancelled {
		t.Fatal("q must cancel before enter", err)
	}
}

func TestLineReaderUsesPendingBytesFirst(t *testing.T) {
	var echo bytes.Buffer
	r := newLineReader([]byte("docs\rsess"), strings.NewReader("ions\nlater\n"), &echo)
	var got []string
	for i := 0; i < 3; i++ {
		line, err := r.ReadString('\n')
		must(t, err)
		got = append(got, line)
	}
	if strings.Join(got, "|") != "docs\n|sessions\n|later\n" || echo.String() != "docs\nsess" {
		t.Fatal(got, echo.String())
	}
	if line, _ := newLineReader(nil, strings.NewReader("x\n"), &echo).ReadString('\n'); line != "x\n" {
		t.Fatal(line)
	}
}
