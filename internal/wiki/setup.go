package wiki

import (
	"bytes"
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

const setupHelp = `Prepare ~/company-wiki (Windows: %USERPROFILE%\\company-wiki) without replacing existing files.

  company-wiki setup                                   새 설정이면 원천을 체크박스로 고른다 (터미널)
  company-wiki setup --source documents=~/docs --source claude=~/.claude/projects/X
                                                       에이전트·스크립트용. 기존 설정에는 원천을 추가한다
                                                       (같은 종류·경로로 이미 등록된 원천은 건너뛴다)
  company-wiki setup --vault ~/Documents/MyVault       workspace를 이 Vault에 연결 (workspace가 없을 때)
  company-wiki setup --list-kinds                      등록할 수 있는 원천 종류

현재 폴더와 관계없이 ~/company-wiki에 wiki.toml, llm-wiki/(위키), workspace/wiki-changes/(변경 절차 폴더),
스킬, AGENTS.md를 만든다.
--source의 상대 경로는 현재 폴더 기준이며 절대 경로(홈 아래면 ~/…)로 저장한다.
workspace가 있으면 그대로 쓴다. 없으면 --vault의 폴더, 그다음 Obsidian 설정에서 찾은 Vault에 연결하고,
둘 다 없으면 빈 폴더를 만든다. Vault 안에는 wiki-changes/만 만들고 기존 내용은 건드리지 않는다.`

// copyMarker marks a skill folder this tool installed and may refresh.
const copyMarker = ".company-wiki-copy.json"

// Setup runs `company-wiki setup`. It always prepares the fixed root, ~/company-wiki.
func Setup(args []string, x IO) int {
	var specs []sourceSpec
	listKinds := false
	vault := ""
	fail := func(err error) int {
		fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
		return 1
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--list-kinds":
			listKinds = true
		case a == "--source" && i+1 < len(args):
			i++
			s, err := parseSpec(args[i])
			if err != nil {
				return fail(err)
			}
			specs = append(specs, s)
		case strings.HasPrefix(a, "--source="):
			s, err := parseSpec(strings.TrimPrefix(a, "--source="))
			if err != nil {
				return fail(err)
			}
			specs = append(specs, s)
		case a == "--vault" && i+1 < len(args):
			i++
			vault = args[i]
		case strings.HasPrefix(a, "--vault="):
			vault = strings.TrimPrefix(a, "--vault=")
		case a == "-h" || a == "--help":
			io.WriteString(x.Stdout, setupHelp+"\n")
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
	if vault != "" && strings.TrimSpace(vault) == "" {
		return fail(errors.New("--vault needs a folder path"))
	}
	base, err := DefaultRoot()
	if err != nil {
		return fail(err)
	}
	// A clone of this tool at ~/company-wiki would get a wiki, a workspace and skills mixed into its source tree.
	if isToolkitRepo(base) {
		return fail(fmt.Errorf("%s는 company-wiki 도구 저장소의 checkout이다. setup은 이 폴더를 위키 루트로 쓰지 않으며 아무것도 쓰지 않았다. "+
			"도구 저장소를 다른 이름으로 옮기고(예: mv %s %s-tool) 다시 실행", base, base, base))
	}
	if vault != "" {
		vault = vaultPath(vault, x.Dir)
		// A bad --vault fails before anything is written; an existing workspace makes it unused.
		if _, err := os.Lstat(filepath.Join(base, "workspace")); err != nil {
			if reason := vaultProblem(vault, base); reason != "" {
				return fail(fmt.Errorf("--vault %s: %s", vault, reason))
			}
		}
	}
	config := filepath.Join(base, "wiki.toml")
	// Always the fixed root's config, whatever the current folder says.
	if isFile(config) {
		// Reject an unsupported config before changing anything.
		if _, err := loadConfigAt(x.Env, config); err != nil {
			return fail(err)
		}
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return fail(err)
	}
	if !isFile(config) {
		if len(specs) == 0 {
			if !isTerminal(x.Stdin) || !isTerminal(x.Stdout) {
				fmt.Fprintf(x.Stderr, "등록할 원천을 --source kind=path 로 지정하세요. 에이전트는 사용자에게 원천 종류와 경로를 먼저 묻는다.\n"+
					"종류:\n%s\n예: company-wiki setup --source documents=~/docs --source slack=~/exports/slack\n", KindsText())
				return 2
			}
			if specs, err = askSources(x, x.Dir); err != nil {
				fmt.Fprintf(x.Stderr, "%v\n", err)
				return 1
			}
		}
		blocks, notices, err := sourceBlocks(specs, map[string]bool{}, nil, x.Dir)
		if err != nil {
			return fail(err)
		}
		printLines(x.Stdout, notices)
		text := wikiBlock
		for _, b := range blocks {
			text += "\n" + b
		}
		if err := os.WriteFile(config, []byte(text), 0o644); err != nil {
			return fail(err)
		}
	} else if len(specs) > 0 {
		raw, err := os.ReadFile(config)
		if err != nil {
			return fail(err)
		}
		data, err := parseTOML(string(raw))
		if err != nil {
			return fail(err)
		}
		taken := map[string]bool{}
		var known []registered
		if sources, ok := data["sources"].(map[string]any); ok {
			for _, alias := range sortedKeys(mapKeys(sources)) {
				taken[alias] = true
				entry, _ := sources[alias].(map[string]any)
				kind, _ := entry["kind"].(string)
				p, _ := entry["path"].(string)
				enabled, isBool := entry["enabled"].(bool)
				if kind != "" && p != "" {
					// Registered paths resolve from wiki.toml; new --source paths from the current folder.
					known = append(known, registered{alias, kind, resolvePath(expandUser(p), base), enabled || !isBool})
				}
			}
		}
		blocks, notices, err := sourceBlocks(specs, taken, known, x.Dir)
		if err != nil {
			return fail(err)
		}
		printLines(x.Stdout, notices)
		// Nothing new: leave wiki.toml byte for byte as it was.
		if len(blocks) > 0 {
			if err := os.WriteFile(config, appendBlocks(raw, blocks), 0o644); err != nil {
				return fail(err)
			}
		}
	}
	c, err := loadConfigAt(x.Env, config)
	if err != nil {
		return fail(err)
	}
	if err := setupVault(c.Root, vault, x.Stdout); err != nil {
		return fail(err)
	}
	// The workspace may now be a link to a vault; resolve the change materials through it.
	c.ChangesRoot = resolvePath(changesDir, c.Root)
	if err := prepareRoot(c, x.Stdout); err != nil {
		return fail(err)
	}
	fmt.Fprintf(x.Stdout, "Wiki ready: %s\nChange materials: %s\n", c.WikiRoot, c.ChangesRoot)
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

// appendBlocks adds TOML blocks after the existing bytes, which stay as they are (BOM, comments, trailing
// blank lines). The new lines use the file's line ending, CRLF if it has any; a blank line separates them.
func appendBlocks(raw []byte, blocks []string) []byte {
	eol := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		eol = "\r\n"
	}
	out := append([]byte(nil), raw...)
	body := bytes.TrimPrefix(out, []byte("\ufeff"))
	if len(body) > 0 && !bytes.HasSuffix(body, []byte("\n")) {
		out = append(out, eol...)
	}
	for _, b := range blocks {
		if len(bytes.TrimPrefix(out, []byte("\ufeff"))) > 0 && !bytes.HasSuffix(out, []byte("\n\n")) && !bytes.HasSuffix(out, []byte("\n\r\n")) {
			out = append(out, eol...)
		}
		out = append(out, strings.ReplaceAll(b, "\n", eol)...)
	}
	return out
}

// registered is a source already in wiki.toml, or added earlier in the same run; root is resolved.
type registered struct {
	alias, kind, root string
	enabled           bool
}

// sourceBlocks returns the TOML blocks for new sources and notices for skipped ones. Spec paths resolve
// from cwd and are written absolute, or as ~/… under the home folder. A source with the same kind and
// resolved path as a registered one is skipped, even under another alias; the same path with another
// kind is a different source.
func sourceBlocks(specs []sourceSpec, taken map[string]bool, known []registered, cwd string) ([]string, []string, error) {
	var out, notices []string
next:
	for _, s := range specs {
		root := resolvePath(expandUser(s.path), cwd)
		if !exists(root) {
			return nil, nil, fmt.Errorf("원천 경로가 없습니다: %s %s", s.kind, s.path)
		}
		for _, r := range known {
			if r.kind != s.kind || r.root != root {
				continue
			}
			msg := fmt.Sprintf("이미 등록됨: %s %s (별칭 %s", s.kind, s.path, r.alias)
			if !r.enabled {
				msg += ", enabled = false"
			}
			msg += ")"
			if s.alias != "" && s.alias != r.alias {
				msg += fmt.Sprintf(". 별칭 %s 는 추가하지 않음", s.alias)
			}
			notices = append(notices, msg)
			continue next
		}
		alias := s.alias
		if alias == "" {
			alias = chooseAlias(s.kind, s.path, taken)
		}
		if taken[alias] {
			return nil, nil, fmt.Errorf("이미 등록된 원천 별칭입니다: %s", alias)
		}
		taken[alias] = true
		block, err := sourceBlock(alias, s.kind, storedPath(s.path, cwd))
		if err != nil {
			return nil, nil, err
		}
		out = append(out, block)
		known = append(known, registered{alias, s.kind, root, true})
	}
	return out, notices, nil
}

// storedPath makes p absolute against cwd without resolving links, and writes it as ~/… under the home folder.
func storedPath(p, cwd string) string {
	abs := expandUser(p)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	abs = filepath.Clean(abs)
	if home, err := os.UserHomeDir(); err == nil {
		// Compare as written, then with links resolved (macOS /tmp is /private/tmp).
		for _, pair := range [][2]string{{abs, filepath.Clean(home)}, {resolvePath(abs, ""), resolvePath(home, "")}} {
			if pair[0] == pair[1] {
				return "~"
			}
			if within(pair[0], pair[1]) {
				rel, _ := filepath.Rel(pair[1], pair[0])
				return "~/" + filepath.ToSlash(rel)
			}
		}
	}
	return abs
}

func mapKeys(m map[string]any) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func printLines(out io.Writer, lines []string) {
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}
}

func askSources(x IO, cwd string) ([]sourceSpec, error) {
	in, _ := x.Stdin.(*os.File)
	items := make([]item, len(kindOrder))
	for i, k := range kindOrder {
		items[i] = item{k, Kinds[k].Label}
	}
	kinds, rest, err := checkbox(in, x.Stdout, "등록할 원천을 고르세요", items)
	if err != nil {
		return nil, err
	}
	reader := newLineReader(rest, in, x.Stdout)
	var specs []sourceSpec
	for _, kind := range kinds {
		def := defaultPath(kind, cwd)
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
					if !exists(resolvePath(expandUser(p), cwd)) {
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

func prepareRoot(c *Config, out io.Writer) error {
	if within(c.ChangesRoot, c.WikiRoot) || within(c.WikiRoot, c.ChangesRoot) {
		return fmt.Errorf("change materials (%s) must be separate from the wiki (%s); drafts and records are not adopted knowledge", c.ChangesRoot, c.WikiRoot)
	}
	for _, folder := range folderOrder {
		if err := os.MkdirAll(filepath.Join(c.WikiRoot, folder), 0o755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(c.ChangesRoot, 0o755); err != nil {
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
			if err := installSkill(src, filepath.Join(c.Root, provider, "skills", s.Name()), out); err != nil {
				return err
			}
		}
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

// writePointer adds or refreshes the wiki block in the root AGENTS.md.
func writePointer(c *Config) error {
	pointer := fmt.Sprintf("\n<!-- company-wiki -->\nWiki rules: %s. Read %s first. Change materials (drafts, reviews, decisions): %s.\n"+
		"Tool: the company-wiki CLI (setup, lint, index, registry, review, inventory, session). Config: %s.\n<!-- /company-wiki -->\n",
		filepath.Join(c.WikiRoot, "SCHEMA.md"), filepath.Join(c.WikiRoot, "index.md"), c.ChangesRoot, c.Path)
	target := filepath.Join(c.Root, "AGENTS.md")
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

// isToolkitRepo reports a checkout of this tool's own repository, which setup never uses as the wiki root.
func isToolkitRepo(dir string) bool {
	if isDir(filepath.Join(dir, "cmd", "company-wiki")) {
		return true
	}
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
