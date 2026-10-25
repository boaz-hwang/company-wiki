package wiki

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Env reads environment variables; tests pass their own.
type Env func(string) string

var (
	aliasRE    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	reviewerRE = regexp.MustCompile(`^human:[a-zA-Z0-9_-]+$`)
)

// Source is one registered source in wiki.toml.
type Source struct {
	Alias, Kind, Path string
	Enabled           bool
}

// Config is the resolved workspace configuration.
type Config struct {
	Path      string // wiki.toml
	Exists    bool
	Workspace string
	RepoRoot  string
	WikiRoot  string
	// Rules, registry and reviews are not knowledge; drafts and records live outside the wiki folder.
	StateRoot string
	Title     string
	Reviewer  string
	Sources   []Source
	// ExtraRoots maps an enabled source alias to its folder.
	ExtraRoots      map[string]string
	RequiredSources map[string]bool
	RequiredKinds   map[string]bool
	// RegistryOverride and ResourcePrefix let review preview check a temporary copy of the wiki.
	RegistryOverride string
	ResourcePrefix   string
}

// FindConfig returns WIKI_CONFIG, or the nearest wiki.toml from dir upward, or dir/wiki.toml.
func FindConfig(env Env, dir string) (string, bool, error) {
	if v := env("WIKI_CONFIG"); v != "" {
		p := resolvePath(expandUser(v), dir)
		return p, true, nil
	}
	for d := resolvePath(dir, ""); ; d = filepath.Dir(d) {
		candidate := filepath.Join(d, "wiki.toml")
		if isFile(candidate) {
			return candidate, false, nil
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return filepath.Join(resolvePath(dir, ""), "wiki.toml"), false, nil
}

// LoadConfig reads the configuration for a command run in dir.
func LoadConfig(env Env, dir string) (*Config, error) {
	path, explicit, err := FindConfig(env, dir)
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	exists := isFile(path)
	if explicit && !exists {
		return nil, fmt.Errorf("Configuration not found: %s", path)
	}
	if exists {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if data, err = parseTOML(string(raw)); err != nil {
			return nil, err
		}
	}
	c := &Config{Path: path, Exists: exists, Workspace: filepath.Dir(path)}
	section, _ := data["wiki"].(map[string]any)
	get := func(key, fallback string) string {
		if v, ok := section[key].(string); ok {
			return v
		}
		return fallback
	}
	at := func(envKey, value string) string {
		if v := env(envKey); v != "" {
			value = v
		}
		return resolvePath(expandUser(value), c.Workspace)
	}
	c.RepoRoot = at("WIKI_REPO_ROOT", ".")
	c.WikiRoot = at("WIKI_ROOT", get("path", "llm-wiki"))
	c.StateRoot = at("WIKI_STATE", get("state", ".llm-wiki"))
	c.Title = get("title", "Company Wiki")
	c.Reviewer = get("reviewer", "human:owner")
	if !reviewerRE.MatchString(c.Reviewer) {
		return nil, errors.New("wiki.reviewer must be human:<identifier>")
	}
	c.ExtraRoots = map[string]string{}
	c.RequiredSources = map[string]bool{}
	c.RequiredKinds = map[string]bool{}
	sources, _ := data["sources"].(map[string]any)
	aliases := make([]string, 0, len(sources))
	for alias := range sources {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		entry, ok := sources[alias].(map[string]any)
		kind, _ := entry["kind"].(string)
		p, _ := entry["path"].(string)
		if !aliasRE.MatchString(alias) || !ok || kind == "" || p == "" {
			return nil, fmt.Errorf("Invalid source configuration: %s", alias)
		}
		if _, known := Kinds[kind]; !known {
			return nil, fmt.Errorf("Unsupported source kind for %s: %s (%s)", alias, kind, strings.Join(kindNames(true), ", "))
		}
		enabled := true
		if v, ok := entry["enabled"].(bool); ok {
			enabled = v
		}
		c.Sources = append(c.Sources, Source{Alias: alias, Kind: kind, Path: p, Enabled: enabled})
		if enabled {
			c.ExtraRoots[alias] = resolvePath(expandUser(p), c.Workspace)
			c.RequiredSources[alias] = true
			c.RequiredKinds[kind] = true
		}
	}
	// Legacy environment overrides keep isolated fixture tests portable.
	for alias, key := range map[string]string{"claude-sessions": "WIKI_CLAUDE_SESSIONS_DIR", "codex-sessions": "WIKI_CODEX_SESSIONS_DIR"} {
		if v := env(key); v != "" {
			c.ExtraRoots[alias] = resolvePath(expandUser(v), c.Workspace)
		}
	}
	if len(c.RequiredKinds) == 0 {
		c.RequiredKinds["documents"] = true
	}
	return c, nil
}

func (c *Config) source(alias string) (Source, bool) {
	for _, s := range c.Sources {
		if s.Alias == alias {
			return s, true
		}
	}
	return Source{}, false
}

func expandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// resolvePath makes p absolute against base and resolves symlinks in the part
// that exists, like Python's Path.resolve(strict=False).
func resolvePath(p, base string) string {
	if !filepath.IsAbs(p) {
		if base == "" {
			base, _ = os.Getwd()
		}
		p = filepath.Join(base, p)
	}
	p = filepath.Clean(p)
	existing, rest := p, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return p
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	real, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return p
	}
	if rest == "" {
		return real
	}
	return filepath.Join(real, rest)
}

// within reports whether path is root or below it (both already resolved).
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
