package wiki

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	placeholderRE = regexp.MustCompile(`(?i)(TODO|TBD|원문을 참조|pending review|작성 예정|\(추후\))`)
	requiredKeys  = []string{"type", "title", "description", "owner"}
	// Adoption and history belong to the default branch and Git.
	gitManaged = []string{"status", "adopted", "verified", "verification_history", "generated", "supersedes", "contested"}
	// Change-process files belong in workspace/wiki-changes; finding one in the wiki means undecided
	// material could be committed. Draft and snapshot copies end in .md.txt.
	changeMaterials = map[string]bool{"card.json": true, "manifest.json": true, "decisions.jsonl": true, "applied.jsonl": true,
		"inventory.json": true, "response.json": true, "apply-journal.json": true}
)

// Lint checks structure deterministically and returns 1 when there are errors.
//
//	error   : change materials (cards, manifests, decisions, receipts, inventories, .md.txt drafts) in the wiki,
//	          missing or broken frontmatter, missing type·title·description·owner, wrong type or folder,
//	          wikilinks or related targets that do not exist, unknown relation types or evidence,
//	          missing sources, citations to missing files or lines, citations of change materials
//	warning : registered source folder missing, changed or moved cited lines, expired sessions,
//	          orphan pages, placeholders, duplicate titles, status/history fields, stale index.md
//	info    : citations without a recorded hash, contradicts relations, pages over 200 lines
//
// With c.WikiOnly (lint --wiki-only) the wiki folder is checked by itself, as in the wiki repository's CI:
// cited files and registry hashes cannot be checked there, so they are reported as one info.
func Lint(c *Config, out io.Writer) int {
	if !isDir(c.WikiRoot) {
		fmt.Fprintf(out, "[ERROR] Wiki directory not found: %s; run company-wiki setup first\n", c.WikiRoot)
		return 1
	}
	if c.WikiOnly && !isFile(filepath.Join(c.WikiRoot, "index.md")) && !hasTypeFolder(c.WikiRoot) {
		fmt.Fprintf(out, "[ERROR] 위키 폴더가 아님: %s (index.md·concepts/·entities/·policies/ 없음)\n", c.WikiRoot)
		return 1
	}
	var errs, warns, infos []string
	for _, rel := range strayChangeMaterials(c.WikiRoot) {
		errs = append(errs, fmt.Sprintf("%s: 변경 절차 자료는 위키에 두지 않는다. workspace/wiki-changes에 둔다", rel))
	}
	// --wiki-only: distinct citations per page, and their source files.
	unchecked, uncheckedFiles := map[string]bool{}, map[string]bool{}
	pages := c.pages()
	res := newResolver(slugsOf(pages))
	reg, err := c.loadRegistry()
	if err != nil {
		fmt.Fprintf(out, "[ERROR] %v\n", err)
		return 1
	}
	inbound := map[string]int{}
	titles := map[string][]string{}
	var titleOrder []string
	lineCounts := map[string]int{}
	for _, s := range c.Sources {
		if root, ok := c.ExtraRoots[s.Alias]; ok && !exists(root) {
			warns = append(warns, fmt.Sprintf("wiki.toml: 원천 `%s` 경로 없음 `%s`", s.Alias, root))
		}
	}
	changesRoot := resolvePath(c.ChangesRoot, "")

	for _, p := range pages {
		rel := p.Slug + ".md"
		text, err := readText(p.Path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		fm, body := ParseFrontmatter(text)
		if fm == nil {
			errs = append(errs, rel+": frontmatter 없음")
			continue
		}
		for _, k := range requiredKeys {
			if !truthy(fm[k]) {
				errs = append(errs, fmt.Sprintf("%s: 필수 키 누락 `%s`", rel, k))
			}
		}
		t, isStr := fm["type"].(string)
		folder, known := typeFolders[t]
		if !isStr || !known {
			errs = append(errs, fmt.Sprintf("%s: 알 수 없는 type `%s` (concept/entity/policy)", rel, str(fm["type"])))
		} else if top, _, _ := strings.Cut(rel, "/"); top != folder {
			errs = append(errs, fmt.Sprintf("%s: type `%s` 는 %s/ 아래에 둔다", rel, t, folder))
		}
		var extra []string
		for _, k := range gitManaged {
			if _, ok := fm[k]; ok {
				extra = append(extra, k)
			}
		}
		if len(extra) > 0 {
			warns = append(warns, fmt.Sprintf("%s: %s 불필요. 채택은 기본 브랜치 반영, 이력은 Git이 맡는다", rel, strings.Join(extra, ", ")))
		}
		sources, isList := fm["sources"].([]any)
		hasSource := false
		for _, x := range sources {
			if strings.TrimSpace(str(x)) != "" {
				hasSource = true
			}
		}
		if !isList || !hasSource {
			errs = append(errs, rel+": sources 없음. 판단의 근거(원문 줄 인용·결정 링크)가 필요하다")
		}
		title := str(fm["title"])
		if _, ok := fm["title"]; !ok {
			title = "None"
		}
		if _, seen := titles[title]; !seen {
			titleOrder = append(titleOrder, title)
		}
		titles[title] = append(titles[title], rel)

		for _, m := range wikilinkRE.FindAllStringSubmatch(body, -1) {
			target := strings.TrimSpace(m[1])
			if hit := res.resolve(target); hit == "" {
				errs = append(errs, fmt.Sprintf("%s: 없는 페이지로의 위키링크 `[[%s]]`", rel, target))
			} else {
				inbound[hit]++
			}
		}
		for i, r := range relations(fm) {
			label := fmt.Sprintf("%s: related[%d]", rel, i+1)
			if hit := res.resolve(r.Target); hit == "" || hit == "index" {
				errs = append(errs, fmt.Sprintf("%s 없는 대상 `%s`", label, r.Target))
			} else {
				inbound[hit]++
			}
			if r.HasType && !RelationTypes[r.Type] {
				errs = append(errs, fmt.Sprintf("%s 알 수 없는 관계 `%s` (%s)", label, r.Type, strings.Join(sortedKeys(RelationTypes), "/")))
			}
			if r.HasEvidence && !citationFullRE.MatchString(r.Evidence) {
				errs = append(errs, label+" evidence는 `^[/경로:줄-줄]` 형식")
			}
			if r.Type == "contradicts" {
				infos = append(infos, fmt.Sprintf("%s contradicts → %s", label, r.Target))
			}
		}

		for _, s := range citedSpans(text) {
			if c.WikiOnly {
				if s.A < 1 || s.B < s.A {
					errs = append(errs, fmt.Sprintf("%s: 인용 줄 범위 오류 `%s:%d-%d`", rel, s.Path, s.A, s.B))
				}
				unchecked[rel+" "+s.Key()] = true
				uncheckedFiles[s.Path] = true
				continue
			}
			f, err := c.resolveResource(s.Path)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			if within(f, changesRoot) {
				errs = append(errs, fmt.Sprintf("%s: 변경 절차 자료(검토 사본 등)를 정식 지식 근거로 인용함 `%s`", rel, s.Path))
				continue
			}
			if !exists(f) {
				if c.isSession(s.Path) {
					warns = append(warns, fmt.Sprintf("%s: 세션 원문 만료 `%s`. 근거를 다시 확인할 수 없음", rel, s.Path))
				} else {
					errs = append(errs, fmt.Sprintf("%s: 인용 파일 없음 `%s`", rel, s.Path))
				}
				continue
			}
			n, ok := lineCounts[f]
			if !ok {
				data, _ := os.ReadFile(f)
				n = len(splitLines(data))
				lineCounts[f] = n
			}
			if s.A < 1 || s.B < s.A || s.B > n {
				errs = append(errs, fmt.Sprintf("%s: 인용 줄 범위 오류 `%s:%d-%d` (파일은 %d줄)", rel, s.Path, s.A, s.B, n))
			}
		}
		var states []keyState
		if !c.WikiOnly {
			states = c.freshness(p.Slug, text, reg)
		}
		for _, ks := range states {
			switch ks.state {
			case "moved":
				warns = append(warns, fmt.Sprintf("%s: 인용 줄 이동 `%s` → `%s`. 내용은 같으니 인용 번호를 고치고 company-wiki registry record", rel, ks.key, c.movedTo(ks.key, p.Slug, reg)))
			case "changed":
				warns = append(warns, fmt.Sprintf("%s: 원문 변경 `%s`. 이 인용에 기댄 문장·관계를 재검토", rel, ks.key))
			case "unrecorded":
				infos = append(infos, fmt.Sprintf("%s: 해시 기록 없는 인용 `%s`. 커밋 전 company-wiki registry record", rel, ks.key))
			}
		}
		if placeholderRE.MatchString(body) {
			warns = append(warns, rel+": 플레이스홀더 문구 발견")
		}
		if n := strings.Count(text, "\n"); n > 200 {
			infos = append(infos, fmt.Sprintf("%s: %d줄, 200줄 초과. 분할 검토", rel, n))
		}
	}

	if len(unchecked) > 0 {
		infos = append(infos, fmt.Sprintf("인용 %d개(원문 %d개)의 파일·줄 범위·해시는 CI에서 확인 불가 (--wiki-only). 원문이 있는 ~/company-wiki에서 company-wiki lint·registry로 확인", len(unchecked), len(uncheckedFiles)))
	}
	for _, p := range pages {
		if inbound[p.Slug] == 0 {
			warns = append(warns, p.Slug+".md: 고아 페이지 (들어오는 링크·관계 0)")
		}
	}
	for _, title := range titleOrder {
		if rels := titles[title]; len(rels) > 1 {
			warns = append(warns, fmt.Sprintf("제목 중복 `%s`: %s", title, strings.Join(rels, ", ")))
		}
	}

	idx := c.WikiRoot + string(os.PathSeparator) + "index.md"
	if text, err := readText(idx); err == nil {
		listed := map[string]bool{}
		for _, m := range wikilinkRE.FindAllStringSubmatch(text, -1) {
			listed[res.resolve(m[1])] = true
		}
		var missing []string
		for _, p := range pages {
			if !listed[p.Slug] {
				missing = append(missing, p.Slug)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			shown := missing
			if len(shown) > 5 {
				shown = shown[:5]
			}
			warns = append(warns, fmt.Sprintf("index.md 에 없는 페이지 %d개 (company-wiki index 실행): %s…", len(missing), strings.Join(shown, ", ")))
		}
	} else {
		warns = append(warns, "index.md 없음 (company-wiki index 실행)")
	}

	for _, group := range []struct {
		label string
		items []string
	}{{"ERROR", errs}, {"WARNING", warns}, {"INFO", infos}} {
		for _, it := range group.items {
			fmt.Fprintf(out, "[%s] %s\n", group.label, it)
		}
	}
	fmt.Fprintf(out, "\n%d pages · %d errors · %d warnings · %d infos\n", len(pages), len(errs), len(warns), len(infos))
	if len(errs) > 0 {
		return 1
	}
	return 0
}

// WikiOnlyConfig checks the wiki folder alone: WIKI_ROOT, or dir. It needs no wiki.toml and registers no sources.
func WikiOnlyConfig(env Env, dir string) *Config {
	root := resolvePath(dir, "")
	if v := env("WIKI_ROOT"); v != "" {
		root = resolvePath(expandUser(v), root)
	}
	// Citations are not resolved, so the configuration root and change materials are never read.
	return &Config{Root: root, RepoRoot: root, WikiRoot: root, ExtraRoots: map[string]string{},
		RegistryOverride: registryPath(root), WikiOnly: true}
}

// strayChangeMaterials lists change-process files anywhere in the wiki except .git.
func strayChangeMaterials(wiki string) []string {
	var out []string
	_ = filepath.WalkDir(wiki, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if changeMaterials[d.Name()] || strings.HasSuffix(d.Name(), ".md.txt") {
			rel, _ := filepath.Rel(wiki, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

func hasTypeFolder(root string) bool {
	for _, f := range folderOrder {
		if isDir(filepath.Join(root, f)) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
