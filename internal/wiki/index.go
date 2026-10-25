package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var sections = map[string]string{
	"concepts": "개념 (concepts) — 무엇을 의미하는가",
	"entities": "대상 (entities) — 무엇이 존재하는가",
	"policies": "정책 (policies) — 어떻게 판단하고 행동하는가",
}

// BuildIndex writes index.md from frontmatter. It is the entry point agents and people read first:
// questions → pages, then per-folder descriptions, owners, aliases and relations, then the relation table.
func BuildIndex(c *Config) (int, error) {
	paths := c.pages()
	res := newResolver(slugsOf(paths))
	type entry struct {
		slug string
		fm   Frontmatter
	}
	var pages []entry
	for _, p := range paths {
		text, err := readText(p.Path)
		if err != nil {
			return 0, err
		}
		if fm, _ := ParseFrontmatter(text); fm != nil {
			pages = append(pages, entry{p.Slug, fm})
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].slug < pages[j].slug })
	titles := map[string]string{}
	for _, p := range pages {
		if _, ok := p.fm["title"]; ok {
			titles[p.slug] = str(p.fm["title"])
		} else {
			titles[p.slug] = p.slug
		}
	}
	link := func(slug string) string {
		t, ok := titles[slug]
		if !ok {
			t = slug
		}
		return "[[" + slug + "|" + t + "]]"
	}
	target := func(r Relation) string {
		if t := res.resolve(r.Target); t != "" {
			return t
		}
		return r.Target
	}

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	out := []string{
		"---",
		`wiki_format: "2"`,
		fmt.Sprintf("generated: { by: process:company-wiki-index, at: %s }", now),
		"---",
		"",
		"# " + c.Title,
		"",
		fmt.Sprintf("조직이 현재 채택한 지식 %d개. 이 파일은 `company-wiki index`가 생성한다. "+
			"질문으로 찾고, 없으면 폴더별 목록에서 별칭과 관계를 따라간다. 규칙은 [[SCHEMA]].", len(pages)),
		"",
	}

	type question struct{ q, slug string }
	var questions []question
	for _, p := range pages {
		for _, q := range asList(p.fm["answers"]) {
			questions = append(questions, question{q, p.slug})
		}
	}
	sort.Slice(questions, func(i, j int) bool {
		if questions[i].q != questions[j].q {
			return questions[i].q < questions[j].q
		}
		return questions[i].slug < questions[j].slug
	})
	if len(questions) > 0 {
		out = append(out, "## 질문으로 찾기", "")
		for _, q := range questions {
			out = append(out, fmt.Sprintf("- %s → %s", q.q, link(q.slug)))
		}
		out = append(out, "")
	}

	groups := map[string]map[string][]entry{}
	for _, p := range pages {
		parts := strings.Split(p.slug, "/")
		sub := strings.Join(parts[1:len(parts)-1], "/")
		if groups[parts[0]] == nil {
			groups[parts[0]] = map[string][]entry{}
		}
		groups[parts[0]][sub] = append(groups[parts[0]][sub], p)
	}
	for _, folder := range folderOrder {
		g, ok := groups[folder]
		if !ok {
			continue
		}
		total := 0
		subs := make([]string, 0, len(g))
		for sub, list := range g {
			total += len(list)
			subs = append(subs, sub)
		}
		sort.Strings(subs)
		out = append(out, fmt.Sprintf("## %s · %d", sections[folder], total), "")
		for _, sub := range subs {
			if sub != "" {
				out = append(out, "### "+sub, "")
			}
			for _, p := range g[sub] {
				owner := ""
				if truthy(p.fm["owner"]) {
					owner = " · 담당 " + str(p.fm["owner"])
				}
				out = append(out, fmt.Sprintf("- %s — %s%s", link(p.slug), p.fm.str("description"), owner))
				if aliases := asList(p.fm["aliases"]); len(aliases) > 0 {
					out = append(out, "  - 별칭: "+strings.Join(aliases, ", "))
				}
				var rels []string
				for _, r := range relations(p.fm) {
					prefix := ""
					if truthy(r.Type) {
						prefix = r.Type + " → "
					}
					rels = append(rels, prefix+link(target(r)))
				}
				if len(rels) > 0 {
					out = append(out, "  - 관계: "+strings.Join(rels, " · "))
				}
			}
			out = append(out, "")
		}
	}

	var typed []string
	for _, p := range pages {
		for _, r := range relations(p.fm) {
			if !truthy(r.Type) {
				continue
			}
			evidence := ""
			if truthy(r.Evidence) {
				evidence = "`" + r.Evidence + "`"
			}
			// Table cells cannot hold the `|` of an aliased wikilink.
			typed = append(typed, fmt.Sprintf("| [[%s]] | %s | [[%s]] | %s |", p.slug, r.Type, target(r), evidence))
		}
	}
	if len(typed) > 0 {
		out = append(out, "## 관계 목록", "", "| 출발 | 관계 | 대상 | 근거 |", "|---|---|---|---|")
		out = append(out, typed...)
		out = append(out, "")
	}
	if err := os.MkdirAll(c.WikiRoot, 0o755); err != nil {
		return 0, err
	}
	return len(pages), os.WriteFile(filepath.Join(c.WikiRoot, "index.md"), []byte(strings.Join(out, "\n")), 0o644)
}
