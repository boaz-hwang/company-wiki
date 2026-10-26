package wiki

import (
	"bytes"
	"encoding/json"
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
	env := func(k string) string {
		if k == "WIKI_CONFIG" {
			return w.config
		}
		return ""
	}
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
	w.ok("review", "apply", "first", "returns")
	receipt := w.read("workspace/wiki-changes/first/applied.jsonl")
	w.ok("review", "apply", "first", "returns")
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
		env := func(k string) string {
			if k == "WIKI_CONFIG" {
				return w.config
			}
			return ""
		}
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
	elsewhere := filepath.Join(outside, "elsewhere", "wiki.toml")
	run := func(dir string, args ...string) result {
		var out, errOut bytes.Buffer
		env := func(k string) string {
			if k == "WIKI_CONFIG" {
				return elsewhere // setup ignores it
			}
			return ""
		}
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
	if exists(elsewhere) || exists(filepath.Join(outside, "project", "wiki.toml")) || exists(filepath.Join(outside, "project", "llm-wiki")) {
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
