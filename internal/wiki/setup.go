package wiki

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	companywiki "github.com/boaz-hwang/company-wiki"
)

const setupHelp = `Prepare a wiki workspace without replacing existing files.

  company-wiki setup                                   새 작업 공간이면 원천을 체크박스로 고른다 (터미널)
  company-wiki setup --source documents=~/docs --source claude=~/.claude/projects/X
                                                       에이전트·스크립트용. 기존 설정에는 원천을 추가한다
  company-wiki setup --list-kinds                      등록할 수 있는 원천 종류

wiki.toml은 WIKI_CONFIG, 없으면 현재 폴더부터 위로 찾은 wiki.toml, 그것도 없으면 현재 폴더에 만든다.`

// copyMarker marks a skill folder this tool installed and may refresh.
const copyMarker = ".company-wiki-copy.json"

// Setup runs `company-wiki setup`.
func Setup(args []string, x IO) int {
	var specs []sourceSpec
	listKinds := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--list-kinds":
			listKinds = true
		case a == "--source" && i+1 < len(args):
			i++
			s, err := parseSpec(args[i])
			if err != nil {
				fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
				return 1
			}
			specs = append(specs, s)
		case strings.HasPrefix(a, "--source="):
			s, err := parseSpec(strings.TrimPrefix(a, "--source="))
			if err != nil {
				fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
				return 1
			}
			specs = append(specs, s)
		case a == "-h" || a == "--help":
			fmt.Fprintln(x.Stdout, setupHelp)
			return 0
		default:
			fmt.Fprintf(x.Stderr, "unknown argument %q\n\n%s\n", a, setupHelp)
			return 2
		}
	}
	if listKinds {
		fmt.Fprintln(x.Stdout, KindsText())
		return 0
	}
	config, explicit, err := FindConfig(x.Env, x.Dir)
	if err != nil {
		fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
		return 1
	}
	base := filepath.Dir(config)
	if err := os.MkdirAll(base, 0o755); err != nil {
		fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
		return 1
	}
	if !isFile(config) {
		if len(specs) == 0 {
			if !isTerminal(x.Stdin) || !isTerminal(x.Stdout) {
				fmt.Fprintf(x.Stderr, "등록할 원천을 --source kind=path 로 지정하세요. 에이전트는 사용자에게 원천 종류와 경로를 먼저 묻는다.\n"+
					"종류:\n%s\n예: company-wiki setup --source documents=../docs --source slack=../exports/slack\n", KindsText())
				return 2
			}
			if specs, err = askSources(x, base); err != nil {
				fmt.Fprintf(x.Stderr, "%v\n", err)
				return 1
			}
		}
		blocks, err := sourceBlocks(specs, map[string]bool{}, base)
		if err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
			return 1
		}
		text := wikiBlock
		for _, b := range blocks {
			text += "\n" + b
		}
		if err := os.WriteFile(config, []byte(text), 0o644); err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
			return 1
		}
	} else if len(specs) > 0 {
		raw, err := os.ReadFile(config)
		if err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
			return 1
		}
		data, err := parseTOML(string(raw))
		if err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
			return 1
		}
		taken := map[string]bool{}
		if sources, ok := data["sources"].(map[string]any); ok {
			for k := range sources {
				taken[k] = true
			}
		}
		blocks, err := sourceBlocks(specs, taken, base)
		if err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
			return 1
		}
		text := strings.TrimRight(strings.TrimPrefix(string(raw), "\ufeff"), "\n") + "\n"
		for _, b := range blocks {
			text += "\n" + b
		}
		if err := os.WriteFile(config, []byte(text), 0o644); err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
			return 1
		}
	}
	env := x.Env
	if !explicit {
		// Use the config this run created or found, not one higher up.
		env = func(k string) string {
			if k == "WIKI_CONFIG" {
				return config
			}
			return x.Env(k)
		}
	}
	c, err := LoadConfig(env, x.Dir)
	if err == nil {
		err = prepareWorkspace(c, x.Stdout)
	}
	if err != nil {
		fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
		return 1
	}
	fmt.Fprintf(x.Stdout, "Workspace ready: %s (state: %s)\n", c.WikiRoot, c.StateRoot)
	for _, s := range c.Sources {
		mark := ""
		if root, ok := c.ExtraRoots[s.Alias]; ok && !exists(root) {
			mark = "  ← 경로 없음"
		}
		fmt.Fprintf(x.Stdout, "  source %-16s %-11s %s%s\n", s.Alias, s.Kind, s.Path, mark)
	}
	if len(c.Sources) == 0 {
		fmt.Fprintln(x.Stdout, "  등록된 원천이 없습니다. --source kind=path 로 추가하세요.")
	}
	fmt.Fprintf(x.Stdout, "Config: %s. Follow the README starter prompt.\n", c.Path)
	return 0
}

func sourceBlocks(specs []sourceSpec, taken map[string]bool, base string) ([]string, error) {
	var out []string
	for _, s := range specs {
		if !exists(resolvePath(expandUser(s.path), base)) {
			return nil, fmt.Errorf("원천 경로가 없습니다: %s %s", s.kind, s.path)
		}
		alias := s.alias
		if alias == "" {
			alias = chooseAlias(s.kind, s.path, taken)
		}
		if taken[alias] {
			return nil, fmt.Errorf("이미 등록된 원천 별칭입니다: %s", alias)
		}
		taken[alias] = true
		block, err := sourceBlock(alias, s.kind, s.path)
		if err != nil {
			return nil, err
		}
		out = append(out, block)
	}
	return out, nil
}

func askSources(x IO, base string) ([]sourceSpec, error) {
	in, _ := x.Stdin.(*os.File)
	items := make([]item, len(kindOrder))
	for i, k := range kindOrder {
		items[i] = item{k, Kinds[k].Label}
	}
	kinds, err := checkbox(in, x.Stdout, "등록할 원천을 고르세요", items)
	if err != nil {
		return nil, err
	}
	reader := bufio.NewReader(in)
	var specs []sourceSpec
	for _, kind := range kinds {
		def := defaultPath(kind, base)
		for {
			prompt := kind + " 경로"
			if def != "" {
				prompt += " [" + def + "]"
			}
			fmt.Fprint(x.Stdout, prompt+" (여러 개면 쉼표): ")
			line, err := reader.ReadString('\n')
			if err != nil && line == "" {
				return nil, errCancelled
			}
			answer := strings.TrimSpace(trimLine(line))
			if answer == "" {
				answer = def
			}
			var paths, missing []string
			for _, p := range strings.Split(answer, ",") {
				if p = strings.TrimSpace(p); p != "" {
					paths = append(paths, p)
					if !exists(resolvePath(expandUser(p), base)) {
						missing = append(missing, p)
					}
				}
			}
			if len(paths) > 0 && len(missing) == 0 {
				for _, p := range paths {
					specs = append(specs, sourceSpec{kind: kind, path: p})
				}
				break
			}
			if len(missing) > 0 {
				fmt.Fprintln(x.Stdout, "없는 경로: "+strings.Join(missing, ", "))
			} else {
				fmt.Fprintln(x.Stdout, "경로를 입력하세요.")
			}
		}
	}
	return specs, nil
}

func prepareWorkspace(c *Config, out io.Writer) error {
	for _, d := range []struct{ name, root string }{{"wiki.path", c.WikiRoot}, {"wiki.state", c.StateRoot}} {
		if !within(d.root, c.RepoRoot) || d.root == c.RepoRoot {
			return fmt.Errorf("%s must be a subdirectory of the workspace", d.name)
		}
	}
	if within(c.StateRoot, c.WikiRoot) || within(c.WikiRoot, c.StateRoot) {
		return errors.New("wiki.state must be separate from wiki.path; drafts and records are not adopted knowledge")
	}
	for _, folder := range folderOrder {
		if err := os.MkdirAll(filepath.Join(c.WikiRoot, folder), 0o755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Join(c.StateRoot, "reviews"), 0o755); err != nil {
		return err
	}
	schema := filepath.Join(c.WikiRoot, "SCHEMA.md")
	if !exists(schema) {
		data, err := fs.ReadFile(companywiki.Assets, "SCHEMA.md")
		if err != nil {
			return err
		}
		if err := os.WriteFile(schema, data, 0o644); err != nil {
			return err
		}
	}
	if !exists(filepath.Join(c.WikiRoot, "index.md")) {
		n, err := BuildIndex(c)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "index.md: %d pages\n", n)
	}
	skills, err := fs.ReadDir(companywiki.Assets, "skills")
	if err != nil {
		return err
	}
	for _, provider := range []string{".claude", ".agents"} {
		for _, s := range skills {
			if _, err := fs.Stat(companywiki.Assets, path.Join("skills", s.Name(), "SKILL.md")); err != nil {
				continue
			}
			src, _ := fs.Sub(companywiki.Assets, path.Join("skills", s.Name()))
			if err := installSkill(src, filepath.Join(c.Workspace, provider, "skills", s.Name()), out); err != nil {
				return err
			}
		}
	}
	if isToolkitRepo(c.Workspace) {
		return nil
	}
	return writePointer(c)
}

func snapshot(dir string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == copyMarker {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = sha(readBytes(p))
		return nil
	})
	return out
}

// installSkill copies a skill and refreshes only a complete, unchanged managed copy.
// Symlinked skills and folders a person edited are kept.
func installSkill(src fs.FS, dest string, out io.Writer) error {
	if st, err := os.Lstat(dest); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(out, "Existing skill preserved: %s\n", dest)
			return nil
		}
		var previous map[string]string
		raw, err := os.ReadFile(filepath.Join(dest, copyMarker))
		if err != nil || json.Unmarshal(raw, &previous) != nil || !sameMap(snapshot(dest), previous) {
			fmt.Fprintf(out, "Existing skill preserved: %s\n", dest)
			return nil
		}
		if err := os.RemoveAll(dest); err != nil {
			return err
		}
	}
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return err
	}
	marker, _ := json.Marshal(snapshot(dest))
	return os.WriteFile(filepath.Join(dest, copyMarker), marker, 0o644)
}

var pointerRE = regexp.MustCompile(`(?s)\n?<!-- (?:llm|boaz|company)-wiki -->.*?<!-- /(?:llm|boaz|company)-wiki -->\n?`)

// writePointer adds or refreshes the wiki block in the workspace AGENTS.md.
func writePointer(c *Config) error {
	pointer := fmt.Sprintf("\n<!-- company-wiki -->\nWiki rules: %s. Read %s first. Drafts and records: %s.\n"+
		"Tool: the company-wiki CLI (setup, lint, index, registry, review, inventory, session). Config: %s.\n<!-- /company-wiki -->\n",
		filepath.Join(c.WikiRoot, "SCHEMA.md"), filepath.Join(c.WikiRoot, "index.md"), c.StateRoot, c.Path)
	target := filepath.Join(c.Workspace, "AGENTS.md")
	old := ""
	if b, err := os.ReadFile(target); err == nil {
		old = strings.TrimPrefix(string(b), "\ufeff")
	}
	updated := old + pointer
	if loc := pointerRE.FindStringIndex(old); loc != nil {
		updated = old[:loc[0]] + pointer + old[loc[1]:]
	}
	if updated == old {
		return nil
	}
	return os.WriteFile(target, []byte(updated), 0o644)
}

// isToolkitRepo keeps setup from writing a pointer into this tool's own repository.
func isToolkitRepo(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	return err == nil && strings.Contains(string(b), "module github.com/boaz-hwang/company-wiki")
}

func sortedStrings(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
