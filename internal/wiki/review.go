package wiki

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Review keeps versioned wiki proposals, human decisions and checked publication.
//
// It is the approval path for agent proposals: the owner answers in the session, decide records that
// answer and apply writes the approved version, which is then committed to the wiki repository's main.
// Review records live in the change materials folder (workspace/wiki-changes), never inside the wiki.
type Review struct {
	cfg                       *Config
	wiki, repo, changes, root string
	journal                   string
	// write is replaceable so tests can simulate a failed write.
	write func(path string, data []byte) error
}

var (
	judgmentKinds = map[string]bool{"fact": true, "interpretation": true, "naming": true, "relationship": true, "confidence": true, "scope": true}
	pageRE        = regexp.MustCompile(`^(?:concepts|entities|policies)/(?:[a-z0-9][a-z0-9-]*/)*[a-z0-9][a-z0-9-]*\.md$`)
	idRE          = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	decisions     = map[string]bool{"approved": true, "changes_requested": true, "deferred": true, "rejected": true}
)

// NewReview opens the change materials of cfg.
func NewReview(cfg *Config) (*Review, error) {
	return newReview(cfg, cfg.WikiRoot, cfg.RepoRoot, cfg.ChangesRoot)
}

func newReview(cfg *Config, wiki, repo, changes string) (*Review, error) {
	r := &Review{cfg: cfg, wiki: resolvePath(wiki, ""), repo: resolvePath(repo, ""), changes: resolvePath(changes, ""), write: atomicWrite}
	if r.changes == r.wiki || within(r.changes, r.wiki) || within(r.wiki, r.changes) {
		return nil, errors.New("변경 절차 폴더(workspace/wiki-changes)는 위키 폴더와 분리한다")
	}
	// Batches sit directly in the change materials folder: <batch>/topics/<topic>/rNNNN, decisions.jsonl, applied.jsonl.
	r.root = r.changes
	r.journal = filepath.Join(r.root, "apply-journal.json")
	return r, nil
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05+00:00") }

// digest hashes bytes, or the Python json.dumps(sort_keys=True) form of a value; nil stays nil.
func digest(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		if x == nil {
			return nil
		}
		return sha(x)
	}
	return sha([]byte(pyJSON(v, true)))
}

func digestStr(v any) string {
	s, _ := digest(v).(string)
	return s
}

func readBytes(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	if b == nil {
		b = []byte{}
	}
	return b
}

// safe joins rel to root, refusing absolute paths, `..`, symlinks and escapes.
func safe(root, rel string) (string, error) {
	slash := filepath.ToSlash(rel)
	if filepath.IsAbs(rel) || strings.HasPrefix(slash, "/") {
		return "", fmt.Errorf("허용되지 않은 경로: %s", rel)
	}
	for _, part := range strings.Split(slash, "/") {
		if part == ".." {
			return "", fmt.Errorf("허용되지 않은 경로: %s", rel)
		}
	}
	path := filepath.Join(root, filepath.FromSlash(slash))
	for part := path; part != root && part != filepath.Dir(part); part = filepath.Dir(part) {
		if st, err := os.Lstat(part); err == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("심링크 경로는 변경하지 않음: %s", path)
		}
	}
	if !within(resolvePath(path, ""), resolvePath(root, "")) {
		return "", fmt.Errorf("루트 밖 경로: %s", path)
	}
	return path, nil
}

func (r *Review) batch(batch string) (string, error) {
	if !idRE.MatchString(batch) {
		return "", errors.New("batch는 영문 소문자·숫자·하이픈")
	}
	return safe(r.root, batch)
}

func (r *Review) topic(batch, topic string) (string, error) {
	if !idRE.MatchString(topic) {
		return "", errors.New("topic은 영문 소문자·숫자·하이픈")
	}
	b, err := r.batch(batch)
	if err != nil {
		return "", err
	}
	return safe(b, "topics/"+topic)
}

func (r *Review) lock() (func(), error) {
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		return nil, err
	}
	return fileLock(filepath.Join(r.root, ".lock"))
}

func (r *Review) events(batch, name string) ([]map[string]any, error) {
	b, err := r.batch(batch)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(b, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		v, err := decodeOrdered([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, plain(v).(map[string]any))
	}
	return out, nil
}

func (r *Review) event(batch, name string, value any) error {
	b, err := r.batch(batch)
	if err != nil {
		return err
	}
	p := filepath.Join(b, name)
	return r.write(p, append(readBytes(p), []byte(pyJSON(value, false)+"\n")...))
}

// manifest is a sealed review version.
type manifest struct {
	Files        map[string]any // rel → {"before": digest|nil, "after": digest}
	Dependencies map[string]any // ref → digest
	Revision     string
	Created      string
}

func (m manifest) fileNames() []string {
	names := make([]string, 0, len(m.Files))
	for k := range m.Files {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func (m manifest) hash(rel, which string) any {
	if f, ok := m.Files[rel].(map[string]any); ok {
		return f[which]
	}
	return nil
}

func readJSON(p string) (any, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	return decodeOrdered(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
}

// card accessors work on decoded JSON.
func cardStr(card any, key string) string { return str(obj(card)[key]) }

func cardList(card any, key string) []any {
	l, _ := obj(card)[key].([]any)
	return l
}

func strList(v any) []string {
	var out []string
	l, _ := v.([]any)
	for _, x := range l {
		out = append(out, str(x))
	}
	return out
}

type payload struct{ old, new []byte }

func (r *Review) current(batch, topic string) (string, manifest, any, error) {
	var m manifest
	parent, err := r.topic(batch, topic)
	if err != nil {
		return "", m, nil, err
	}
	revisions, _ := filepath.Glob(filepath.Join(parent, "r[0-9]*"))
	sort.Strings(revisions)
	if len(revisions) == 0 {
		return "", m, nil, fmt.Errorf("검토안 없음: %s", topic)
	}
	p := revisions[len(revisions)-1]
	rawM, err := readJSON(filepath.Join(p, "manifest.json"))
	if err != nil {
		return "", m, nil, err
	}
	card, err := readJSON(filepath.Join(p, "card.json"))
	if err != nil {
		return "", m, nil, err
	}
	mm := plain(rawM).(map[string]any)
	m.Files, _ = mm["files"].(map[string]any)
	m.Dependencies, _ = mm["dependencies"].(map[string]any)
	m.Revision, _ = mm["revision"].(string)
	m.Created, _ = mm["created"].(string)
	fingerprint := map[string]any{"card": card, "files": mm["files"], "dependencies": mm["dependencies"]}
	if digestStr(fingerprint) != m.Revision {
		return "", m, nil, fmt.Errorf("봉인된 검토안 변경: %s. prepare로 새 버전을 만드세요.", topic)
	}
	after := filepath.Join(p, "after")
	actual := map[string]bool{}
	bad := false
	_ = filepath.WalkDir(after, func(f string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md.txt") {
			bad = true
		}
		rel, _ := filepath.Rel(after, f)
		actual[strings.TrimSuffix(filepath.ToSlash(rel), ".txt")] = true
		return nil
	})
	if bad {
		return "", m, nil, fmt.Errorf("초안 사본은 .md.txt만 허용: %s", topic)
	}
	if len(actual) != len(m.Files) {
		return "", m, nil, fmt.Errorf("선언 밖 초안 파일: %s", topic)
	}
	for rel := range m.Files {
		if !actual[rel] {
			return "", m, nil, fmt.Errorf("선언 밖 초안 파일: %s", topic)
		}
	}
	payloads := map[string]payload{}
	for _, rel := range m.fileNames() {
		if !pageRE.MatchString(rel) {
			return "", m, nil, fmt.Errorf("페이지 경로 오류: %s", rel)
		}
		af, err := safe(after, rel+".txt")
		if err != nil {
			return "", m, nil, err
		}
		bf, err := safe(filepath.Join(p, "before"), rel+".txt")
		if err != nil {
			return "", m, nil, err
		}
		newData, oldData := readBytes(af), readBytes(bf)
		if digest(newData) != m.hash(rel, "after") {
			return "", m, nil, fmt.Errorf("승인 버전과 다른 초안: %s/%s", topic, rel)
		}
		if digest(oldData) != m.hash(rel, "before") {
			return "", m, nil, fmt.Errorf("변경 전 사본 변조: %s/%s", topic, rel)
		}
		payloads[rel] = payload{oldData, newData}
	}
	shown, _ := os.ReadFile(filepath.Join(p, "review.md"))
	// The card lists files in the order the review document shows them.
	if string(shown) != render(m, card, payloads, strList(obj(card)["files"])) {
		return "", m, nil, fmt.Errorf("표시된 검토 문서 변경: %s", topic)
	}
	return p, m, card, nil
}

func (r *Review) resource(name string) (string, error) {
	head, tail, _ := strings.Cut(strings.TrimLeft(name, "/"), "/")
	if root := r.cfg.ExtraRoots[head]; root != "" {
		return safe(root, tail)
	}
	return safe(r.repo, strings.TrimLeft(name, "/"))
}

func (r *Review) dependency(ref string) (string, error) {
	if m := citationFullRE.FindStringSubmatch(ref); m != nil {
		path, err := r.resource(m[1])
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		lines := splitLines(normalizeEOL(data))
		a, b := atoi(m[2]), atoi(m[2])
		if m[3] != "" {
			b = atoi(m[3])
		}
		if !(1 <= a && a <= b && b <= len(lines)) {
			return "", fmt.Errorf("근거 범위 오류: %s", ref)
		}
		// Session files keep growing. Bind the cited records, not later appends.
		return sha(joinLines(lines, a, b)), nil
	}
	if !strings.HasPrefix(ref, "/") {
		return "", fmt.Errorf("의존 문서는 /경로 또는 줄 인용으로 지정: %s", ref)
	}
	p, err := r.resource(ref)
	if err != nil {
		return "", err
	}
	if !isFile(p) {
		return "", fmt.Errorf("근거 파일 없음: %s", ref)
	}
	if filepath.Ext(p) == ".jsonl" {
		return "", fmt.Errorf("세션 근거는 사용자/근거 레코드의 줄 인용 필요: %s", ref)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return sha(data), nil
}

// pages maps every page path under the wiki to its bytes.
func (r *Review) pages() map[string][]byte {
	out := map[string][]byte{}
	_ = filepath.WalkDir(r.wiki, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(r.wiki, p)
		rel = filepath.ToSlash(rel)
		if pageRE.MatchString(rel) {
			out[rel] = readBytes(p)
		}
		return nil
	})
	return out
}

// PrepareResult is printed by `review prepare`.
type PrepareResult struct {
	Topic    string `json:"topic"`
	Revision string `json:"revision"`
	Review   string `json:"review"`
}

func (r *Review) Prepare(batch, topic string, card any, draft string) (*PrepareResult, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if exists(r.journal) {
		return nil, errors.New("중단된 반영이 있습니다. recover를 먼저 실행하세요.")
	}
	c := obj(card)
	if c == nil {
		return nil, errors.New("검토 카드 필수 항목: title")
	}
	for _, key := range []string{"title", "summary", "judgments", "files"} {
		if !truthy(plain(c[key])) {
			return nil, fmt.Errorf("검토 카드 필수 항목: %s", key)
		}
	}
	deps := map[string]bool{}
	for _, d := range strList(c["dependencies"]) {
		deps[d] = true
	}
	for _, raw := range cardList(card, "judgments") {
		item := obj(raw)
		if !judgmentKinds[sget(item, "kind")] {
			return nil, errors.New("판단 유형은 fact/interpretation/naming/relationship/confidence/scope")
		}
		for _, key := range []string{"before", "proposed", "reason", "evidence"} {
			if _, ok := item[key]; !ok {
				return nil, fmt.Errorf("판단에 %s 필요", key)
			}
		}
		for _, e := range strList(item["evidence"]) {
			deps[e] = true
		}
	}
	names := strList(c["files"])
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] || !pageRE.MatchString(n) {
			return nil, errors.New("files에는 중복 없는 정식 페이지 상대경로만 허용")
		}
		seen[n] = true
	}
	draftFiles := map[string]bool{}
	notTxt := false
	_ = filepath.WalkDir(draft, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md.txt") {
			notTxt = true
		}
		rel, _ := filepath.Rel(draft, p)
		draftFiles[strings.TrimSuffix(filepath.ToSlash(rel), ".txt")] = true
		return nil
	})
	if notTxt {
		return nil, errors.New("draft 파일은 Obsidian 링크 충돌 방지를 위해 .md.txt로 저장")
	}
	if len(draftFiles) != len(seen) || !sameSet(draftFiles, seen) {
		return nil, errors.New("draft-dir 파일 목록과 card.files가 다름")
	}
	b, err := r.batch(batch)
	if err != nil {
		return nil, err
	}
	others, _ := filepath.Glob(filepath.Join(b, "topics", "*"))
	sort.Strings(others)
	for _, other := range others {
		name := filepath.Base(other)
		if name == topic {
			continue
		}
		_, om, _, err := r.current(batch, name)
		if err != nil {
			return nil, err
		}
		overlap := false
		for _, n := range names {
			if _, ok := om.Files[n]; ok {
				overlap = true
			}
		}
		applied, err := r.applied(batch, name, om.Revision)
		if err != nil {
			return nil, err
		}
		if overlap && !applied {
			return nil, fmt.Errorf("같은 페이지를 수정하는 %s과 한 승인 묶음으로 합치세요", name)
		}
	}
	files := map[string]any{}
	payloads := map[string]payload{}
	live := r.pages()
	for _, rel := range names {
		target, err := safe(r.wiki, rel)
		if err != nil {
			return nil, err
		}
		old := readBytes(target)
		df, err := safe(draft, rel+".txt")
		if err != nil {
			return nil, err
		}
		newData, err := os.ReadFile(df)
		if err != nil {
			return nil, err
		}
		if old != nil && bytes.Equal(old, newData) {
			return nil, fmt.Errorf("변경 없는 파일은 제외: %s", rel)
		}
		if !utf8.Valid(newData) {
			return nil, fmt.Errorf("UTF-8이 아닌 초안: %s", rel)
		}
		fm, text := ParseFrontmatter(string(newData))
		t, _ := fm["type"].(string)
		if fm == nil || typeFolders[t] == "" || !truthy(fm["title"]) || !truthy(fm["description"]) || !truthy(fm["owner"]) {
			return nil, fmt.Errorf("frontmatter 오류(type·title·description·owner): %s", rel)
		}
		files[rel] = map[string]any{"before": digest(old), "after": digest(newData)}
		payloads[rel] = payload{old, newData}
		// Relation evidence lives in frontmatter, so read citations from the whole page.
		for _, m := range citationRE.FindAllString(string(newData), -1) {
			deps[m] = true
		}
		// Include existing linked context even when it has no inline citation.
		for link := range links(fm, text) {
			if t := resolveLink(link, live); t != "" && !seen[t] {
				relRepo, err := filepath.Rel(r.repo, filepath.Join(r.wiki, filepath.FromSlash(t)))
				if err != nil || strings.HasPrefix(relRepo, "..") {
					return nil, fmt.Errorf("위키가 저장소 밖에 있음: %s", r.wiki)
				}
				deps["/"+filepath.ToSlash(relRepo)] = true
			}
		}
	}
	dependencies := map[string]any{}
	for _, ref := range sortedKeys(deps) {
		cite := citationFullRE.FindStringSubmatch(ref)
		name := ref
		if cite != nil {
			name = cite[1]
		}
		resource, err := r.resource(name)
		if err != nil {
			return nil, err
		}
		local := ""
		if within(resolvePath(resource, ""), r.wiki) {
			l, _ := filepath.Rel(r.wiki, resolvePath(resource, ""))
			local = filepath.ToSlash(l)
		}
		if pl, ok := payloads[local]; ok && local != "" {
			// A jointly proposed page is already bound by its after hash.
			if cite != nil {
				a, b := atoi(cite[2]), atoi(cite[2])
				if cite[3] != "" {
					b = atoi(cite[3])
				}
				if !(1 <= a && a <= b && b <= len(splitLines(pl.new))) {
					return nil, fmt.Errorf("초안 근거 범위 오류: %s", ref)
				}
			}
			continue
		}
		h, err := r.dependency(ref)
		if err != nil {
			return nil, err
		}
		dependencies[ref] = h
	}
	fingerprint := map[string]any{"card": card, "files": files, "dependencies": dependencies}
	m := manifest{Files: files, Dependencies: dependencies, Revision: digestStr(fingerprint), Created: now()}
	parent, err := r.topic(batch, topic)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	existing, _ := filepath.Glob(filepath.Join(parent, "r[0-9]*"))
	target := filepath.Join(parent, fmt.Sprintf("r%04d", len(existing)+1))
	tmp, err := os.MkdirTemp(parent, ".prepare-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	folder := filepath.Join(tmp, "revision")
	for rel, pl := range payloads {
		if pl.old != nil {
			if err := atomicWrite(filepath.Join(folder, "before", filepath.FromSlash(rel)+".txt"), pl.old); err != nil {
				return nil, err
			}
		}
		if err := atomicWrite(filepath.Join(folder, "after", filepath.FromSlash(rel)+".txt"), pl.new); err != nil {
			return nil, err
		}
	}
	// Same bytes and key order as the Python tool wrote, so either can verify the other's versions.
	fileMap := &omap{m: map[string]any{}}
	for _, rel := range names {
		before := digest(payloads[rel].old)
		fileMap.keys = append(fileMap.keys, rel)
		fileMap.m[rel] = &omap{keys: []string{"before", "after"}, m: map[string]any{"before": before, "after": digest(payloads[rel].new)}}
	}
	depMap := &omap{m: dependencies}
	depMap.keys = sortedAnyKeys(dependencies)
	manifestJSON := []byte(pyJSONIndent(&omap{keys: []string{"files", "dependencies", "revision", "created"},
		m: map[string]any{"files": fileMap, "dependencies": depMap, "revision": m.Revision, "created": m.Created}}) + "\n")
	cardJSON := []byte(pyJSONIndent(card) + "\n")
	writes := []struct {
		name string
		data []byte
	}{{"manifest.json", manifestJSON}, {"card.json", cardJSON}, {"review.md", []byte(render(m, card, payloads, names))}}
	for _, w := range writes {
		if err := atomicWrite(filepath.Join(folder, w.name), w.data); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(folder, target); err != nil {
		return nil, err
	}
	return &PrepareResult{topic, m.Revision, filepath.Join(target, "review.md")}, nil
}

// render writes the review document; current() re-renders it to detect edits.
func render(m manifest, card any, payloads map[string]payload, order []string) string {
	out := []string{"# " + cardStr(card, "title"), "", cardStr(card, "summary"), "", "검토 버전: `" + m.Revision + "`", "",
		"승인 / 수정 요청 / 보류 / 제외 중 선택. 승인 범위는 아래 변경 전후와 판단 내용입니다.", ""}
	for i, raw := range cardList(card, "judgments") {
		j := obj(raw)
		evidence := strings.Join(strList(j["evidence"]), " · ")
		if evidence == "" {
			evidence = "추가 확인 필요"
		}
		out = append(out, fmt.Sprintf("## %d. %s", i+1, str(j["kind"])), "", "- 이전: "+str(j["before"]),
			"- 제안: "+str(j["proposed"]), "- 이유: "+str(j["reason"]), "- 근거: "+evidence, "")
	}
	if lc, ok := obj(card)["link_constraints"]; ok && truthy(plain(lc)) {
		out = append(out, "## 적용 범위 검사", "", "```json", pyJSONIndent(lc), "```", "")
	}
	for _, rel := range order {
		pl := payloads[rel]
		diff := unifiedDiff(textLines(string(pl.old)), textLines(string(pl.new)), "before/"+rel, "after/"+rel)
		out = append(out, "## "+rel, "", "```diff", strings.TrimRight(diff, " \t\r\n\v\f"), "```", "")
	}
	return strings.Join(out, "\n")
}

func (r *Review) decision(batch, topic, revision string) (map[string]any, error) {
	events, err := r.events(batch, "decisions.jsonl")
	if err != nil {
		return nil, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i]["topic"] == topic && events[i]["revision"] == revision {
			return events[i], nil
		}
	}
	return nil, nil
}

func (r *Review) applied(batch, topic, revision string) (bool, error) {
	events, err := r.events(batch, "applied.jsonl")
	if err != nil {
		return false, err
	}
	for _, e := range events {
		if e["topic"] == topic && e["revision"] == revision {
			return true, nil
		}
	}
	return false, nil
}

func (r *Review) userEvidence(response map[string]any) (string, error) {
	ref, _ := response["evidence"].(string)
	quote, _ := response["quote"].(string)
	m := citationFullRE.FindStringSubmatch(ref)
	if m == nil || !(strings.HasPrefix(m[1], "/codex-sessions/") || strings.HasPrefix(m[1], "/claude-sessions/")) || strings.TrimSpace(quote) == "" {
		return "", errors.New("실제 사용자 세션 줄 인용과 원문 quote 필요")
	}
	path, err := r.resource(m[1])
	if err != nil {
		return "", err
	}
	text, err := readText(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	a, b := atoi(m[2]), atoi(m[2])
	if m[3] != "" {
		b = atoi(m[3])
	}
	var messages []string
	for i := a - 1; i < b && i < len(lines); i++ {
		if i < 0 || strings.TrimSpace(lines[i]) == "" {
			continue
		}
		row, err := decodeRecord([]byte(lines[i]))
		if err != nil {
			return "", err
		}
		for _, msg := range sessionMessages(row, false) {
			if msg.role == "user" {
				messages = append(messages, msg.text)
			}
		}
		if p := obj(row["payload"]); sget(row, "type") == "event_msg" && sget(p, "type") == "user_message" {
			messages = append(messages, sget(p, "message"))
		}
	}
	if !strings.Contains(strings.Join(messages, "\n"), quote) {
		return "", errors.New("quote가 인용된 사용자 메시지에 없음 (모델/도구 응답은 승인 근거 아님)")
	}
	return r.dependency(ref)
}

// Decide records a human decision for the exact current version.
func (r *Review) Decide(batch, topic, revision, decision string, response any) (any, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	_, m, _, err := r.current(batch, topic)
	if err != nil {
		return nil, err
	}
	if revision != m.Revision {
		return nil, errors.New("이전 검토 버전의 응답입니다. 현재 버전을 확인하세요.")
	}
	if done, err := r.applied(batch, topic, revision); err != nil {
		return nil, err
	} else if done {
		return nil, errors.New("이미 반영된 판단을 바꾸려면 새 검토안을 만드세요")
	}
	if !decisions[decision] {
		return nil, errors.New("지원하지 않는 판단")
	}
	resp := obj(response)
	if resp == nil || resp["by"] != r.cfg.Reviewer {
		return nil, fmt.Errorf("이 위키의 판단 주체는 %s", r.cfg.Reviewer)
	}
	judgments, _ := resp["judgments"].([]any)
	for _, raw := range judgments {
		item := obj(raw)
		if !judgmentKinds[sget(item, "kind")] || !truthy(plain(item["value"])) {
			return nil, errors.New("응답 판단에는 kind와 value 필요")
		}
	}
	if decision == "approved" {
		stale, err := r.stale(m)
		if err != nil {
			return nil, err
		}
		if len(stale) > 0 {
			return nil, errors.New("기준이 바뀐 검토안은 승인할 수 없습니다. prepare로 갱신하세요")
		}
	}
	evidence, err := r.userEvidence(plain(response).(map[string]any))
	if err != nil {
		return nil, err
	}
	event := &omap{m: map[string]any{}}
	for _, kv := range []struct {
		k string
		v any
	}{{"topic", topic}, {"revision", revision}, {"decision", decision}, {"at", now()}, {"response", response}, {"evidence_hash", evidence}} {
		event.keys = append(event.keys, kv.k)
		event.m[kv.k] = kv.v
	}
	return event, r.event(batch, "decisions.jsonl", event)
}

func (r *Review) stale(m manifest) ([]string, error) {
	var changed []string
	for _, rel := range m.fileNames() {
		target, err := safe(r.wiki, rel)
		if err != nil {
			return nil, err
		}
		if digest(readBytes(target)) != m.hash(rel, "before") {
			changed = append(changed, rel)
		}
	}
	refs := make([]string, 0, len(m.Dependencies))
	for ref := range m.Dependencies {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		h, err := r.dependency(ref)
		if err != nil || h != m.Dependencies[ref] {
			changed = append(changed, ref)
		}
	}
	return changed, nil
}

func links(fm Frontmatter, text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range wikilinkRE.FindAllStringSubmatch(text, -1) {
		out[m[1]] = true
	}
	for _, rel := range relations(fm) {
		out[rel.Target] = true
	}
	return out
}

func resolveLink(link string, pages map[string][]byte) string {
	slug := strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(link), ".md"), "wiki/")
	if _, ok := pages[slug+".md"]; ok {
		return slug + ".md"
	}
	var hits []string
	for p := range pages {
		if strings.HasSuffix(strings.TrimSuffix(p, ".md"), "/"+slug) {
			hits = append(hits, p)
		}
	}
	if len(hits) == 1 {
		return hits[0]
	}
	return ""
}

func (r *Review) checkConstraints(cards []any, pages map[string][]byte) error {
	edges := map[string]map[string]bool{}
	for path, data := range pages {
		fm, text := ParseFrontmatter(string(data))
		set := map[string]bool{}
		for l := range links(fm, text) {
			if t := resolveLink(l, pages); t != "" {
				set[t] = true
			}
		}
		edges[path] = set
	}
	for _, card := range cards {
		for _, raw := range cardList(card, "link_constraints") {
			rule := obj(raw)
			if rule == nil {
				return errors.New("link_constraints에는 정식 target과 inbound_from/outbound_to 필요")
			}
			_, hasIn := rule["inbound_from"]
			_, hasOut := rule["outbound_to"]
			unknown := false
			for k := range rule {
				if k != "target" && k != "inbound_from" && k != "outbound_to" {
					unknown = true
				}
			}
			targetSlug, _ := rule["target"].(string)
			if unknown || !(hasIn || hasOut) || !pageRE.MatchString(targetSlug+".md") {
				return errors.New("link_constraints에는 정식 target과 inbound_from/outbound_to 필요")
			}
			for _, dir := range []string{"inbound_from", "outbound_to"} {
				v, ok := rule[dir]
				if !ok {
					continue
				}
				list, isList := v.([]any)
				valid := isList
				for _, x := range list {
					if s, isStr := x.(string); !isStr || !pageRE.MatchString(s+".md") {
						valid = false
					}
				}
				if !valid {
					return errors.New("link_constraints 허용 목록은 정식 페이지 슬러그 배열")
				}
			}
			target := targetSlug + ".md"
			if _, ok := pages[target]; !ok {
				return fmt.Errorf("범위 검사 대상 없음: %s", target)
			}
			for _, name := range []string{"inbound_from", "outbound_to"} {
				v, ok := rule[name]
				if !ok {
					continue
				}
				allowed := map[string]bool{}
				for _, x := range strList(v) {
					allowed[x+".md"] = true
				}
				actual := map[string]bool{}
				if name == "outbound_to" {
					actual = edges[target]
				} else {
					for p, l := range edges {
						if l[target] {
							actual[p] = true
						}
					}
				}
				var outside []string
				for a := range actual {
					if !allowed[a] {
						outside = append(outside, a)
					}
				}
				if len(outside) > 0 {
					sort.Strings(outside)
					return fmt.Errorf("승인 범위 밖 연결: %s %s: %s", target, name, pyList(outside))
				}
			}
		}
	}
	return nil
}

type selection struct {
	topic string
	m     manifest
}

type previewResult struct {
	writes   map[string][]byte
	order    []string
	selected []selection
	index    []byte
	output   string
	registry []byte
}

// Preview checks the merged wiki in a temporary copy with the index builder and lint.
func (r *Review) Preview(batch string, topics []string) (string, error) {
	res, err := r.preview(batch, topics, false)
	if err != nil {
		return "", err
	}
	return res.output, nil
}

func (r *Review) preview(batch string, topics []string, requireApproval bool) (*previewResult, error) {
	pages := r.pages()
	res := &previewResult{writes: map[string][]byte{}}
	var cards []any
	for _, topic := range topics {
		path, m, card, err := r.current(batch, topic)
		if err != nil {
			return nil, err
		}
		if requireApproval {
			if done, err := r.applied(batch, topic, m.Revision); err != nil {
				return nil, err
			} else if done {
				continue
			}
		}
		stale, err := r.stale(m)
		if err != nil {
			return nil, err
		}
		if len(stale) > 0 {
			return nil, fmt.Errorf("%s의 기준 변경. 이 항목만 다시 prepare/검토: %s", topic, pyList(stale))
		}
		if requireApproval {
			decision, err := r.decision(batch, topic, m.Revision)
			if err != nil {
				return nil, err
			}
			if decision == nil || decision["decision"] != "approved" {
				return nil, fmt.Errorf("사람 승인 없음: %s", topic)
			}
			resp, _ := decision["response"].(map[string]any)
			if h, err := r.userEvidence(resp); err != nil || h != decision["evidence_hash"] {
				return nil, fmt.Errorf("승인 원문 변경: %s", topic)
			}
		}
		for _, rel := range m.fileNames() {
			if _, dup := res.writes[rel]; dup {
				return nil, fmt.Errorf("같은 파일 중복 수정: %s", rel)
			}
			f, err := safe(filepath.Join(path, "after"), rel+".txt")
			if err != nil {
				return nil, err
			}
			res.writes[rel] = readBytes(f)
			res.order = append(res.order, rel)
		}
		cards = append(cards, card)
		res.selected = append(res.selected, selection{topic, m})
	}
	for rel, data := range res.writes {
		pages[rel] = data
	}
	if err := r.checkConstraints(cards, pages); err != nil {
		return nil, err
	}
	revisions := map[string]string{}
	for _, s := range res.selected {
		for rel := range s.m.Files {
			revisions[rel] = s.m.Revision
		}
	}
	registry, err := r.registryAfter(res.writes, revisions)
	if err != nil {
		return nil, err
	}
	res.registry = registry
	tmp, err := os.MkdirTemp("", "wiki-review-preview-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	tmp = resolvePath(tmp, "")
	root := filepath.Join(tmp, "wiki")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	for rel, data := range pages {
		if err := atomicWrite(filepath.Join(root, filepath.FromSlash(rel)), data); err != nil {
			return nil, err
		}
	}
	regPath := filepath.Join(tmp, "registry.json")
	if err := atomicWrite(regPath, registry); err != nil {
		return nil, err
	}
	prefix, _ := filepath.Rel(r.repo, r.wiki)
	view := *r.cfg
	view.WikiRoot, view.ChangesRoot, view.RepoRoot = root, r.changes, r.repo
	view.RegistryOverride, view.ResourcePrefix = regPath, filepath.ToSlash(prefix)
	n, err := BuildIndex(&view)
	if err != nil {
		return nil, err
	}
	var lint bytes.Buffer
	code := Lint(&view, &lint)
	res.output = fmt.Sprintf("index.md: %d pages\n", n) + "\n" + lint.String()
	if code != 0 {
		return nil, errors.New("검토안 검사 실패:\n" + res.output)
	}
	res.index = readBytes(filepath.Join(root, "index.md"))
	return res, nil
}

// registryAfter records cited-line hashes for the pages being applied; other pages keep their records.
func (r *Review) registryAfter(writes map[string][]byte, revisions map[string]string) ([]byte, error) {
	view := *r.cfg
	view.WikiRoot, view.ChangesRoot, view.RegistryOverride = r.wiki, r.changes, ""
	reg, err := view.loadRegistry()
	if err != nil {
		return nil, err
	}
	for rel, data := range writes {
		citations := map[string]string{}
		for _, s := range citedSpans(string(data)) {
			resource, err := r.resource(s.Path)
			if err != nil {
				return nil, err
			}
			local := ""
			if rp := resolvePath(resource, ""); within(rp, r.wiki) {
				l, _ := filepath.Rel(r.wiki, rp)
				local = filepath.ToSlash(l)
			}
			if w, ok := writes[local]; ok && local != "" {
				lines := splitLines(normalizeEOL(w))
				if !(1 <= s.A && s.A <= s.B && s.B <= len(lines)) {
					continue
				}
				citations[s.Key()] = sha(joinLines(lines, s.A, s.B))
				continue
			}
			h, err := r.dependency(fmt.Sprintf("^[%s:%d-%d]", s.Path, s.A, s.B))
			if err != nil {
				continue // lint reports the broken citation
			}
			citations[s.Key()] = h
		}
		rev := revisions[rel]
		var revPtr *string
		if rev != "" {
			revPtr = &rev
		}
		reg.Pages[strings.TrimSuffix(rel, ".md")] = &PageRecord{Revision: revPtr, RecordedAt: now(), Citations: citations}
	}
	return jsonIndent(reg)
}

// target maps a journal key, wiki/<path> or changes/<path>, to a file.
func (r *Review) target(key string) (string, error) {
	head, rel, _ := strings.Cut(key, "/")
	if (head != "wiki" && head != "changes") || rel == "" {
		return "", fmt.Errorf("잘못된 반영 대상: %s", key)
	}
	if head == "wiki" {
		return safe(r.wiki, rel)
	}
	return safe(r.changes, rel)
}

func samePages(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || !bytes.Equal(v, w) {
			return false
		}
	}
	return true
}

// Apply publishes approved versions after re-checking approval, basis and lint, with a recovery journal.
func (r *Review) Apply(batch string, topics []string) (string, error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if exists(r.journal) {
		return "", errors.New("중단된 반영이 있습니다. recover 필요")
	}
	baseline := r.pages()
	res, err := r.preview(batch, topics, true)
	if err != nil {
		return "", err
	}
	if len(res.selected) == 0 {
		// Same suggested message as the first apply, in case that commit was not made yet.
		out := "이미 반영됨. 추가 변경 없음."
		if msg, err := r.commitMessage(batch, topics); err == nil {
			out += "\n\n" + msg
		}
		return out, nil
	}
	if !samePages(baseline, r.pages()) {
		return "", errors.New("검사 중 위키가 바뀌었습니다. 다시 apply하세요.")
	}
	for _, s := range res.selected {
		if _, _, _, err := r.current(batch, s.topic); err != nil {
			return "", err
		}
		if stale, err := r.stale(s.m); err != nil || len(stale) > 0 {
			return "", fmt.Errorf("검사 중 기준 변경: %s", s.topic)
		}
	}
	type write struct {
		key  string
		data []byte
	}
	var writes []write
	for _, rel := range res.order {
		writes = append(writes, write{"wiki/" + rel, res.writes[rel]})
	}
	writes = append(writes, write{"wiki/index.md", res.index}, write{"wiki/" + verificationDir + "/registry.json", res.registry})
	b, err := r.batch(batch)
	if err != nil {
		return "", err
	}
	receipts := readBytes(filepath.Join(b, "applied.jsonl"))
	for _, s := range res.selected {
		decision, err := r.decision(batch, s.topic, s.m.Revision)
		if err != nil {
			return "", err
		}
		resp, _ := decision["response"].(map[string]any)
		// The receipt records approval and application, not fact verification of the page.
		receipt := &omap{m: map[string]any{"topic": s.topic, "revision": s.m.Revision, "at": now(), "files": s.m.Files, "decision_evidence": resp["evidence"]},
			keys: []string{"topic", "revision", "at", "files", "decision_evidence"}}
		receipts = append(receipts, []byte(pyJSON(receipt, false)+"\n")...)
	}
	writes = append(writes, write{"changes/" + batch + "/applied.jsonl", receipts})
	journal := &omap{m: map[string]any{}}
	for _, w := range writes {
		t, err := r.target(w.key)
		if err != nil {
			return "", err
		}
		var before any
		if exists(t) {
			before = string(readBytes(t))
		}
		journal.keys = append(journal.keys, w.key)
		journal.m[w.key] = &omap{keys: []string{"before", "after"}, m: map[string]any{"before": before, "after": string(w.data)}}
	}
	if err := atomicWrite(r.journal, []byte(pyJSONIndent(journal)+"\n")); err != nil {
		return "", err
	}
	for _, w := range writes {
		t, _ := r.target(w.key)
		if err := r.write(t, w.data); err != nil {
			if rerr := r.recoverJournal(); rerr != nil {
				return "", fmt.Errorf("%v; 복구 실패: %v", err, rerr)
			}
			return "", err
		}
	}
	if err := os.Remove(r.journal); err != nil {
		return "", err
	}
	names := make([]string, len(res.selected))
	for i, s := range res.selected {
		names[i] = s.topic
	}
	out := res.output + "\n반영 완료: " + strings.Join(names, ", ")
	if msg, err := r.commitMessage(batch, names); err == nil {
		out += "\n\n" + msg
	}
	return out, nil
}

// commitMessage suggests the wiki commit for applied topics. Its trailers trace each change in the wiki's
// Git history to the decision recorded in the change materials.
func (r *Review) commitMessage(batch string, topics []string) (string, error) {
	var titles, trailers []string
	for _, topic := range topics {
		_, m, card, err := r.current(batch, topic)
		if err != nil {
			return "", err
		}
		decision, err := r.decision(batch, topic, m.Revision)
		if err != nil {
			return "", err
		}
		resp, _ := decision["response"].(map[string]any)
		titles = append(titles, oneLine(cardStr(card, "title")))
		trailers = append(trailers, "Wiki-Change: "+batch+"/"+topic, "Wiki-Revision: "+m.Revision,
			"Approved-By: "+oneLine(str(resp["by"])), "Approval-Evidence: "+oneLine(str(resp["evidence"])))
	}
	return "커밋 메시지:\n" + strings.Join(titles, "; ") + "\n\n" + strings.Join(trailers, "\n"), nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func (r *Review) recoverJournal() error {
	raw, err := readJSON(r.journal)
	if err != nil {
		return err
	}
	journal, ok := raw.(*omap)
	if !ok {
		return errors.New("journal 형식 오류")
	}
	for _, key := range journal.keys {
		t, err := r.target(key)
		if err != nil {
			return err
		}
		current := readBytes(t)
		match := false
		for _, v := range obj(journal.m[key]) {
			if s, isStr := v.(string); isStr && current != nil && string(current) == s {
				match = true
			}
			if v == nil && current == nil {
				match = true
			}
		}
		if !match {
			return fmt.Errorf("복구 대상에 다른 변경이 생김. 수동 대조 필요: %s", key)
		}
	}
	for _, key := range journal.keys {
		t, _ := r.target(key)
		var data []byte
		if s, ok := obj(journal.m[key])["before"].(string); ok {
			data = []byte(s)
		}
		if err := atomicWrite(t, data); err != nil {
			return err
		}
	}
	return os.Remove(r.journal)
}

// Recover rolls back an interrupted apply.
func (r *Review) Recover() (string, error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if !exists(r.journal) {
		return "복구할 반영 없음.", nil
	}
	if err := r.recoverJournal(); err != nil {
		return "", err
	}
	return "중단된 반영을 변경 전 상태로 복구했습니다.", nil
}

// TopicStatus is one row of `review status`.
type TopicStatus struct {
	Topic        string   `json:"topic"`
	Title        string   `json:"title"`
	Revision     string   `json:"revision"`
	State        string   `json:"state"`
	ChangedBasis []string `json:"changed_basis"`
}

func (r *Review) Status(batch string) ([]TopicStatus, error) {
	b, err := r.batch(batch)
	if err != nil {
		return nil, err
	}
	dirs, _ := filepath.Glob(filepath.Join(b, "topics", "*"))
	sort.Strings(dirs)
	out := []TopicStatus{}
	for _, d := range dirs {
		name := filepath.Base(d)
		_, m, card, err := r.current(batch, name)
		if err != nil {
			return nil, err
		}
		decision, err := r.decision(batch, name, m.Revision)
		if err != nil {
			return nil, err
		}
		done, err := r.applied(batch, name, m.Revision)
		if err != nil {
			return nil, err
		}
		state := "pending"
		changed := []string{}
		if done {
			state = "applied"
		} else {
			if decision != nil {
				state = str(decision["decision"])
			}
			if changed, err = r.stale(m); err != nil {
				return nil, err
			}
			if changed == nil {
				changed = []string{}
			}
		}
		out = append(out, TopicStatus{name, cardStr(card, "title"), m.Revision, state, changed})
	}
	return out, nil
}

// StatusAll lists every batch.
func (r *Review) StatusAll() (map[string][]TopicStatus, error) {
	out := map[string][]TopicStatus{}
	entries, _ := os.ReadDir(r.root)
	for _, e := range entries {
		if e.IsDir() && idRE.MatchString(e.Name()) {
			s, err := r.Status(e.Name())
			if err != nil {
				return nil, err
			}
			out[e.Name()] = s
		}
	}
	return out, nil
}

func sortedAnyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sameSet(a, b map[string]bool) bool {
	for k := range a {
		if !b[k] {
			return false
		}
	}
	for k := range b {
		if !a[k] {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
