package wiki

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Kind describes a source kind an organization already runs on. Personal web
// clippings are not organizational sources.
type Kind struct {
	Label   string
	Alias   string
	Include []string
	Exclude []string
}

var kindOrder = []string{"documents", "code", "slack", "jira", "notion", "confluence", "claude", "codex"}

// Kinds lists the source kinds setup can register.
var Kinds = map[string]Kind{
	"documents":  {"문서 폴더 (Markdown·MDX)", "documents", []string{"**/*.md", "**/*.mdx"}, []string{"**/.env*", "**/node_modules/**", "**/.git/**"}},
	"code":       {"코드 저장소", "code", nil, []string{"**/.env*", "**/node_modules/**", "**/.git/**"}},
	"slack":      {"Slack 내보내기 폴더", "slack", []string{"**/*.md", "**/*.txt"}, nil},
	"jira":       {"Jira 내보내기 폴더", "jira", []string{"**/*.md", "**/*.txt"}, nil},
	"notion":     {"Notion 내보내기 폴더", "notion", []string{"**/*.md", "**/*.txt"}, nil},
	"confluence": {"Confluence 내보내기 폴더", "confluence", []string{"**/*.md", "**/*.txt"}, nil},
	"claude":     {"Claude Code 세션 (~/.claude/projects/<프로젝트>)", "claude-sessions", nil, nil},
	"codex":      {"Codex 세션 (~/.codex/sessions)", "codex-sessions", nil, nil},
}

var sessionKinds = map[string]bool{"claude": true, "codex": true}

func kindNames(sorted bool) []string {
	out := append([]string(nil), kindOrder...)
	if sorted {
		// Error messages list kinds alphabetically.
		sort.Strings(out)
	}
	return out
}

// KindsText is the kind list setup prints.
func KindsText() string {
	var b strings.Builder
	for i, k := range kindOrder {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "  %-11s %s", k, Kinds[k].Label)
	}
	return b.String()
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// defaultPath suggests a location for session sources; other kinds have no safe default.
func defaultPath(kind, project string) string {
	switch kind {
	case "claude":
		// Claude Code stores a project's sessions under its path with separators replaced by '-'.
		return "~/.claude/projects/" + nonAlnum.ReplaceAllString(resolvePath(project, ""), "-")
	case "codex":
		return "~/.codex/sessions"
	}
	return ""
}

type sourceSpec struct{ kind, alias, path string }

// parseSpec reads `kind=path` or `kind:alias=path`.
func parseSpec(spec string) (sourceSpec, error) {
	head, path, ok := strings.Cut(spec, "=")
	path = strings.TrimSpace(path)
	if !ok || path == "" {
		return sourceSpec{}, fmt.Errorf("--source must be kind=path or kind:alias=path: %s", spec)
	}
	kind, alias, _ := strings.Cut(head, ":")
	if _, known := Kinds[kind]; !known {
		return sourceSpec{}, fmt.Errorf("Unsupported source kind: %s (%s)", kind, strings.Join(kindOrder, ", "))
	}
	if alias != "" && !aliasRE.MatchString(alias) {
		return sourceSpec{}, fmt.Errorf("Alias must use letters, digits, - or _: %s", alias)
	}
	return sourceSpec{kind, alias, path}, nil
}

var aliasSlug = regexp.MustCompile(`[^a-z0-9]+`)

func chooseAlias(kind, path string, taken map[string]bool) string {
	base := Kinds[kind].Alias
	if !taken[base] {
		return base
	}
	name := strings.Trim(aliasSlug.ReplaceAllString(strings.ToLower(filepath.Base(expandUser(path))), "-"), "-")
	if name == "" {
		name = "source"
	}
	alias := kind + "-" + name
	for n := 2; taken[alias]; n++ {
		alias = fmt.Sprintf("%s-%s-%d", kind, name, n)
	}
	return alias
}

func tomlList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = `"` + v + `"`
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func sourceBlock(alias, kind, path string) (string, error) {
	if strings.ContainsAny(path, "'\n") {
		return "", fmt.Errorf("Path cannot contain quotes or newlines: %s", path)
	}
	k := Kinds[kind]
	// A literal string keeps Windows backslashes.
	lines := []string{"[sources." + alias + "]", `kind = "` + kind + `"`, "path = '" + path + "'"}
	if len(k.Include) > 0 {
		lines = append(lines, "include = "+tomlList(k.Include))
	}
	if len(k.Exclude) > 0 {
		lines = append(lines, "exclude = "+tomlList(k.Exclude))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

const wikiBlock = `# Company Wiki workspace. Paths are relative to this file, or absolute / ~/ paths.
[wiki]
title = "Company Wiki"
# Adopted knowledge only: concepts/, entities/, policies/. Citation hashes live in <path>/.verification/.
path = "llm-wiki"
# Workspace outside the wiki: drafts, reviews, decisions.
state = ".llm-wiki"
reviewer = "human:owner"
language = "ko"
`
