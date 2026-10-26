package wiki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	companywiki "github.com/boaz-hwang/company-wiki"
)

// Public workspace integration: synthetic sources and decisions only.

type ws struct {
	t                  *testing.T
	home, root, config string
}

// newWS gives each test its own home folder, so setup's fixed root is <home>/company-wiki.
func newWS(t *testing.T) *ws {
	home := filepath.Join(resolvePath(t.TempDir(), ""), "한글 홈")
	must(t, os.MkdirAll(home, 0o755))
	setHome(t, home)
	w := &ws{t: t, home: home, root: filepath.Join(home, "company-wiki")}
	for _, d := range []string{"raw", "sessions"} {
		must(t, os.MkdirAll(filepath.Join(w.root, d), 0o755))
	}
	must(t, os.WriteFile(filepath.Join(w.root, "raw/policy.md"), []byte("Return within 14 days.\n"), 0o644))
	w.config = filepath.Join(w.root, "wiki.toml")
	w.writeConfig("\ufeff[wiki]\ntitle=\"Team Wiki 한글\"\nreviewer=\"human:alice\"\n[sources.notes]\nkind=\"documents\"\npath=\"raw\"\n[sources.claude-sessions]\nkind=\"claude\"\npath=\"sessions\"\n")
	return w
}

func setHome(t *testing.T, home string) {
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", "")
}

func (w *ws) writeConfig(s string) { must(w.t, os.WriteFile(w.config, []byte(s), 0o644)) }

type result struct {
	code           int
	stdout, stderr string
}

func (w *ws) run(args ...string) result {
	var out, errOut bytes.Buffer
	env := func(string) string { return "" }
	code := Main(args, IO{Env: env, Dir: w.root, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
	return result{code, out.String(), errOut.String()}
}

func (w *ws) ok(args ...string) result {
	w.t.Helper()
	r := w.run(args...)
	if r.code != 0 {
		w.t.Fatalf("%v exited %d\n%s%s", args, r.code, r.stdout, r.stderr)
	}
	return r
}

func (w *ws) prepare() string {
	w.ok("setup")
	draft := filepath.Join(w.root, "draft")
	must(w.t, os.MkdirAll(filepath.Join(draft, "policies"), 0o755))
	body := "---\ntype: policy\ntitle: Returns\ndescription: Return window\nowner: support-lead\nsources:\n  - ^[/notes/policy.md:1]\n---\nReturn within 14 days. ^[/notes/policy.md:1]\n"
	must(w.t, os.WriteFile(filepath.Join(draft, "policies/returns.md.txt"), []byte(strings.ReplaceAll(body, "\n", "\r\n")), 0o644))
	card := `{"title":"Return policy","summary":"Initial policy","files":["policies/returns.md"],"judgments":[{"kind":"fact","before":"No wiki","proposed":"14 days","reason":"Policy document","evidence":["^[/notes/policy.md:1]"]}]}`
	must(w.t, os.WriteFile(filepath.Join(w.root, "card.json"), []byte(card), 0o644))
	out := w.ok("review", "prepare", "first", "returns", "--card", filepath.Join(w.root, "card.json"), "--draft-dir", draft)
	var res PrepareResult
	must(w.t, json.Unmarshal([]byte(out.stdout), &res))
	return res.Revision
}

func (w *ws) approve(revision, role, by string) result {
	row, _ := json.Marshal(map[string]any{"type": role, "message": map[string]any{"content": "Approve the reviewed change."}})
	must(w.t, os.WriteFile(filepath.Join(w.root, "sessions/decision.jsonl"), append(row, '\n'), 0o644))
	resp, _ := json.Marshal(map[string]any{"by": by, "quote": "Approve the reviewed change.", "evidence": "^[/claude-sessions/decision.jsonl:1]"})
	must(w.t, os.WriteFile(filepath.Join(w.root, "response.json"), resp, 0o644))
	return w.run("review", "decide", "first", "returns", "--revision", revision, "--decision", "approved", "--response", filepath.Join(w.root, "response.json"))
}

func (w *ws) read(rel string) string {
	return string(readBytes(filepath.Join(w.root, filepath.FromSlash(rel))))
}

func TestSetupIdempotentPreservesHumanFilesAndSkills(t *testing.T) {
	w := newWS(t)
	must(t, os.WriteFile(filepath.Join(w.root, "AGENTS.md"), []byte("Existing project instructions\n"), 0o644))
	w.ok("setup")
	must(t, os.WriteFile(filepath.Join(w.root, "llm-wiki/SCHEMA.md"), []byte("My edited rules\n"), 0o644))
	config := w.read("wiki.toml")
	w.ok("setup")
	if w.read("llm-wiki/SCHEMA.md") != "My edited rules\n" || w.read("wiki.toml") != config {
		t.Fatal("setup overwrote user files")
	}
	agents := w.read("AGENTS.md")
	if !strings.HasPrefix(agents, "Existing project instructions") || strings.Count(agents, "<!-- company-wiki -->") != 1 {
		t.Fatal(agents)
	}
	if exists(filepath.Join(w.root, "CLAUDE.md")) {
		t.Fatal("setup wrote CLAUDE.md")
	}
	for _, folder := range []string{"concepts", "entities", "policies"} {
		if !isDir(filepath.Join(w.root, "llm-wiki", folder)) {
			t.Fatal(folder)
		}
	}
	if !isDir(filepath.Join(w.root, "workspace/wiki-changes")) || exists(filepath.Join(w.root, "llm-wiki/reviews")) {
		t.Fatal("workspace layout")
	}
	want, _ := fs.ReadFile(companywiki.Assets, "skills/wiki-review/SKILL.md")
	for _, provider := range []string{".claude", ".agents"} {
		entries, _ := os.ReadDir(filepath.Join(w.root, provider, "skills"))
		if len(entries) != 4 || w.read(provider+"/skills/wiki-review/SKILL.md") != string(want) {
			t.Fatal(provider)
		}
	}
	if !strings.Contains(w.read("llm-wiki/index.md"), "Team Wiki") {
		t.Fatal("index title")
	}
}

func TestSetupRefusesToolRepositoryCheckout(t *testing.T) {
	for _, marker := range []struct{ rel, data string }{
		{"go.mod", "module github.com/boaz-hwang/company-wiki\n\ngo 1.27\n"},
		{"cmd/company-wiki/main.go", "package main\n"},
	} {
		home := resolvePath(t.TempDir(), "")
		setHome(t, home)
		root := filepath.Join(home, "company-wiki")
		p := filepath.Join(root, filepath.FromSlash(marker.rel))
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte(marker.data), 0o644))
		must(t, os.MkdirAll(filepath.Join(home, "docs"), 0o755))
		vault := filepath.Join(home, "vault")
		must(t, os.MkdirAll(vault, 0o755))
		before := snapshotTree(home)
		var out, errOut bytes.Buffer
		code := Main([]string{"setup", "--source", "documents=" + filepath.Join(home, "docs"), "--vault", vault},
			IO{Env: func(string) string { return "" }, Dir: home, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
		if code != 1 || !strings.Contains(errOut.String(), "도구 저장소의 checkout") || out.Len() != 0 {
			t.Fatal(marker.rel, code, out.String(), errOut.String())
		}
		if after := snapshotTree(home); len(after) != len(before) || exists(filepath.Join(root, "wiki.toml")) || exists(filepath.Join(root, "workspace")) ||
			exists(filepath.Join(root, "llm-wiki")) || exists(filepath.Join(vault, "wiki-changes")) {
			t.Fatal("setup wrote into the tool checkout", marker.rel)
		}
	}
}

func TestSetupRefreshesPreviousToolkitPointer(t *testing.T) {
	w := newWS(t)
	w.ok("setup")
	agents := filepath.Join(w.root, "AGENTS.md")
	old := strings.ReplaceAll(w.read("AGENTS.md"), "company-wiki -->", "boaz-wiki -->") + "Toolkit and skills: /old/toolkit/path\n"
	old = strings.Replace(old, "<!-- /boaz-wiki -->", "Toolkit and skills: /old/toolkit/path\n<!-- /boaz-wiki -->", 1)
	must(t, os.WriteFile(agents, []byte(old), 0o644))
	w.ok("setup")
	updated := w.read("AGENTS.md")
	if strings.Count(updated, "<!-- company-wiki -->") != 1 || strings.Count(updated, "/old/toolkit/path") != 1 || !strings.Contains(updated, "company-wiki CLI") {
		t.Fatal(updated)
	}
}

func TestConfiguredAliasReviewerAndFullPublicWorkflow(t *testing.T) {
	w := newWS(t)
	revision := w.prepare()
	if out := w.ok("review", "preview", "first", "returns"); !strings.Contains(out.stdout, "0 errors") {
		t.Fatal(out.stdout)
	}
	if w.run("review", "apply", "first", "returns").code == 0 {
		t.Fatal("applied without approval")
	}
	if r := w.approve(revision, "user", "human:alice"); r.code != 0 {
		t.Fatal(r.stderr)
	}
	applied := w.ok("review", "apply", "first", "returns").stdout
	message := "커밋 메시지:\nReturn policy\n\nWiki-Change: first/returns\nWiki-Revision: " + revision +
		"\nApproved-By: human:alice\nApproval-Evidence: ^[/claude-sessions/decision.jsonl:1]\n"
	if !strings.HasSuffix(applied, message) {
		t.Fatal(applied)
	}
	receipt := w.read("workspace/wiki-changes/first/applied.jsonl")
	if again := w.ok("review", "apply", "first", "returns").stdout; !strings.HasSuffix(again, "\n\n"+message) || !strings.HasPrefix(again, "이미 반영됨") {
		t.Fatal(again)
	}
	if w.read("workspace/wiki-changes/first/applied.jsonl") != receipt {
		t.Fatal("second apply added a receipt")
	}
	if !exists(filepath.Join(w.root, "llm-wiki/policies/returns.md")) || !strings.Contains(w.read("llm-wiki/index.md"), "[[policies/returns") {
		t.Fatal("page not applied")
	}
	if !isFile(filepath.Join(w.root, "llm-wiki/.verification/registry.json")) || exists(filepath.Join(w.root, "workspace/wiki-changes/registry.json")) {
		t.Fatal("registry location")
	}
	if !strings.Contains(w.ok("registry", "--all").stdout, "fresh") {
		t.Fatal("not fresh")
	}
	must(t, os.WriteFile(filepath.Join(w.root, "raw/policy.md"), []byte("Return within 30 days.\n"), 0o644))
	if !strings.Contains(w.run("lint").stdout, "원문 변경") || !strings.Contains(w.ok("registry", "--source", "/notes/policy.md").stdout, "changed") {
		t.Fatal("change not detected")
	}
	w.ok("registry", "record")
	if !strings.Contains(w.run("lint").stdout, "원문 변경") {
		t.Fatal("plain record hid the change")
	}
	w.ok("registry", "record", "policies/returns")
	if strings.Contains(w.run("lint").stdout, "원문 변경") {
		t.Fatal("re-record did not clear")
	}
	if page := w.ok("registry", "--page", "policies/returns").stdout; !strings.Contains(page, "기대는 페이지 없음") {
		t.Fatal(page)
	}
}

func TestWrongReviewerAndAssistantEvidenceRejected(t *testing.T) {
	w := newWS(t)
	rev := w.prepare()
	if w.approve(rev, "user", "human:bob").code == 0 || w.approve(rev, "assistant", "human:alice").code == 0 {
		t.Fatal("bad approval accepted")
	}
}

// The owner's answer may sit in any registered claude/codex source, not only claude-sessions.
func TestDecideAcceptsAnyRegisteredSessionSource(t *testing.T) {
	w := newWS(t)
	for _, d := range []string{"other-sessions", "mine", "codex-dir"} {
		must(t, os.MkdirAll(filepath.Join(w.root, d), 0o755))
	}
	w.ok("setup", "--source", "claude=other-sessions", "--source", "claude:mine=mine", "--source", "codex:cx=codex-dir")
	toml := w.read("wiki.toml")
	if !strings.Contains(toml, "[sources.claude-other-sessions]") || !strings.Contains(toml, "[sources.mine]") {
		t.Fatal(toml)
	}
	revision := w.prepare()
	decide := func(alias, folder string) result {
		row, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"content": "Approve the reviewed change."}})
		must(t, os.WriteFile(filepath.Join(w.root, folder, "answer.jsonl"), append(row, '\n'), 0o644))
		resp, _ := json.Marshal(map[string]any{"by": "human:alice", "quote": "Approve the reviewed change.", "evidence": "^[/" + alias + "/answer.jsonl:1]"})
		must(t, os.WriteFile(filepath.Join(w.root, "response.json"), resp, 0o644))
		return w.run("review", "decide", "first", "returns", "--revision", revision, "--decision", "deferred", "--response", filepath.Join(w.root, "response.json"))
	}
	for _, c := range []struct{ alias, folder string }{{"claude-other-sessions", "other-sessions"}, {"mine", "mine"}, {"cx", "codex-dir"}} {
		if r := decide(c.alias, c.folder); r.code != 0 {
			t.Fatal(c.alias, r)
		}
	}
	// A documents source is named, not silently treated as a missing session.
	if r := decide("notes", "raw"); r.code != 1 || !strings.Contains(r.stderr, "`notes`는 documents 원천이라 세션이 아니다") {
		t.Fatal(r)
	}
	if r := decide("nowhere", "raw"); r.code != 1 || !strings.Contains(r.stderr, "`nowhere`는 등록된 claude·codex 세션 원천이 아님") {
		t.Fatal(r)
	}
	w.writeConfig(strings.Replace(w.read("wiki.toml"), "[sources.mine]\n", "[sources.mine]\nenabled = false\n", 1))
	if r := decide("mine", "mine"); r.code != 1 || !strings.Contains(r.stderr, "`mine`가 비활성") {
		t.Fatal(r)
	}
}

func TestHumanEditAfterPrepareIsPreserved(t *testing.T) {
	w := newWS(t)
	rev := w.prepare()
	if r := w.approve(rev, "user", "human:alice"); r.code != 0 {
		t.Fatal(r.stderr)
	}
	p := filepath.Join(w.root, "llm-wiki/policies/returns.md")
	must(t, os.WriteFile(p, []byte("Human edited this in Obsidian.\n"), 0o644))
	if w.run("review", "apply", "first", "returns").code == 0 || w.read("llm-wiki/policies/returns.md") != "Human edited this in Obsidian.\n" {
		t.Fatal("human edit overwritten")
	}
}

func TestRegisteredSourcesAllRequiredEvenSameKind(t *testing.T) {
	w := newWS(t)
	w.writeConfig(w.read("wiki.toml") + "\n[sources.second]\nkind=\"documents\"\npath=\"other\"\n")
	record := map[string]any{"version": 1, "period": map[string]any{"from": "2026-01-01T00:00:00Z", "through": "2026-02-01T00:00:00Z"},
		"basis": map[string]any{"last_apply": "none", "last_full_review": "unknown; first run"}, "sources": []any{
			map[string]any{"id": "notes", "kind": "documents", "location": "notes", "status": "scanned", "reason": "read and compared", "items": []any{}},
			map[string]any{"id": "claude-sessions", "kind": "claude", "location": "claude-sessions", "status": "scanned", "reason": "read and compared", "items": []any{}},
		}}
	p := filepath.Join(w.root, "inventory.json")
	b, _ := json.Marshal(record)
	must(t, os.WriteFile(p, b, 0o644))
	r := w.run("inventory", p, "--require-complete")
	if r.code != 1 || !strings.Contains(r.stdout, "second") {
		t.Fatal(r)
	}
	record["sources"] = append(record["sources"].([]any), map[string]any{"id": "second", "kind": "documents", "location": "other", "status": "unavailable", "reason": "missing folder", "items": []any{}})
	b, _ = json.Marshal(record)
	must(t, os.WriteFile(p, b, 0o644))
	if r := w.run("inventory", p, "--require-complete"); r.code != 2 {
		t.Fatal(r)
	}
}

func TestNewWorkspaceRegistersSelectedSources(t *testing.T) {
	w := newWS(t)
	must(t, os.Remove(w.config))
	for _, d := range []string{"exports/slack", "repo-a", "repo-b"} {
		must(t, os.MkdirAll(filepath.Join(w.root, d), 0o755))
	}
	refused := w.run("setup")
	if refused.code != 2 || !strings.Contains(refused.stderr, "--source") || exists(w.config) {
		t.Fatal(refused)
	}
	w.ok("setup", "--source", "documents=raw", "--source", "slack=exports/slack", "--source", "code=repo-a", "--source", "code=repo-b")
	data, err := parseTOML(w.read("wiki.toml"))
	must(t, err)
	kinds := map[string]string{}
	for alias, v := range data["sources"].(map[string]any) {
		kinds[alias] = v.(map[string]any)["kind"].(string)
	}
	want := map[string]string{"documents": "documents", "slack": "slack", "code": "code", "code-repo-b": "code"}
	if len(kinds) != len(want) {
		t.Fatal(kinds)
	}
	for k, v := range want {
		if kinds[k] != v {
			t.Fatal(kinds)
		}
	}
	w.ok("setup", "--source", "claude=sessions")
	if !strings.Contains(w.read("wiki.toml"), "[sources.claude-sessions]") {
		t.Fatal("source not appended")
	}
	if !strings.Contains(w.ok("lint").stdout, "0 errors") {
		t.Fatal("lint")
	}
}

func TestSetupRejectsMissingPathAndUnknownKind(t *testing.T) {
	w := newWS(t)
	for _, spec := range []string{"slack=nowhere", "clippings=raw", "documents"} {
		if r := w.run("setup", "--source", spec); r.code != 1 {
			t.Fatal(spec, r)
		}
	}
	if strings.Contains(w.read("wiki.toml"), "nowhere") {
		t.Fatal("missing path written")
	}
}

func TestLintWarnsWhenRegisteredSourceIsGone(t *testing.T) {
	w := newWS(t)
	w.ok("setup")
	must(t, os.RemoveAll(filepath.Join(w.root, "raw")))
	if !strings.Contains(w.run("lint").stdout, "원천 `notes` 경로 없음") {
		t.Fatal("no warning")
	}
}

func TestPersonalClippingsAreNotASourceKind(t *testing.T) {
	w := newWS(t)
	w.writeConfig("[wiki]\n[sources.Clippings]\nkind=\"clippings\"\npath=\"raw\"\n")
	if r := w.run("lint"); r.code == 0 || !strings.Contains(r.stderr, "Unsupported source kind") {
		t.Fatal(r)
	}
}

func TestMissingExplicitConfigurationFails(t *testing.T) {
	w := newWS(t)
	must(t, os.Remove(w.config))
	if w.run("lint").code == 0 {
		t.Fatal("lint ran without config")
	}
}

func TestOldWikiPathAndStateSettingsRejected(t *testing.T) {
	w := newWS(t)
	for _, key := range []string{"path = \"knowledge\"", "state = \".llm-wiki\""} {
		w.writeConfig("[wiki]\n" + key + "\n")
		for _, cmd := range []string{"lint", "setup"} {
			if r := w.run(cmd); r.code == 0 || !strings.Contains(r.stderr, "no longer supported; the layout is fixed") ||
				!strings.Contains(r.stderr, filepath.Join("workspace", "wiki-changes")) {
				t.Fatal(key, cmd, r)
			}
		}
	}
	if exists(filepath.Join(w.root, "knowledge")) || exists(filepath.Join(w.root, "llm-wiki")) {
		t.Fatal("setup wrote folders for a rejected config")
	}
}

func TestConfigFallsBackToFixedRoot(t *testing.T) {
	home := resolvePath(t.TempDir(), "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := filepath.Join(home, "company-wiki")
	must(t, os.MkdirAll(root, 0o755))
	must(t, os.WriteFile(filepath.Join(root, "wiki.toml"), []byte("[wiki]\ntitle = \"Home\"\n"), 0o644))
	c, err := LoadConfig(func(string) string { return "" }, t.TempDir())
	must(t, err)
	if c.Path != filepath.Join(root, "wiki.toml") || c.WikiRoot != filepath.Join(root, "llm-wiki") || c.ChangesRoot != filepath.Join(root, "workspace", "wiki-changes") {
		t.Fatal(c.Path, c.WikiRoot, c.ChangesRoot)
	}
}

func TestConfigFoundFromSubfolder(t *testing.T) {
	w := newWS(t)
	w.ok("setup")
	sub := filepath.Join(w.root, "llm-wiki", "concepts")
	var out bytes.Buffer
	code := Main([]string{"lint"}, IO{Env: func(string) string { return "" }, Dir: sub, Stdout: &out, Stderr: &out})
	if code != 0 || !strings.Contains(out.String(), "0 pages · 0 errors") {
		t.Fatal(out.String())
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	must(t, filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return os.WriteFile(target, readBytes(p), 0o644)
	}))
}

func TestLintWikiOnlyChecksStructureWithoutSources(t *testing.T) {
	w := newWS(t)
	w.ok("setup")
	page := "---\ntype: policy\ntitle: Returns\ndescription: Return window\nowner: support-lead\nsources:\n  - ^[/notes/policy.md:1]\n---\nReturn within 14 days. ^[/notes/policy.md:1] See [[concepts/window]].\n"
	concept := "---\ntype: concept\ntitle: Window\ndescription: Return window\nowner: support-lead\nsources:\n  - ^[/notes/policy.md:1]\nrelated:\n  - policies/returns\n---\nWindow. ^[/notes/policy.md:1]\n"
	must(t, os.WriteFile(filepath.Join(w.root, "llm-wiki/policies/returns.md"), []byte(page), 0o644))
	must(t, os.WriteFile(filepath.Join(w.root, "llm-wiki/concepts/window.md"), []byte(concept), 0o644))
	w.ok("index")
	w.ok("registry", "record")
	if !strings.Contains(w.ok("lint").stdout, "0 errors") {
		t.Fatal("workspace lint")
	}
	// The wiki repository checked out alone: no wiki.toml, no sources.
	repo := filepath.Join(resolvePath(t.TempDir(), ""), "wiki-repo")
	copyTree(t, filepath.Join(w.root, "llm-wiki"), repo)
	lint := func(dir string, env map[string]string, args ...string) result {
		var out, errOut bytes.Buffer
		code := Main(append([]string{"lint"}, args...), IO{Env: func(k string) string { return env[k] }, Dir: dir, Stdout: &out, Stderr: &errOut})
		return result{code, out.String(), errOut.String()}
	}
	setHome(t, resolvePath(t.TempDir(), "")) // no ~/company-wiki on the CI runner
	if r := lint(repo, nil); r.code != 1 {
		t.Fatal("plain lint should fail without a configuration", r)
	}
	r := lint(repo, nil, "--wiki-only")
	if r.code != 0 || !strings.Contains(r.stdout, "2 pages · 0 errors") || !strings.Contains(r.stdout, "[INFO] 인용 2개(원문 1개)") || strings.Contains(r.stdout, "인용 파일 없음") {
		t.Fatal(r)
	}
	if r := lint(t.TempDir(), map[string]string{"WIKI_ROOT": repo}, "--wiki-only"); r.code != 0 {
		t.Fatal("WIKI_ROOT", r)
	}
	if r := lint(t.TempDir(), nil, "--wiki-only"); r.code != 1 || !strings.Contains(r.stdout, "위키 폴더가 아님") {
		t.Fatal("empty folder accepted", r)
	}
	if r := lint(repo, nil, "--wiki-only", "extra"); r.code != 2 {
		t.Fatal("unknown argument accepted", r)
	}
	for _, broken := range []struct{ old, new, want string }{
		{"[[concepts/window]]", "[[concepts/nowhere]]", "없는 페이지로의 위키링크"},
		{"owner: support-lead\n", "", "필수 키 누락 `owner`"},
		{"type: policy", "type: concept", "concepts/ 아래에 둔다"},
		{"Return within 14 days. ^[/notes/policy.md:1]", "Return within 14 days. ^[/notes/policy.md:3-2]", "인용 줄 범위 오류"},
	} {
		must(t, os.WriteFile(filepath.Join(repo, "policies/returns.md"), []byte(strings.Replace(page, broken.old, broken.new, 1)), 0o644))
		if r := lint(repo, nil, "--wiki-only"); r.code != 1 || !strings.Contains(r.stdout, broken.want) {
			t.Fatal(broken.want, r)
		}
	}
	must(t, os.WriteFile(filepath.Join(repo, "policies/returns.md"), []byte(page), 0o644))
	must(t, os.WriteFile(filepath.Join(repo, "concepts/window.md"), []byte(strings.Replace(concept, "policies/returns", "policies/gone", 1)), 0o644))
	if r := lint(repo, nil, "--wiki-only"); r.code != 1 || !strings.Contains(r.stdout, "없는 대상 `policies/gone`") {
		t.Fatal("related target", r)
	}
}

func TestLintAndRegistryOutputIsDeterministic(t *testing.T) {
	w := newWS(t)
	w.ok("setup")
	lines := []string{}
	for i := 1; i <= 12; i++ {
		lines = append(lines, fmt.Sprintf("Line %d.", i))
	}
	must(t, os.WriteFile(filepath.Join(w.root, "raw/policy.md"), []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	// Cited out of order, so first appearance is not numeric order.
	cites := "^[/notes/policy.md:10] ^[/notes/policy.md:2-3] ^[/notes/policy.md:2] ^[/notes/policy.md:9]"
	for _, name := range []string{"b", "a"} {
		page := "---\ntype: policy\ntitle: P " + name + "\ndescription: d\nowner: support-lead\nsources:\n  - ^[/notes/policy.md:2-3]\n---\nText. " + cites + "\n"
		must(t, os.WriteFile(filepath.Join(w.root, "llm-wiki/policies/"+name+".md"), []byte(page), 0o644))
	}
	w.ok("index")
	w.ok("registry", "record")
	// Insert a line at the top: every cited range moves down by one.
	must(t, os.WriteFile(filepath.Join(w.root, "raw/policy.md"), []byte("New first line.\n"+strings.Join(lines, "\n")+"\n"), 0o644))
	first := w.run("lint").stdout
	if !strings.Contains(first, "인용 줄 이동") {
		t.Fatal(first)
	}
	order := []string{"policies/a.md: 인용 줄 이동 `/notes/policy.md:2-2`", "policies/a.md: 인용 줄 이동 `/notes/policy.md:2-3`",
		"policies/a.md: 인용 줄 이동 `/notes/policy.md:9-9`", "policies/a.md: 인용 줄 이동 `/notes/policy.md:10-10`", "policies/b.md: 인용 줄 이동 `/notes/policy.md:2-2`"}
	last := -1
	for _, o := range order {
		i := strings.Index(first, o)
		if i <= last {
			t.Fatalf("%q out of order in\n%s", o, first)
		}
		last = i
	}
	reg := w.ok("registry").stdout
	if i, j := strings.Index(reg, "policies/a (줄 2-2 → 3-3)"), strings.Index(reg, "policies/a (줄 10-10 → 11-11)"); i < 0 || j < i {
		t.Fatal(reg)
	}
	regJSON := w.ok("registry", "--json").stdout
	for n := 0; n < 20; n++ {
		if got := w.run("lint").stdout; got != first {
			t.Fatalf("lint run %d differs:\n%s\n---\n%s", n, first, got)
		}
		if w.ok("registry").stdout != reg || w.ok("registry", "--json").stdout != regJSON {
			t.Fatal("registry output differs", n)
		}
	}
}

func TestRegistrySourceAcceptsFilePaths(t *testing.T) {
	w := newWS(t)
	home := w.home
	must(t, os.MkdirAll(filepath.Join(home, "hdocs"), 0o755))
	must(t, os.WriteFile(filepath.Join(home, "hdocs/guide.md"), []byte("Be kind.\n"), 0o644))
	w.writeConfig(w.read("wiki.toml") + "[sources.home]\nkind=\"documents\"\npath=\"~/hdocs\"\n")
	w.ok("setup")
	page := "---\ntype: policy\ntitle: Returns\ndescription: Return window\nowner: support-lead\nsources:\n  - ^[/notes/policy.md:1]\n---\nReturn within 14 days. ^[/notes/policy.md:1] Be kind. ^[/home/guide.md:1]\n"
	must(t, os.WriteFile(filepath.Join(w.root, "llm-wiki/policies/returns.md"), []byte(page), 0o644))
	w.ok("registry", "record")
	at := func(dir, source string) result {
		var out, errOut bytes.Buffer
		env := func(string) string { return "" }
		code := Main([]string{"registry", "--source", source, "--json"}, IO{Env: env, Dir: dir, Stdout: &out, Stderr: &errOut})
		return result{code, out.String(), errOut.String()}
	}
	cites := func(r result, key string) bool {
		var got struct {
			Citations map[string]map[string][][]string
		}
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &got) != nil || len(got.Citations) != 1 {
			return false
		}
		return len(got.Citations[key]["fresh"]) == 1 && got.Citations[key]["fresh"][0][0] == "policies/returns"
	}
	for _, c := range []struct{ dir, source, key string }{
		{w.root, "/notes/policy.md", "/notes/policy.md"},
		{w.root, "raw/policy.md", "/notes/policy.md"},
		{filepath.Join(w.root, "raw"), "policy.md", "/notes/policy.md"},
		{filepath.Join(w.root, "llm-wiki"), "../raw/./policy.md", "/notes/policy.md"},
		{t.TempDir(), filepath.Join(w.root, "raw", "policy.md"), "/notes/policy.md"},
		{w.root, "~/hdocs/guide.md", "/home/guide.md"},
		{w.root, filepath.Join(home, "hdocs", "guide.md"), "/home/guide.md"},
	} {
		if r := at(c.dir, c.source); !cites(r, c.key) {
			t.Fatal(c.source, r)
		}
	}
	if r := at(w.root, "/notes/absent.md"); r.code != 0 || strings.Contains(r.stdout, "policies/returns") {
		t.Fatal("alias form of an uncited file", r)
	}
	for _, outside := range []string{"wiki.toml", filepath.Join(w.root, "llm-wiki", "policies", "returns.md"), "/nowhere/policy.md", "raw"} {
		if r := at(w.root, outside); r.code != 1 || !strings.Contains(r.stderr, "등록 원천 밖의 경로") {
			t.Fatal(outside, r)
		}
	}
}

// /x/... citations where x is not an alias are root-relative; every spelling of one file matches.
func TestRegistrySourceMatchesResolvedFile(t *testing.T) {
	w := newWS(t)
	w.ok("setup")
	must(t, os.WriteFile(filepath.Join(w.root, "raw/other.md"), []byte("Other.\n"), 0o644))
	pages := map[string]string{"policies/by-alias.md": "^[/notes/policy.md:1]", "policies/by-root.md": "^[/raw/policy.md:1]"}
	for rel, cite := range pages {
		page := "---\ntype: policy\ntitle: " + rel + "\ndescription: d\nowner: support-lead\nsources:\n  - " + cite + "\n---\nText. " + cite + "\n"
		must(t, os.WriteFile(filepath.Join(w.root, "llm-wiki", filepath.FromSlash(rel)), []byte(page), 0o644))
	}
	w.ok("registry", "record")
	query := func(dir, source string) (result, map[string]map[string][][]string) {
		var out, errOut bytes.Buffer
		code := Main([]string{"registry", "--source", source, "--json"}, IO{Env: func(string) string { return "" }, Dir: dir, Stdout: &out, Stderr: &errOut})
		var got struct{ Citations map[string]map[string][][]string }
		_ = json.Unmarshal(out.Bytes(), &got)
		return result{code, out.String(), errOut.String()}, got.Citations
	}
	both := []string{"/notes/policy.md", "/raw/policy.md"}
	for _, c := range []struct{ dir, source string }{
		{w.root, "/notes/policy.md"},
		{w.root, "/raw/policy.md"},
		{w.root, "/raw/./policy.md"},
		{w.root, "raw/policy.md"},
		{t.TempDir(), filepath.Join(w.root, "raw", "policy.md")},
		{filepath.Join(w.root, "raw"), "policy.md"},
	} {
		r, got := query(c.dir, c.source)
		if r.code != 0 || len(got) != 2 {
			t.Fatal(c.source, r)
		}
		for _, k := range both {
			if len(got[k]["fresh"]) != 1 {
				t.Fatal(c.source, k, r)
			}
		}
	}
	// Inside a source but uncited: no pages. Outside every source and uncited: an error.
	for _, source := range []string{"/raw/other.md", "/notes/other.md", "raw/other.md"} {
		if r, got := query(w.root, source); r.code != 0 || len(got) != 0 {
			t.Fatal(source, r)
		}
	}
	for _, source := range []string{"/wiki.toml", "/llm-wiki/policies/by-root.md", "/nowhere/x.md"} {
		if r, _ := query(w.root, source); r.code != 1 || !strings.Contains(r.stderr, "등록 원천 밖의 경로") {
			t.Fatal(source, r)
		}
	}
}

func TestSetupSkipsAlreadyRegisteredSources(t *testing.T) {
	w := newWS(t)
	home := w.home
	must(t, os.MkdirAll(filepath.Join(home, "hdocs"), 0o755))
	w.ok("setup")
	before := w.read("wiki.toml") // starts with a BOM, so any rewrite would show
	for _, c := range []struct{ spec, notice string }{
		{"documents=raw", "이미 등록됨: documents raw (별칭 notes)"},
		{"documents=./raw/", "이미 등록됨: documents ./raw/ (별칭 notes)"},
		{"documents=llm-wiki/../raw", "(별칭 notes)"},
		{"documents=" + filepath.Join(w.root, "raw"), "(별칭 notes)"},
		{"documents:docs2=raw", "이미 등록됨: documents raw (별칭 notes). 별칭 docs2 는 추가하지 않음"},
		{"claude=sessions", "이미 등록됨: claude sessions (별칭 claude-sessions)"},
	} {
		r := w.ok("setup", "--source", c.spec)
		if !strings.Contains(r.stdout, c.notice) || w.read("wiki.toml") != before {
			t.Fatal(c.spec, r.stdout)
		}
	}
	// The same folder as another kind is a different source.
	if r := w.ok("setup", "--source", "code=raw"); strings.Contains(r.stdout, "이미 등록됨") || !strings.Contains(w.read("wiki.toml"), "[sources.code]\nkind = \"code\"\npath = '~/company-wiki/raw'") {
		t.Fatal("code source not added", r.stdout)
	}
	// ~ and absolute spellings of one folder, in one run and across runs.
	added := w.read("wiki.toml")
	r := w.ok("setup", "--source", "documents=~/hdocs", "--source", "documents="+filepath.Join(home, "hdocs"))
	toml := w.read("wiki.toml")
	if strings.Count(toml, "kind = \"documents\"") != 1 || !strings.Contains(toml, "path = '~/hdocs'") || !strings.Contains(r.stdout, "(별칭 documents)") || !strings.HasPrefix(toml, strings.TrimRight(added, "\n")) {
		t.Fatal(toml, r.stdout)
	}
	w.ok("setup", "--source", "documents="+filepath.Join(home, "hdocs"))
	if w.read("wiki.toml") != toml {
		t.Fatal("absolute spelling of ~/hdocs added again")
	}
	if strings.Contains(w.ok("lint").stdout, "ERROR") {
		t.Fatal("lint")
	}
}

// Adding sources appends blocks; the existing bytes, BOM and line endings are kept.
func TestSetupAppendKeepsBOMAndLineEndings(t *testing.T) {
	for _, c := range []struct{ name, old, want string }{
		{"bom crlf", "\ufeff[wiki]\r\ntitle = \"T\"\r\n[sources.notes]\r\nkind = \"documents\"\r\npath = \"raw\"\r\n# my note\r\n\r\n\r\n", "\r\n\r\n\r\n[sources.code]\r\n"},
		{"crlf no final newline", "[wiki]\r\n[sources.notes]\r\nkind = \"documents\"\r\npath = \"raw\" # end", "path = \"raw\" # end\r\n\r\n[sources.code]\r\n"},
		{"lf", "\ufeff[wiki]\n[sources.notes]\nkind = \"documents\"\npath = \"raw\"\n", "path = \"raw\"\n\n[sources.code]\nkind = \"code\"\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWS(t)
			w.writeConfig(c.old)
			w.ok("setup", "--source", "code=sessions", "--source", "slack=raw")
			got := w.read("wiki.toml")
			if !strings.HasPrefix(got, c.old) || !strings.Contains(got, c.want) || !strings.Contains(got, "[sources.slack]") {
				t.Fatalf("%q", got)
			}
			added := got[len(c.old):]
			if crlf := strings.Contains(c.old, "\r\n"); crlf && strings.Count(added, "\n") != strings.Count(added, "\r\n") || !crlf && strings.Contains(added, "\r") {
				t.Fatalf("mixed line endings %q", added)
			}
			if strings.Count(got, "\ufeff") != strings.Count(c.old, "\ufeff") {
				t.Fatal("BOM changed")
			}
			if r := w.ok("lint"); !strings.Contains(r.stdout, "0 errors") {
				t.Fatal(r.stdout)
			}
		})
	}
}

func TestNewWorkspaceSkipsDuplicateSourcesInOneRun(t *testing.T) {
	w := newWS(t)
	must(t, os.Remove(w.config))
	r := w.ok("setup", "--source", "documents=raw", "--source", "documents:again=./raw", "--source", "code=raw")
	toml := w.read("wiki.toml")
	if strings.Count(toml, "[sources.") != 2 || !strings.Contains(toml, "[sources.documents]") || !strings.Contains(toml, "[sources.code]") ||
		!strings.Contains(r.stdout, "이미 등록됨: documents ./raw (별칭 documents). 별칭 again 는 추가하지 않음") {
		t.Fatal(toml, r.stdout)
	}
}

// Setup always prepares <home>/company-wiki; --source paths come from the current folder.
func TestSetupFromAnyFolderUsesFixedRootAndStoresAbsoluteSources(t *testing.T) {
	home := resolvePath(t.TempDir(), "")
	setHome(t, home)
	outside := resolvePath(t.TempDir(), "") // not under home
	must(t, os.MkdirAll(filepath.Join(outside, "project", "docs"), 0o755))
	must(t, os.MkdirAll(filepath.Join(home, "notes"), 0o755))
	must(t, os.MkdirAll(filepath.Join(home, "sub"), 0o755))
	run := func(dir string, args ...string) result {
		var out, errOut bytes.Buffer
		env := func(string) string { return "" }
		code := Main(args, IO{Env: env, Dir: dir, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
		return result{code, out.String(), errOut.String()}
	}
	if r := run(filepath.Join(outside, "project"), "setup", "--source", "documents=docs"); r.code != 0 {
		t.Fatal(r)
	}
	if r := run(filepath.Join(home, "sub"), "setup", "--source", "documents:notes=../notes"); r.code != 0 {
		t.Fatal(r)
	}
	root := filepath.Join(home, "company-wiki")
	toml := string(readBytes(filepath.Join(root, "wiki.toml")))
	if !strings.Contains(toml, "path = '"+filepath.Join(outside, "project", "docs")+"'") || !strings.Contains(toml, "path = '~/notes'") {
		t.Fatal(toml)
	}
	for _, p := range []string{"llm-wiki/index.md", "llm-wiki/SCHEMA.md", "llm-wiki/policies", "workspace/wiki-changes", "AGENTS.md", ".claude/skills/wiki-review/SKILL.md"} {
		if !exists(filepath.Join(root, filepath.FromSlash(p))) {
			t.Fatal("missing", p)
		}
	}
	if exists(filepath.Join(outside, "project", "wiki.toml")) || exists(filepath.Join(outside, "project", "llm-wiki")) {
		t.Fatal("setup wrote outside the fixed root")
	}
	again := run(outside, "setup")
	if again.code != 0 || !strings.Contains(again.stdout, "workspace: 기존 폴더 사용") || string(readBytes(filepath.Join(root, "wiki.toml"))) != toml {
		t.Fatal(again)
	}
	// Other commands find the fixed root from any folder.
	var out bytes.Buffer
	if code := Main([]string{"lint"}, IO{Env: func(string) string { return "" }, Dir: outside, Stdout: &out, Stderr: &out}); code != 0 || !strings.Contains(out.String(), "0 errors") {
		t.Fatal(out.String())
	}
}

func writeObsidian(t *testing.T, vaults map[string]any) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"vaults": vaults})
	p := obsidianConfigs()[0]
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(t, os.WriteFile(p, data, 0o644))
}

func listDir(t *testing.T, dir string) []string {
	entries, err := os.ReadDir(dir)
	must(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestSetupWorkspaceCases(t *testing.T) {
	setup := func(w *ws, dir string, args ...string) result {
		var out, errOut bytes.Buffer
		code := Main(append([]string{"setup", "--source", "documents=" + filepath.Join(w.home, "docs")}, args...),
			IO{Env: func(string) string { return "" }, Dir: dir, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
		return result{code, out.String(), errOut.String()}
	}
	fresh := func(t *testing.T) (*ws, string) {
		w := newWS(t)
		must(t, os.MkdirAll(filepath.Join(w.home, "docs"), 0o755))
		vaults := resolvePath(t.TempDir(), "")
		for _, v := range []string{"open-wiki", "chosen", "older"} {
			must(t, os.MkdirAll(filepath.Join(vaults, v, ".obsidian"), 0o755))
			must(t, os.WriteFile(filepath.Join(vaults, v, "note.md"), []byte("my note\n"), 0o644))
		}
		must(t, os.WriteFile(filepath.Join(vaults, "open-wiki", "wiki.toml"), []byte("[wiki]\n"), 0o644))
		return w, vaults
	}
	linkedTo := func(t *testing.T, w *ws, vault string) {
		t.Helper()
		ws := filepath.Join(w.root, "workspace")
		if _, err := os.Readlink(ws); err != nil {
			t.Fatal("workspace is not a link", err)
		}
		if got := listDir(t, vault); strings.Join(got, ",") != ".obsidian,note.md,wiki-changes" {
			t.Fatal("vault contents", got)
		}
		if string(readBytes(filepath.Join(vault, "note.md"))) != "my note\n" || !isDir(filepath.Join(ws, "wiki-changes")) || !isFile(filepath.Join(ws, "note.md")) {
			t.Fatal("link does not reach the vault")
		}
	}

	t.Run("obsidian", func(t *testing.T) {
		w, vaults := fresh(t)
		writeObsidian(t, map[string]any{
			"a": map[string]any{"path": filepath.Join(vaults, "open-wiki"), "ts": 300, "open": true},
			"b": map[string]any{"path": filepath.Join(vaults, "chosen"), "ts": 200},
			"c": map[string]any{"path": filepath.Join(vaults, "older"), "ts": 100},
			"d": map[string]any{"path": filepath.Join(vaults, "missing"), "ts": 900, "open": true},
			"e": map[string]any{"path": filepath.Join(w.root, "llm-wiki"), "ts": 950},
			"f": map[string]any{"path": w.home, "ts": 990},
		})
		r := setup(w, t.TempDir())
		if r.code != 0 || !strings.Contains(r.stdout, "Obsidian Vault 자동 감지") || !strings.Contains(r.stdout, filepath.Join(vaults, "chosen")) || !strings.Contains(r.stdout, "--vault") {
			t.Fatal(r)
		}
		linkedTo(t, w, filepath.Join(vaults, "chosen"))
		if got := listDir(t, filepath.Join(vaults, "older")); strings.Join(got, ",") != ".obsidian,note.md" {
			t.Fatal("other vault touched", got)
		}
		// Rerun keeps the link, and --vault does not replace an existing workspace.
		r = setup(w, t.TempDir(), "--vault", filepath.Join(vaults, "older"))
		if r.code != 0 || !strings.Contains(r.stdout, "workspace: 기존 링크 사용") || !strings.Contains(r.stdout, "--vault 무시") {
			t.Fatal(r)
		}
		linkedTo(t, w, filepath.Join(vaults, "chosen"))
	})

	t.Run("vault flag", func(t *testing.T) {
		w, vaults := fresh(t)
		writeObsidian(t, map[string]any{"b": map[string]any{"path": filepath.Join(vaults, "chosen"), "ts": 200, "open": true}})
		for _, bad := range []string{"nowhere", filepath.Join(vaults, "open-wiki"), filepath.Join(w.root, "inside"), w.home} {
			if bad == filepath.Join(w.root, "inside") {
				must(t, os.MkdirAll(bad, 0o755))
			}
			if r := setup(w, vaults, "--vault", bad); r.code != 1 || exists(filepath.Join(w.root, "workspace")) || exists(filepath.Join(w.root, "llm-wiki")) {
				t.Fatal(bad, r)
			}
		}
		r := setup(w, vaults, "--vault", "older") // relative to the current folder
		if r.code != 0 || !strings.Contains(r.stdout, "--vault의 Vault에 연결") {
			t.Fatal(r)
		}
		linkedTo(t, w, filepath.Join(vaults, "older"))
		if strings.Join(listDir(t, filepath.Join(vaults, "chosen")), ",") != ".obsidian,note.md" {
			t.Fatal("detected vault used despite --vault")
		}
	})

	t.Run("new folder", func(t *testing.T) {
		w, _ := fresh(t)
		r := setup(w, t.TempDir())
		ws := filepath.Join(w.root, "workspace")
		if _, err := os.Readlink(ws); r.code != 0 || err == nil || !isDir(filepath.Join(ws, "wiki-changes")) || !strings.Contains(r.stdout, "새 폴더 생성") {
			t.Fatal(r)
		}
	})

	t.Run("dangling link", func(t *testing.T) {
		w, vaults := fresh(t)
		gone := filepath.Join(vaults, "gone")
		must(t, os.MkdirAll(gone, 0o755))
		must(t, os.MkdirAll(w.root, 0o755))
		must(t, linkDir(gone, filepath.Join(w.root, "workspace")))
		must(t, os.Remove(gone))
		if r := setup(w, t.TempDir()); r.code != 1 || !strings.Contains(r.stderr, "링크의 대상이 없음") {
			t.Fatal(r)
		}
	})
}

func TestDetectVaultPrefersOpenThenRecent(t *testing.T) {
	setHome(t, resolvePath(t.TempDir(), ""))
	root := filepath.Join(resolvePath(t.TempDir(), ""), "company-wiki")
	dir := resolvePath(t.TempDir(), "")
	for _, v := range []string{"x", "y", "z"} {
		must(t, os.MkdirAll(filepath.Join(dir, v), 0o755))
	}
	if got, _ := detectVault(root); got != "" {
		t.Fatal("no obsidian.json", got)
	}
	writeObsidian(t, map[string]any{"x": map[string]any{"path": filepath.Join(dir, "x"), "ts": 300}, "y": map[string]any{"path": filepath.Join(dir, "y"), "ts": 100, "open": true}})
	if got, _ := detectVault(root); got != filepath.Join(dir, "y") {
		t.Fatal("open vault not preferred", got)
	}
	writeObsidian(t, map[string]any{"x": map[string]any{"path": filepath.Join(dir, "x"), "ts": 100}, "z": map[string]any{"path": filepath.Join(dir, "z"), "ts": 300}})
	if got, _ := detectVault(root); got != filepath.Join(dir, "z") {
		t.Fatal("latest vault not preferred", got)
	}
}

func TestLintRejectsChangeMaterialsInWiki(t *testing.T) {
	w := newWS(t)
	w.ok("setup")
	wiki := filepath.Join(w.root, "llm-wiki")
	must(t, os.MkdirAll(filepath.Join(wiki, ".git", "x"), 0o755))
	must(t, os.WriteFile(filepath.Join(wiki, ".git", "x", "card.json"), []byte("{}"), 0o644))
	wikiOnly := func() result {
		var out, errOut bytes.Buffer
		code := Main([]string{"lint", "--wiki-only"}, IO{Env: func(string) string { return "" }, Dir: wiki, Stdout: &out, Stderr: &errOut})
		return result{code, out.String(), errOut.String()}
	}
	if r := w.run("lint"); r.code != 0 || wikiOnly().code != 0 {
		t.Fatal(".git or .verification flagged", r)
	}
	for _, rel := range []string{"card.json", "policies/x.md.txt", "reviews/b/topics/t/r0001/manifest.json", "b/decisions.jsonl", "b/applied.jsonl",
		"concepts/inventory.json", "response.json"} {
		p := filepath.Join(wiki, filepath.FromSlash(rel))
		must(t, os.MkdirAll(filepath.Dir(p), 0o755))
		must(t, os.WriteFile(p, []byte("{}"), 0o644))
		want := "[ERROR] " + rel + ": 변경 절차 자료는 위키에 두지 않는다. workspace/wiki-changes에 둔다"
		if r := w.run("lint"); r.code != 1 || !strings.Contains(r.stdout, want) {
			t.Fatal(rel, r)
		}
		if r := wikiOnly(); r.code != 1 || !strings.Contains(r.stdout, want) {
			t.Fatal("--wiki-only", rel, r)
		}
		must(t, os.Remove(p))
	}
	if r := w.run("lint"); r.code != 0 {
		t.Fatal(r)
	}
}

// WIKI_CONFIG no longer selects a wiki; a leftover setting is reported instead of ignored.
func TestWikiConfigIsRejectedNotIgnored(t *testing.T) {
	w := newWS(t)
	for _, args := range [][]string{{"setup"}, {"lint"}, {"registry"}, {"review", "status"}} {
		var out, errOut bytes.Buffer
		env := func(k string) string {
			if k == "WIKI_CONFIG" {
				return filepath.Join(t.TempDir(), "wiki.toml")
			}
			return ""
		}
		code := Main(args, IO{Env: env, Dir: w.root, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut})
		if code != 2 || !strings.Contains(errOut.String(), "WIKI_CONFIG is no longer supported") || out.Len() != 0 {
			t.Fatal(args, code, out.String(), errOut.String())
		}
	}
}
