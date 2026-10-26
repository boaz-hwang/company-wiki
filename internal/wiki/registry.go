package wiki

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RegistryHelp documents the registry command.
const RegistryHelp = `원천 레지스트리. 인용별 최신성과 원문 → 의존 페이지 역조회를 보여준다.

레지스트리(위키 저장소의 .verification/registry.json)는 페이지별로 채택 시점의 인용 줄 해시를 기록한다.
원문 사본은 두지 않는다. review apply 가 자동 기록하고, 담당자가 페이지를 직접 고쳤으면 커밋 전에
record <페이지> 를 실행해 레지스트리 변경을 같은 커밋에 넣는다.

  company-wiki registry [--source 경로] [--all] [--json]          조회 (읽기 전용)
  company-wiki registry --page concepts/x                         이 페이지가 바뀌면 영향받는 페이지
  company-wiki registry record                                    새 인용만 기록. 바뀐 원문의 기존 해시는 유지
  company-wiki registry record concepts/x ...                     지정 페이지를 재검토했을 때 현재 원문으로 갱신

--source 는 인용 형식(/별칭/파일.md 또는 ~/company-wiki 기준 /경로)이나 실제 파일 경로(상대·절대·~)를 받는다.
어느 형식이든 실제 파일로 풀어, 표기가 달라도 같은 파일을 가리키는 인용을 모두 찾는다.
인용한 페이지가 없으면 원천 폴더 안의 파일은 빈 결과, 등록 원천 밖이면 오류다.

상태: fresh(승인 때와 같음) · moved(내용은 같고 줄만 이동, 인용 번호 수정) · changed(원문 변경, 재검토)
      expired(세션 원문 정리됨) · missing(원문 없음) · unrecorded(기록 없는 인용)
영향: 재검토가 필요한 페이지에 depends_on·implements·applies_to 로 기대는 페이지도 함께 보여준다.`

type hit struct{ Slug, Lines string }

func (h hit) MarshalJSON() ([]byte, error) { return json.Marshal([]string{h.Slug, h.Lines}) }

// report maps source path → state → hits. moved hits read `old → new`.
func (c *Config) report() (map[string]map[string][]hit, error) {
	reg, err := c.loadRegistry()
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]hit{}
	for _, p := range c.pages() {
		text, err := readText(p.Path)
		if err != nil {
			return nil, err
		}
		for _, ks := range c.freshness(p.Slug, text, reg) {
			i := strings.LastIndex(ks.key, ":")
			path, lines := ks.key[:i], ks.key[i+1:]
			if ks.state == "moved" {
				to := c.movedTo(ks.key, p.Slug, reg)
				lines += " → " + to[strings.LastIndex(to, ":")+1:]
			}
			if out[path] == nil {
				out[path] = map[string][]hit{}
			}
			out[path][ks.state] = append(out[path][ks.state], hit{p.Slug, lines})
		}
	}
	// Text and --json list hits by page, then by line range in numeric order.
	for _, states := range out {
		for _, hits := range states {
			sort.SliceStable(hits, func(i, j int) bool {
				if hits[i].Slug != hits[j].Slug {
					return hits[i].Slug < hits[j].Slug
				}
				return naturalLess(hits[i].Lines, hits[j].Lines)
			})
		}
	}
	return out, nil
}

// Record adds hashes for unrecorded citations, or re-records the selected pages from current sources.
// Without a selection it keeps existing hashes: overwriting them would hide source changes.
func (c *Config) Record(selected []string) ([]string, error) {
	reg, err := c.loadRegistry()
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC().Format("2006-01-02T15:04:05+00:00")
	pages := map[string]Page{}
	var order []string
	for _, p := range c.pages() {
		pages[p.Slug] = p
		order = append(order, p.Slug)
	}
	var unknown []string
	chosen := map[string]bool{}
	for _, s := range selected {
		chosen[s] = true
		if _, ok := pages[s]; !ok {
			unknown = append(unknown, s)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("없는 페이지: %s", strings.Join(unknown, ", "))
	}
	var changed []string
	for _, slug := range order {
		if len(selected) > 0 && !chosen[slug] {
			continue
		}
		text, err := readText(pages[slug].Path)
		if err != nil {
			return nil, err
		}
		current := map[string]string{}
		for _, kh := range c.pageSpans(text) {
			if kh.hash != "" {
				current[kh.key] = kh.hash
			}
		}
		old := reg.citations(slug)
		next := current
		if len(selected) == 0 {
			next = map[string]string{}
			for k, v := range current {
				if o, ok := old[k]; ok {
					next[k] = o
				} else {
					next[k] = v
				}
			}
		}
		if _, recorded := reg.Pages[slug]; !recorded || !sameMap(next, old) {
			reg.Pages[slug] = &PageRecord{RecordedAt: at, Citations: next}
			changed = append(changed, slug)
		}
	}
	return changed, writeJSONFile(registryPath(c.WikiRoot), reg)
}

func sameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// jsonIndent writes indented JSON without HTML escaping, ending with a newline.
func jsonIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func jsonLine(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeJSONFile(path string, v any) error {
	data, err := jsonIndent(v)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// atomicWrite replaces path with data, or removes it when data is nil.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if data == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".write-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// sourceKeys finds the cited paths a --source value refers to. The value may be an `/alias/path` or a
// wiki-root-relative `/path` citation, or a file path (relative to dir, absolute or ~); each form is
// resolved to a file, and every citation whose file is the same matches, whatever its spelling.
// A value nothing cites is fine inside a registered source (no pages depend on it), else an error.
func (c *Config) sourceKeys(value, dir string, cited map[string]map[string][]hit) ([]string, error) {
	targets := map[string]bool{resolvePath(expandUser(value), dir): true}
	if strings.HasPrefix(value, "/") {
		if f, err := c.resolveResource(value); err == nil {
			targets[f] = true
		}
	}
	var keys []string
	for key := range cited {
		if key == value {
			keys = append(keys, key)
		} else if f, err := c.resolveResource(key); err == nil && targets[f] {
			keys = append(keys, key)
		}
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return keys, nil
	}
	if strings.HasPrefix(value, "/") {
		head, _, _ := strings.Cut(strings.TrimLeft(value, "/"), "/")
		if _, ok := c.source(head); ok || c.ExtraRoots[head] != "" {
			return nil, nil
		}
	}
	for t := range targets {
		for _, root := range c.ExtraRoots {
			if t != root && within(t, root) {
				return nil, nil
			}
		}
	}
	return nil, fmt.Errorf("등록 원천 밖의 경로: %s (원천 폴더 안의 파일이나 /별칭/경로 형식으로 지정)", value)
}

// RegistryCmd runs `company-wiki registry`; dir resolves a relative --source path.
func RegistryCmd(c *Config, args []string, dir string, out, errOut io.Writer) int {
	if len(args) > 0 && args[0] == "record" {
		changed, err := c.Record(args[1:])
		if err != nil {
			fmt.Fprintf(errOut, "ERROR: %v\n", err)
			return 1
		}
		msg := fmt.Sprintf("기록한 페이지 %d개", len(changed))
		if len(changed) > 0 {
			msg += ": " + strings.Join(changed, ", ")
		}
		fmt.Fprintln(out, msg)
		return 0
	}
	opts, pos, err := parseFlags(args, map[string]bool{"source": true, "page": true}, map[string]bool{"all": true, "json": true})
	if err != nil || len(pos) > 0 {
		fmt.Fprintln(errOut, RegistryHelp)
		return 2
	}
	matter := c.frontmatters()
	if page := opts["page"]; page != "" {
		if _, ok := matter[page]; !ok {
			fmt.Fprintf(errOut, "ERROR: 없는 페이지: %s\n", page)
			return 1
		}
		affected := dependents(matter, map[string]bool{page: true})
		if opts["json"] != "" {
			data, _ := jsonIndent(map[string]any{"page": page, "dependents": impactJSON(affected)})
			out.Write(data)
		} else {
			printDependents(out, affected, page)
		}
		return 0
	}
	data, err := c.report()
	if err != nil {
		fmt.Fprintf(errOut, "ERROR: %v\n", err)
		return 1
	}
	source := opts["source"]
	if source != "" {
		keys, err := c.sourceKeys(source, dir, data)
		if err != nil {
			fmt.Fprintf(errOut, "ERROR: %v\n", err)
			return 1
		}
		filtered := map[string]map[string][]hit{}
		for _, k := range keys {
			filtered[k] = data[k]
		}
		data = filtered
	}
	// --source starts from every page citing that source; the full report from pages needing review.
	roots := map[string]bool{}
	for _, states := range data {
		for state, hits := range states {
			if source != "" || state != "fresh" {
				for _, h := range hits {
					roots[h.Slug] = true
				}
			}
		}
	}
	affected := dependents(matter, roots)
	if opts["json"] != "" {
		b, _ := jsonIndent(map[string]any{"citations": data, "dependents": impactJSON(affected)})
		out.Write(b)
		return 0
	}
	attention, total := 0, 0
	paths := make([]string, 0, len(data))
	for p := range data {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	all := opts["all"] != ""
	for _, path := range paths {
		states := data[path]
		for _, hits := range states {
			total += len(hits)
		}
		if _, onlyFresh := states["fresh"]; onlyFresh && len(states) == 1 && !all && source == "" {
			continue
		}
		fmt.Fprintln(out, path)
		for state := range states {
			if state != "fresh" {
				attention++
				break
			}
		}
		names := make([]string, 0, len(states))
		for s := range states {
			names = append(names, s)
		}
		sort.Strings(names)
		for _, state := range names {
			if state == "fresh" && !(all || source != "") {
				continue
			}
			for _, h := range states[state] {
				fmt.Fprintf(out, "  %-10s %s (줄 %s)\n", state, h.Slug, h.Lines)
			}
		}
		if _, ok := states["missing"]; ok {
			if _, err := c.resolveResource(path); err != nil {
				fmt.Fprintf(out, "  이유: %v\n", err)
			}
		}
	}
	printDependents(out, affected, "")
	fmt.Fprintf(out, "\n원문 %d개 · 인용 %d개 · 확인 필요 원문 %d건 · 관계로 영향받는 페이지 %d개\n", len(data), total, attention, len(affected))
	return 0
}

func impactJSON(m map[string]Impact) map[string][]string {
	out := map[string][]string{}
	for k, v := range m {
		out[k] = []string{v.Kind, v.Target}
	}
	return out
}

func printDependents(out io.Writer, affected map[string]Impact, page string) {
	if len(affected) == 0 {
		if page != "" {
			fmt.Fprintf(out, "%s 에 depends_on·implements·applies_to 로 기대는 페이지 없음\n", page)
		}
		return
	}
	suffix := ""
	if page != "" {
		suffix = " (" + page + " 변경 시)"
	}
	fmt.Fprintf(out, "\n관계로 영향받는 페이지%s\n", suffix)
	slugs := make([]string, 0, len(affected))
	for s := range affected {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	for _, s := range slugs {
		fmt.Fprintf(out, "  %s ← %s %s\n", s, affected[s].Kind, affected[s].Target)
	}
}

// parseFlags accepts --name value, --name=value and boolean --name anywhere among the arguments.
func parseFlags(args []string, valued, boolean map[string]bool) (map[string]string, []string, error) {
	opts := map[string]string{}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") || a == "--" {
			pos = append(pos, a)
			continue
		}
		name, value, hasValue := strings.Cut(a[2:], "=")
		switch {
		case boolean[name] && !hasValue:
			opts[name] = "true"
		case valued[name]:
			if !hasValue {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("--%s needs a value", name)
				}
				i++
				value = args[i]
			}
			opts[name] = value
		default:
			return nil, nil, fmt.Errorf("unknown option --%s", name)
		}
	}
	return opts, pos, nil
}
