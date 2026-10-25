package wiki

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	typeFolders = map[string]string{"concept": "concepts", "entity": "entities", "policy": "policies"}
	folderOrder = []string{"concepts", "entities", "policies"}
	// RelationTypes are the typed relations a page may declare.
	RelationTypes = map[string]bool{"part_of": true, "depends_on": true, "implements": true, "applies_to": true,
		"supersedes": true, "contradicts": true, "related_to": true}
	// impactTypes: when the target changes, the page that declares the relation needs review too.
	impactTypes = map[string]bool{"depends_on": true, "implements": true, "applies_to": true}

	wikilinkRE     = regexp.MustCompile(`\[\[([^\]|#]+)(?:#[^\]|]*)?(?:\|[^\]]*)?\]\]`)
	citationRE     = regexp.MustCompile(`\^\[(/[^\]:]+?):(\d+)(?:-(\d+))?\]`)
	citationFullRE = regexp.MustCompile(`^\^\[(/[^\]:]+?):(\d+)(?:-(\d+))?\]$`)
)

// verificationDir holds citation hashes: not knowledge, but shared by the team, so it lives in the wiki repository.
const verificationDir = ".verification"

// Page is a wiki page under one of the type folders.
type Page struct {
	Path string
	Slug string
}

func (c *Config) pages() []Page {
	var out []Page
	_ = filepath.WalkDir(c.WikiRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, _ := filepath.Rel(c.WikiRoot, p)
		rel = filepath.ToSlash(rel)
		top, _, _ := strings.Cut(rel, "/")
		if d.Name() == "index.md" || !contains(folderOrder, top) {
			return nil
		}
		out = append(out, Page{Path: p, Slug: strings.TrimSuffix(rel, ".md")})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return lessParts(out[i].Slug+".md", out[j].Slug+".md") })
	return out
}

// lessParts orders paths by their components, as Python sorts Path objects.
func lessParts(a, b string) bool {
	pa, pb := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return len(pa) < len(pb)
}

func readText(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(string(b), "\ufeff"), nil
}

// resolver resolves wikilinks in the same order as Obsidian:
// exact path, path without `wiki/`, unique basename, unique path suffix.
type resolver struct {
	slugs  map[string]bool
	byBase map[string][]string
}

func newResolver(slugs []string) *resolver {
	r := &resolver{slugs: map[string]bool{"index": true, "SCHEMA": true}, byBase: map[string][]string{}}
	for _, s := range slugs {
		r.slugs[s] = true
	}
	for s := range r.slugs {
		base := s[strings.LastIndex(s, "/")+1:]
		r.byBase[base] = append(r.byBase[base], s)
	}
	return r
}

func (r *resolver) resolve(target string) string {
	t := strings.TrimSuffix(strings.TrimSpace(target), ".md")
	if r.slugs[t] {
		return t
	}
	if strings.HasPrefix(t, "wiki/") && r.slugs[t[5:]] {
		return t[5:]
	}
	var c []string
	if !strings.Contains(t, "/") {
		c = r.byBase[t]
	} else {
		for s := range r.slugs {
			if strings.HasSuffix(s, "/"+t) || s == t {
				c = append(c, s)
			}
		}
	}
	if len(c) == 1 {
		return c[0]
	}
	return ""
}

func slugsOf(pages []Page) []string {
	out := make([]string, len(pages))
	for i, p := range pages {
		out[i] = p.Slug
	}
	return out
}

// resolveResource maps `/repo/path` or `/alias/file` to a file.
func (c *Config) resolveResource(path string) (string, error) {
	rel := strings.TrimLeft(path, "/")
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == ".." {
			return "", fmt.Errorf("Source path escapes its root: %s", path)
		}
	}
	head, tail, _ := strings.Cut(rel, "/")
	prefix := strings.TrimRight(c.ResourcePrefix, "/")
	var root, relative string
	switch {
	case c.ExtraRoots[head] != "":
		root, relative = c.ExtraRoots[head], tail
	case prefix != "" && strings.HasPrefix(rel, prefix+"/"):
		root, relative = c.WikiRoot, rel[len(prefix)+1:]
	default:
		root, relative = c.RepoRoot, rel
	}
	target := resolvePath(filepath.FromSlash(relative), root)
	if !within(target, resolvePath(root, "")) {
		return "", fmt.Errorf("Source path escapes its root: %s", path)
	}
	return target, nil
}

func (c *Config) isSession(path string) bool {
	head, _, _ := strings.Cut(strings.TrimLeft(path, "/"), "/")
	if c.ExtraRoots[head] == "" {
		return false
	}
	if s, ok := c.source(head); ok && sessionKinds[s.Kind] {
		return true
	}
	return head == "claude-sessions" || head == "codex-sessions"
}

// Span is one line citation.
type Span struct {
	Path string
	A, B int
}

func (s Span) Key() string { return fmt.Sprintf("%s:%d-%d", s.Path, s.A, s.B) }

func parseKey(key string) Span {
	i := strings.LastIndex(key, ":")
	a, b, _ := strings.Cut(key[i+1:], "-")
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	return Span{key[:i], x, y}
}

// citedSpans collects line citations in the body and frontmatter (relation evidence).
func citedSpans(text string) []Span {
	var out []Span
	for _, m := range citationRE.FindAllStringSubmatch(text, -1) {
		a, _ := strconv.Atoi(m[2])
		b := a
		if m[3] != "" {
			b, _ = strconv.Atoi(m[3])
		}
		out = append(out, Span{m[1], a, b})
	}
	return out
}

// splitLines splits on \n, \r\n and \r, keeping the line endings, like Python bytes.splitlines(keepends=True).
func splitLines(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			out = append(out, data[start:i+1])
			start = i + 1
		case '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
			out = append(out, data[start:i+1])
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, data[start:])
	}
	return out
}

func sha(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func joinLines(lines [][]byte, a, b int) []byte {
	var out []byte
	for _, l := range lines[a-1 : b] {
		out = append(out, l...)
	}
	return out
}

func (c *Config) sourceLines(path string) [][]byte {
	f, err := c.resolveResource(path)
	if err != nil || !isFile(f) {
		return nil
	}
	data, err := os.ReadFile(f)
	if err != nil {
		return nil
	}
	lines := splitLines(data)
	if lines == nil {
		lines = [][]byte{}
	}
	return lines
}

// spanHash hashes the cited lines; "" when the file is missing or the range is out of bounds.
func (c *Config) spanHash(s Span) string {
	lines := c.sourceLines(s.Path)
	if lines == nil || !(1 <= s.A && s.A <= s.B && s.B <= len(lines)) {
		return ""
	}
	return sha(joinLines(lines, s.A, s.B))
}

// relocate finds the recorded content at other lines of the same length, nearest to the original.
func (c *Config) relocate(s Span, recorded string) (Span, bool) {
	lines := c.sourceLines(s.Path)
	if lines == nil {
		return Span{}, false
	}
	size := s.B - s.A + 1
	best := -1
	for i := 1; i <= len(lines)-size+1; i++ {
		if i == s.A || sha(joinLines(lines, i, i+size-1)) != recorded {
			continue
		}
		if best < 0 || abs(i-s.A) < abs(best-s.A) {
			best = i
		}
	}
	if best < 0 {
		return Span{}, false
	}
	return Span{s.Path, best, best + size - 1}, true
}

type keyHash struct{ key, hash string }

// pageSpans returns each distinct citation in order of first appearance with its current hash.
func (c *Config) pageSpans(text string) []keyHash {
	seen := map[string]bool{}
	var out []keyHash
	for _, s := range citedSpans(text) {
		if seen[s.Key()] {
			continue
		}
		seen[s.Key()] = true
		out = append(out, keyHash{s.Key(), c.spanHash(s)})
	}
	return out
}

// Registry records the hash of each cited range at adoption time.
type Registry struct {
	Version int                    `json:"version"`
	Pages   map[string]*PageRecord `json:"pages"`
}

// PageRecord is one page's recorded citations.
type PageRecord struct {
	Revision   *string           `json:"revision"`
	RecordedAt string            `json:"recorded_at"`
	Citations  map[string]string `json:"citations"`
}

func registryPath(wiki string) string {
	return filepath.Join(wiki, verificationDir, "registry.json")
}

// loadRegistry reads the wiki's registry, or the legacy workspace registry until it is rewritten.
func (c *Config) loadRegistry() (*Registry, error) {
	p := c.RegistryOverride
	if p == "" {
		p = registryPath(c.WikiRoot)
		legacy := filepath.Join(c.StateRoot, "registry.json")
		if !exists(p) && exists(legacy) {
			p = legacy
		}
	}
	reg := &Registry{Version: 1, Pages: map[string]*PageRecord{}}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return reg, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(string(data), "\ufeff")), reg); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if reg.Pages == nil {
		reg.Pages = map[string]*PageRecord{}
	}
	return reg, nil
}

func (r *Registry) citations(slug string) map[string]string {
	if p := r.Pages[slug]; p != nil && p.Citations != nil {
		return p.Citations
	}
	return map[string]string{}
}

type keyState struct{ key, state string }

// freshness returns each citation's state: fresh, moved, changed, expired, missing or unrecorded.
func (c *Config) freshness(slug, text string, reg *Registry) []keyState {
	recorded := reg.citations(slug)
	var out []keyState
	for _, kh := range c.pageSpans(text) {
		old, has := recorded[kh.key]
		state := ""
		switch {
		case has && kh.hash != old && c.moved(parseKey(kh.key), old):
			state = "moved"
		case kh.hash == "":
			if c.isSession(parseKey(kh.key).Path) {
				state = "expired"
			} else {
				state = "missing"
			}
		case !has:
			state = "unrecorded"
		case old == kh.hash:
			state = "fresh"
		default:
			state = "changed"
		}
		out = append(out, keyState{kh.key, state})
	}
	return out
}

func (c *Config) moved(s Span, recorded string) bool {
	_, ok := c.relocate(s, recorded)
	return ok
}

// movedTo is the new `path:a-b` of a moved citation.
func (c *Config) movedTo(key, slug string, reg *Registry) string {
	recorded, ok := reg.citations(slug)[key]
	if !ok {
		return ""
	}
	if s, ok := c.relocate(parseKey(key), recorded); ok {
		return s.Key()
	}
	return ""
}

// Impact names the relation through which a page is affected.
type Impact struct{ Kind, Target string }

// dependents follows impact relations backwards from roots: page → (relation, page it relies on).
func dependents(pages map[string]Frontmatter, roots map[string]bool) map[string]Impact {
	slugs := make([]string, 0, len(pages))
	for s := range pages {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	res := newResolver(slugs)
	incoming := map[string][]Impact{}
	for _, slug := range slugs {
		for _, r := range relations(pages[slug]) {
			if t := res.resolve(r.Target); t != "" && impactTypes[r.Type] {
				incoming[t] = append(incoming[t], Impact{r.Type, slug})
			}
		}
	}
	out := map[string]Impact{}
	queue := make([]string, 0, len(roots))
	for r := range roots {
		queue = append(queue, r)
	}
	sort.Strings(queue)
	for len(queue) > 0 {
		page := queue[0]
		queue = queue[1:]
		for _, in := range incoming[page] {
			source := in.Target
			if !roots[source] {
				if _, seen := out[source]; !seen {
					out[source] = Impact{in.Kind, page}
					queue = append(queue, source)
				}
			}
		}
	}
	return out
}

func (c *Config) frontmatters() map[string]Frontmatter {
	out := map[string]Frontmatter{}
	for _, p := range c.pages() {
		text, _ := readText(p.Path)
		fm, _ := ParseFrontmatter(text)
		if fm == nil {
			fm = Frontmatter{}
		}
		out[p.Slug] = fm
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func fmtAny(v any) string { return fmt.Sprint(v) }
