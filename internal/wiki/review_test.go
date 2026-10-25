package wiki

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Human review gates exercised on isolated repositories; never publish fixture decisions.

func page(title, body, extra string) []byte {
	return []byte("---\ntype: concept\ntitle: " + title + "\ndescription: " + title + " summary\nowner: team-a\nsources: [\"^[/raw.md:2]\"]\n" + extra + "---\n" + body + "\n")
}

type fixture struct {
	t                                *testing.T
	repo, wiki, state, raw, sessions string
	cfg                              *Config
	r                                *Review
	response                         map[string]any
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, repo: resolvePath(t.TempDir(), "")}
	f.wiki = filepath.Join(f.repo, "docs", "wiki")
	f.state = filepath.Join(f.repo, ".llm-wiki")
	f.raw = filepath.Join(f.repo, "raw.md")
	f.sessions = filepath.Join(f.repo, "sessions")
	must(t, os.MkdirAll(f.wiki, 0o755))
	must(t, os.MkdirAll(f.sessions, 0o755))
	must(t, os.WriteFile(f.raw, []byte("actual evidence\nsecond line\n"), 0o644))
	f.cfg = &Config{WikiRoot: f.wiki, RepoRoot: f.repo, StateRoot: f.state, Title: "Company Wiki", Reviewer: "human:owner",
		ExtraRoots: map[string]string{"claude-sessions": f.sessions, "codex-sessions": f.sessions}, RequiredSources: map[string]bool{}, RequiredKinds: map[string]bool{"documents": true}}
	r, err := newReview(f.cfg, f.wiki, f.repo, f.state)
	must(t, err)
	f.r = r
	f.record(map[string]any{"type": "user", "message": map[string]any{"content": "검토한 범위로 승인합니다"}})
	f.response = map[string]any{"by": "human:owner", "quote": "검토한 범위로 승인합니다", "evidence": "^[/claude-sessions/user.jsonl:1]"}
	f.put("concepts/a.md", page("a", "old", ""))
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) record(row any) {
	b, _ := json.Marshal(row)
	must(f.t, os.WriteFile(filepath.Join(f.sessions, "user.jsonl"), append(b, '\n'), 0o644))
}

func (f *fixture) put(rel string, data []byte) {
	p := filepath.Join(f.wiki, filepath.FromSlash(rel))
	must(f.t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(f.t, os.WriteFile(p, data, 0o644))
}

func (f *fixture) read(rel string) []byte {
	return readBytes(filepath.Join(f.wiki, filepath.FromSlash(rel)))
}

type files []struct {
	rel  string
	data []byte
}

func one(rel string, data []byte) files {
	return files{{rel, data}}
}

func (f *fixture) prepareErr(topic string, fs files, extra map[string]any) (*PrepareResult, error) {
	if fs == nil {
		fs = one("concepts/a.md", page("a", "new ^[/raw.md:1]", ""))
	}
	draft := filepath.Join(f.repo, "draft-"+topic)
	must(f.t, os.RemoveAll(draft))
	var names []any
	for _, x := range fs {
		p := filepath.Join(draft, filepath.FromSlash(x.rel)+".txt")
		must(f.t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(f.t, os.WriteFile(p, x.data, 0o644))
		names = append(names, x.rel)
	}
	card := map[string]any{"title": topic, "summary": "Review change and its scope", "files": names,
		"judgments": []any{map[string]any{"kind": "interpretation", "before": "old", "proposed": "new", "reason": "evidence", "evidence": []any{"^[/raw.md:1]"}}}}
	for k, v := range extra {
		card[k] = v
	}
	raw, _ := json.Marshal(card)
	decoded, _ := decodeOrdered(raw)
	return f.r.Prepare("batch", topic, decoded, draft)
}

func (f *fixture) prepare(topic string, fs files, extra map[string]any) *PrepareResult {
	f.t.Helper()
	res, err := f.prepareErr(topic, fs, extra)
	must(f.t, err)
	return res
}

func (f *fixture) approve(res *PrepareResult, state string) error {
	raw, _ := json.Marshal(f.response)
	decoded, _ := decodeOrdered(raw)
	_, err := f.r.Decide("batch", res.Topic, res.Revision, state, decoded)
	return err
}

func wantErr(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), part) {
		t.Fatalf("want error containing %q, got %v", part, err)
	}
}

func TestPrepareIsolatedAndExcludedFromReaders(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	if !bytes.Equal(f.read("concepts/a.md"), page("a", "old", "")) {
		t.Fatal("prepare changed the wiki")
	}
	if !strings.Contains(string(readBytes(res.Review)), "-old") {
		t.Fatal("review document lacks the diff")
	}
	f.put("drafts/bad.md", []byte("[[does-not-exist]]"))
	if got := f.cfg.pages(); len(got) != 1 || got[0].Slug != "concepts/a" {
		t.Fatalf("pages %v", got)
	}
	out, err := f.r.Preview("batch", []string{"one"})
	must(t, err)
	if !strings.Contains(out, "1 pages · 0 errors") || strings.Contains(out, "bad.md") {
		t.Fatal(out)
	}
}

func TestAllNonApprovedStatesBlockApply(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	for _, state := range []string{"", "deferred", "rejected", "changes_requested"} {
		if state != "" {
			must(t, f.approve(res, state))
		}
		_, err := f.r.Apply("batch", []string{"one"})
		wantErr(t, err, "사람 승인 없음")
	}
	if !bytes.Equal(f.read("concepts/a.md"), page("a", "old", "")) {
		t.Fatal("unapproved change applied")
	}
}

func snapshotTree(root string) map[string]string {
	out := map[string]string{}
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			out[p] = string(readBytes(p))
		}
		return nil
	})
	return out
}

func TestExactApprovalApplyReceiptAndIdempotence(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	must(t, f.approve(res, "approved"))
	out, err := f.r.Apply("batch", []string{"one"})
	must(t, err)
	if !strings.Contains(out, "반영 완료") {
		t.Fatal(out)
	}
	before := snapshotTree(f.wiki)
	out, err = f.r.Apply("batch", []string{"one"})
	must(t, err)
	if !strings.Contains(out, "이미 반영") || !reflect.DeepEqual(before, snapshotTree(f.wiki)) {
		t.Fatal("second apply wrote again")
	}
	if exists(filepath.Join(f.wiki, "log.md")) || exists(filepath.Join(f.wiki, "reviews")) {
		t.Fatal("records leaked into the wiki")
	}
	var receipt map[string]any
	must(t, json.Unmarshal(readBytes(filepath.Join(f.state, "reviews/batch/applied.jsonl")), &receipt))
	if receipt["decision_evidence"] != f.response["evidence"] {
		t.Fatal(receipt)
	}
	if exists(filepath.Join(f.state, "registry.json")) {
		t.Fatal("registry written to the workspace")
	}
	var reg Registry
	must(t, json.Unmarshal(readBytes(filepath.Join(f.wiki, ".verification/registry.json")), &reg))
	got := sortedStrings(map[string]bool{})
	for k := range reg.Pages["concepts/a"].Citations {
		got = append(got, k)
	}
	if len(got) != 2 || reg.Pages["concepts/a"].Citations["/raw.md:1-1"] == "" || reg.Pages["concepts/a"].Citations["/raw.md:2-2"] == "" {
		t.Fatal(got)
	}
	status, err := f.r.StatusAll()
	must(t, err)
	if status["batch"][0].State != "applied" {
		t.Fatal(status)
	}
}

func TestOnlySelectedApprovedTopicIsApplied(t *testing.T) {
	f := newFixture(t)
	a := f.prepare("one", nil, nil)
	b := f.prepare("two", one("concepts/b.md", page("b", "text", "")), nil)
	must(t, f.approve(a, "approved"))
	must(t, f.approve(b, "approved"))
	_, err := f.r.Apply("batch", []string{"two"})
	must(t, err)
	if !bytes.Equal(f.read("concepts/a.md"), page("a", "old", "")) || !exists(filepath.Join(f.wiki, "concepts/b.md")) {
		t.Fatal("wrong topic applied")
	}
	_, err = f.r.Apply("batch", []string{"one"})
	must(t, err)
}

func TestModelAndToolCannotSupplyApproval(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	q := f.response["quote"]
	for _, row := range []any{
		map[string]any{"type": "assistant", "message": map[string]any{"content": q}},
		map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "content": q}}}},
		map[string]any{"type": "attachment", "attachment": map[string]any{"type": "queued_command", "prompt": q, "origin": map[string]any{"kind": "agent"}}},
	} {
		f.record(row)
		wantErr(t, f.approve(res, "approved"), "사용자 메시지에 없음")
	}
}

func TestClaudeQueuedAndCodexMessagesSupported(t *testing.T) {
	f := newFixture(t)
	q := f.response["quote"]
	for _, row := range []any{
		map[string]any{"type": "attachment", "attachment": map[string]any{"type": "queued_command", "prompt": q, "origin": map[string]any{"kind": "human"}}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": q}}}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "user_message", "message": q}},
	} {
		f.record(row)
		if _, err := f.r.userEvidence(f.response); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionAppendDoesNotInvalidate(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, map[string]any{"dependencies": []any{f.response["evidence"]}})
	must(t, f.approve(res, "approved"))
	fh, _ := os.OpenFile(filepath.Join(f.sessions, "user.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	fh.WriteString(`{"type": "metadata"}` + "\n")
	fh.Close()
	_, err := f.r.Apply("batch", []string{"one"})
	must(t, err)
}

func TestUserEvidenceMutationBlocksApply(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	must(t, f.approve(res, "approved"))
	f.record(map[string]any{"type": "user", "message": map[string]any{"content": f.response["quote"].(string) + " but changed"}})
	_, err := f.r.Apply("batch", []string{"one"})
	wantErr(t, err, "승인 원문 변경")
}

func TestNewRevisionInvalidatesOnlyThatTopic(t *testing.T) {
	f := newFixture(t)
	a := f.prepare("one", nil, nil)
	b := f.prepare("two", one("concepts/b.md", page("b", "text", "")), nil)
	must(t, f.approve(a, "approved"))
	must(t, f.approve(b, "approved"))
	newer := f.prepare("one", one("concepts/a.md", page("a", "revised", "")), nil)
	if newer.Revision == a.Revision {
		t.Fatal("same revision")
	}
	wantErr(t, f.approve(a, "approved"), "이전 검토 버전")
	_, err := f.r.Apply("batch", []string{"one"})
	wantErr(t, err, "사람 승인 없음")
	_, err = f.r.Apply("batch", []string{"two"})
	must(t, err)
}

func TestTamperingAfterCardOrDisplayFails(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	must(t, f.approve(res, "approved"))
	folder := filepath.Dir(res.Review)
	for _, p := range []string{"after/concepts/a.md.txt", "before/concepts/a.md.txt", "review.md", "card.json"} {
		path := filepath.Join(folder, p)
		old := readBytes(path)
		if filepath.Base(p) == "card.json" {
			var card map[string]any
			must(t, json.Unmarshal(old, &card))
			card["summary"] = "different"
			b, _ := json.Marshal(card)
			must(t, os.WriteFile(path, b, 0o644))
		} else {
			must(t, os.WriteFile(path, append(append([]byte{}, old...), []byte("changed")...), 0o644))
		}
		if _, err := f.r.Apply("batch", []string{"one"}); err == nil {
			t.Fatalf("tampered %s accepted", p)
		}
		must(t, os.WriteFile(path, old, 0o644))
	}
}

func TestStaleTargetAndBasisBlockApply(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	must(t, f.approve(res, "approved"))
	for _, path := range []string{f.raw, filepath.Join(f.wiki, "concepts/a.md")} {
		old := readBytes(path)
		changed := []byte("changed\n")
		if path != f.raw {
			changed = bytes.ReplaceAll(old, []byte("old"), []byte("changed"))
		}
		must(t, os.WriteFile(path, changed, 0o644))
		_, err := f.r.Apply("batch", []string{"one"})
		wantErr(t, err, "기준 변경")
		must(t, os.WriteFile(path, old, 0o644))
	}
	_, err := f.r.Apply("batch", []string{"one"})
	must(t, err)
}

func TestLinkedContextIsADependency(t *testing.T) {
	f := newFixture(t)
	f.put("concepts/context.md", page("context", "text", ""))
	res := f.prepare("one", one("concepts/a.md", page("a", "[[context]]", "")), nil)
	must(t, f.approve(res, "approved"))
	f.put("concepts/context.md", page("context", "changed", ""))
	_, err := f.r.Apply("batch", []string{"one"})
	wantErr(t, err, "기준 변경")
}

func TestSharedPageTopicsRejected(t *testing.T) {
	f := newFixture(t)
	f.prepare("one", nil, nil)
	_, err := f.prepareErr("two", nil, nil)
	wantErr(t, err, "한 승인 묶음")
}

func TestPartialLinkDependencyFailsAndCombinedSucceeds(t *testing.T) {
	f := newFixture(t)
	a := f.prepare("one", one("concepts/a.md", page("a", "[[b]]", "")), nil)
	b := f.prepare("two", one("concepts/b.md", page("b", "[[a]]", "")), nil)
	must(t, f.approve(a, "approved"))
	must(t, f.approve(b, "approved"))
	_, err := f.r.Apply("batch", []string{"one"})
	wantErr(t, err, "없는 페이지")
	_, err = f.r.Apply("batch", []string{"one", "two"})
	must(t, err)
}

func TestScopeChecksAliasesInBothDirections(t *testing.T) {
	f := newFixture(t)
	f.put("concepts/other.md", page("other", "[[wiki/concepts/a|alias]]", ""))
	res := f.prepare("one", nil, map[string]any{"link_constraints": []any{map[string]any{"target": "concepts/a", "inbound_from": []any{}}}})
	must(t, f.approve(res, "approved"))
	_, err := f.r.Apply("batch", []string{"one"})
	wantErr(t, err, "승인 범위 밖 연결")
	card, _ := decodeOrdered([]byte(`{"link_constraints":[{"target":"concepts/other","outbound_to":[]}]}`))
	wantErr(t, f.r.checkConstraints([]any{card}, f.r.pages()), "승인 범위 밖 연결")
}

func TestRegistryFlagsChangedSourceUntilRerecorded(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	must(t, f.approve(res, "approved"))
	_, err := f.r.Apply("batch", []string{"one"})
	must(t, err)
	text := string(f.read("concepts/a.md"))
	reg, err := f.cfg.loadRegistry()
	must(t, err)
	for _, ks := range f.cfg.freshness("concepts/a", text, reg) {
		if ks.state != "fresh" {
			t.Fatal(ks)
		}
	}
	must(t, os.WriteFile(f.raw, []byte("changed evidence\nsecond line\n"), 0o644))
	got := map[string]string{}
	for _, ks := range f.cfg.freshness("concepts/a", text, reg) {
		got[ks.key] = ks.state
	}
	if got["/raw.md:1-1"] != "changed" || got["/raw.md:2-2"] != "fresh" {
		t.Fatal(got)
	}
}

func TestInsertedLineMarksCitationMovedNotChanged(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	must(t, f.approve(res, "approved"))
	_, err := f.r.Apply("batch", []string{"one"})
	must(t, err)
	text := string(f.read("concepts/a.md"))
	reg, _ := f.cfg.loadRegistry()
	must(t, os.WriteFile(f.raw, []byte("inserted heading\nactual evidence\nsecond line\n"), 0o644))
	got := map[string]string{}
	for _, ks := range f.cfg.freshness("concepts/a", text, reg) {
		got[ks.key] = ks.state
	}
	if !reflect.DeepEqual(got, map[string]string{"/raw.md:1-1": "moved", "/raw.md:2-2": "moved"}) {
		t.Fatal(got)
	}
	if to := f.cfg.movedTo("/raw.md:1-1", "concepts/a", reg); to != "/raw.md:2-2" {
		t.Fatal(to)
	}
	must(t, os.WriteFile(f.raw, []byte("inserted heading\nrewritten evidence\nsecond line\n"), 0o644))
	for _, ks := range f.cfg.freshness("concepts/a", text, reg) {
		if ks.key == "/raw.md:1-1" && ks.state != "changed" {
			t.Fatal(ks)
		}
	}
}

func TestLegacyWorkspaceRegistryIsReadUntilRewritten(t *testing.T) {
	f := newFixture(t)
	must(t, os.MkdirAll(f.state, 0o755))
	must(t, os.WriteFile(filepath.Join(f.state, "registry.json"), []byte(`{"version":1,"pages":{"concepts/a":{"citations":{}}}}`), 0o644))
	reg, err := f.cfg.loadRegistry()
	must(t, err)
	if _, ok := reg.Pages["concepts/a"]; !ok {
		t.Fatal("legacy registry not read")
	}
	must(t, os.MkdirAll(filepath.Join(f.wiki, ".verification"), 0o755))
	must(t, os.WriteFile(registryPath(f.wiki), []byte(`{"version":1,"pages":{}}`), 0o644))
	reg, _ = f.cfg.loadRegistry()
	if len(reg.Pages) != 0 {
		t.Fatal("wiki registry not preferred")
	}
}

func TestDependentsFollowImpactRelationsTransitively(t *testing.T) {
	rel := func(target, kind string) any { return map[string]any{"target": target, "type": kind} }
	pages := map[string]Frontmatter{
		"concepts/growth":       {},
		"policies/training":     {"related": []any{rel("concepts/growth", "depends_on")}},
		"entities/products/app": {"related": []any{rel("policies/training", "implements")}},
		"concepts/other":        {"related": []any{"concepts/growth", rel("concepts/growth", "related_to")}},
	}
	got := dependents(pages, map[string]bool{"concepts/growth": true})
	want := map[string]Impact{"policies/training": {"depends_on", "concepts/growth"}, "entities/products/app": {"implements", "policies/training"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestExpiredSessionIsDistinctFromMissingDocument(t *testing.T) {
	f := newFixture(t)
	got := map[string]string{}
	for _, ks := range f.cfg.freshness("concepts/a", "a ^[/claude-sessions/gone.jsonl:1] b ^[/gone.md:1]", &Registry{Pages: map[string]*PageRecord{}}) {
		got[ks.key] = ks.state
	}
	if !reflect.DeepEqual(got, map[string]string{"/claude-sessions/gone.jsonl:1-1": "expired", "/gone.md:1-1": "missing"}) {
		t.Fatal(got)
	}
}

func asPolicy(b []byte) []byte {
	return bytes.Replace(b, []byte("type: concept"), []byte("type: policy"), 1)
}

func TestNestedPagesAndTypedRelated(t *testing.T) {
	f := newFixture(t)
	f.put("entities/products/plco.md", bytes.Replace(page("plco", "text", ""), []byte("type: concept"), []byte("type: entity"), 1))
	rel := "related:\n  - entities/products/plco\n  - target: entities/products/plco\n    type: applies_to\n    evidence: ^[/raw.md:1]\n"
	f.prepare("one", one("policies/product/training.md", asPolicy(page("training", "rule ^[/raw.md:1]", rel))), nil)
	out, err := f.r.Preview("batch", []string{"one"})
	must(t, err)
	if !strings.Contains(out, "0 errors") {
		t.Fatal(out)
	}
	bad := asPolicy(page("training", "rule", strings.Replace(rel, "applies_to", "causes", 1)))
	f.prepare("two", one("policies/product/other.md", bad), nil)
	_, err = f.r.Preview("batch", []string{"two"})
	wantErr(t, err, "알 수 없는 관계")
}

func TestOwnerAndSourcesRequired(t *testing.T) {
	f := newFixture(t)
	_, err := f.prepareErr("one", one("concepts/a.md", bytes.Replace(page("a", "new", ""), []byte("owner: team-a\n"), nil, 1)), nil)
	wantErr(t, err, "owner")
	f.prepare("one", one("concepts/a.md", bytes.Replace(page("a", "new", ""), []byte("sources: [\"^[/raw.md:2]\"]\n"), nil, 1)), nil)
	_, err = f.r.Preview("batch", []string{"one"})
	wantErr(t, err, "sources 없음")
}

func TestPathsAndSymlinksRejected(t *testing.T) {
	f := newFixture(t)
	_, err := f.prepareErr("one", one("../escape.md", page("bad", "text", "")), nil)
	wantErr(t, err, "페이지 상대경로")
	must(t, os.Remove(filepath.Join(f.wiki, "concepts/a.md")))
	if err := os.Symlink(f.raw, filepath.Join(f.wiki, "concepts/a.md")); err != nil {
		t.Skip("symlinks not permitted")
	}
	_, err = f.prepareErr("one", nil, nil)
	wantErr(t, err, "심링크")
}

func TestMissingCitationAndReviewLinkFail(t *testing.T) {
	f := newFixture(t)
	_, err := f.prepareErr("one", one("concepts/a.md", page("a", "^[/raw.md:999]", "")), nil)
	wantErr(t, err, "근거 범위 오류")
	f.prepare("one", one("concepts/a.md", page("a", "[[reviews/bad]]", "")), nil)
	_, err = f.r.Preview("batch", []string{"one"})
	wantErr(t, err, "없는 페이지")
}

func TestOverlayResourcesUseProposedVersion(t *testing.T) {
	f := newFixture(t)
	f.prepare("one", one("concepts/a.md", page("a", "short", "")), nil)
	f.put("concepts/c.md", page("c", "^[/docs/wiki/concepts/a.md:50]", ""))
	// The real page has enough lines; the candidate does not.
	f.put("concepts/a.md", page("a", strings.Repeat("old\n", 60), ""))
	f.prepare("one", one("concepts/a.md", page("a", "short", "")), nil)
	_, err := f.r.Preview("batch", []string{"one"})
	wantErr(t, err, "인용 줄 범위 오류")
}

func TestReviewCitationsAreNotAuthoritative(t *testing.T) {
	f := newFixture(t)
	must(t, os.MkdirAll(filepath.Join(f.state, "reviews"), 0o755))
	must(t, os.WriteFile(filepath.Join(f.state, "reviews/proposal.md"), []byte("speculative claim\n"), 0o644))
	f.prepare("one", one("concepts/a.md", page("a", "^[/.llm-wiki/reviews/proposal.md:1]", "")), nil)
	_, err := f.r.Preview("batch", []string{"one"})
	wantErr(t, err, "검토 사본")
}

func TestStateInsideWikiRejected(t *testing.T) {
	f := newFixture(t)
	_, err := newReview(f.cfg, f.wiki, f.repo, filepath.Join(f.wiki, "state"))
	wantErr(t, err, "분리")
}

func TestJointNewPageCitationUsesSealedDraft(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", files{
		{"concepts/a.md", page("a", "^[/docs/wiki/concepts/new.md:7]", "")},
		{"concepts/new.md", page("new", "[[a]]", "")},
	}, nil)
	must(t, f.approve(res, "approved"))
	_, err := f.r.Apply("batch", []string{"one"})
	must(t, err)
}

func TestMisspelledScopeConstraintFailsClosed(t *testing.T) {
	f := newFixture(t)
	f.prepare("one", nil, map[string]any{"link_constraints": []any{map[string]any{"target": "concepts/a", "inbound_form": []any{}}}})
	_, err := f.r.Preview("batch", []string{"one"})
	wantErr(t, err, "link_constraints")
}

func TestPartialWriteFailureRollsBack(t *testing.T) {
	f := newFixture(t)
	res := f.prepare("one", nil, nil)
	must(t, f.approve(res, "approved"))
	failed := false
	f.r.write = func(path string, data []byte) error {
		if path == filepath.Join(f.wiki, "index.md") && !failed {
			failed = true
			return errors.New("simulated disk error")
		}
		return atomicWrite(path, data)
	}
	_, err := f.r.Apply("batch", []string{"one"})
	wantErr(t, err, "simulated")
	if !bytes.Equal(f.read("concepts/a.md"), page("a", "old", "")) || exists(filepath.Join(f.wiki, "index.md")) || exists(f.r.journal) {
		t.Fatal("not rolled back")
	}
	_, err = f.r.Apply("batch", []string{"one"})
	must(t, err)
}

func TestRecoveryRefusesOverwritingUnrelatedEdit(t *testing.T) {
	f := newFixture(t)
	must(t, os.MkdirAll(f.r.root, 0o755))
	must(t, os.WriteFile(f.r.journal, []byte(`{"wiki/concepts/a.md":{"before":"old","after":"new"}}`), 0o644))
	f.put("concepts/a.md", []byte("third party"))
	_, err := f.r.Recover()
	wantErr(t, err, "다른 변경")
	f.put("concepts/a.md", []byte("new"))
	_, err = f.r.Recover()
	must(t, err)
	if string(f.read("concepts/a.md")) != "old" {
		t.Fatal("not restored")
	}
}
