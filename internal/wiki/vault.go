package wiki

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// The workspace is the person's own vault: change materials live in workspace/wiki-changes, outside the
// wiki repository. Setup never moves, renames or edits what is already in the vault.

// setupVault makes <root>/workspace: an existing folder or link is kept; otherwise it links to --vault,
// then to the Obsidian vault found in obsidian.json, and otherwise creates an empty folder.
// vault is an absolute folder from vaultPath, or "".
func setupVault(root, vault string, out io.Writer) error {
	ws := filepath.Join(root, "workspace")
	hint := fmt.Sprintf("바꾸려면 링크만 지우고(macOS·Linux: rm %s, Windows: rmdir %s) company-wiki setup --vault <경로>로 다시 실행. Vault 내용은 지우지 않는다.", ws, ws)
	if st, err := os.Lstat(ws); err == nil {
		target, linkErr := os.Readlink(ws)
		switch {
		case linkErr == nil && !isDir(ws):
			return fmt.Errorf("workspace 링크의 대상이 없음: %s → %s. 링크를 지우고 setup --vault <경로>로 다시 실행", ws, target)
		case linkErr == nil:
			fmt.Fprintf(out, "workspace: 기존 링크 사용 %s → %s\n%s\n", ws, target, hint)
		case st.IsDir():
			fmt.Fprintf(out, "workspace: 기존 폴더 사용 %s\n", ws)
		default:
			return fmt.Errorf("workspace가 폴더나 링크가 아님: %s", ws)
		}
		if vault != "" {
			fmt.Fprintf(out, "--vault 무시: workspace가 이미 있음. %s\n", hint)
		}
		return nil
	}
	if vault != "" {
		target := vault
		if reason := vaultProblem(target, root); reason != "" {
			return fmt.Errorf("--vault %s: %s", vault, reason)
		}
		if err := linkDir(target, ws); err != nil {
			return fmt.Errorf("workspace 링크 생성 실패 (%s → %s): %w", ws, target, err)
		}
		fmt.Fprintf(out, "workspace: --vault의 Vault에 연결 %s → %s\n%s\n", ws, target, hint)
		return nil
	}
	if target, from := detectVault(root); target != "" {
		if err := linkDir(target, ws); err != nil {
			return fmt.Errorf("workspace 링크 생성 실패 (%s → %s): %w", ws, target, err)
		}
		fmt.Fprintf(out, "workspace: Obsidian Vault 자동 감지(%s), 연결 %s → %s\n%s\n", from, ws, target, hint)
		return nil
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return err
	}
	fmt.Fprintf(out, "workspace: 연결할 Vault가 없어 새 폴더 생성 %s\nVault를 쓰려면 이 폴더를 다른 이름으로 옮기고 company-wiki setup --vault <경로>로 다시 실행.\n", ws)
	return nil
}

// vaultPath makes --vault absolute against cwd without resolving links.
func vaultPath(vault, cwd string) string {
	target := expandUser(vault)
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, target)
	}
	return filepath.Clean(target)
}

// vaultProblem explains why a folder cannot be the workspace, or returns "".
func vaultProblem(path, root string) string {
	if !isDir(path) {
		return "폴더가 없음"
	}
	if isFile(filepath.Join(path, "wiki.toml")) {
		return "wiki.toml이 있는 폴더는 Vault로 쓰지 않음"
	}
	p, r := resolvePath(path, ""), resolvePath(root, "")
	if within(p, r) {
		return "company-wiki 루트 안의 폴더는 Vault로 쓰지 않음"
	}
	if within(r, p) {
		return "company-wiki 루트를 포함하는 폴더는 링크가 순환하므로 Vault로 쓰지 않음"
	}
	return ""
}

// obsidianConfigs lists where Obsidian keeps obsidian.json on this OS.
func obsidianConfigs() []string {
	home, _ := os.UserHomeDir()
	config, _ := os.UserConfigDir() // macOS ~/Library/Application Support, Windows %APPDATA%, else $XDG_CONFIG_HOME or ~/.config
	return obsidianConfigsFor(runtime.GOOS, home, config)
}

// obsidianConfigsFor lists obsidian.json locations for goos. On Linux the Flatpak and Snap packages keep
// their own config folders besides $XDG_CONFIG_HOME and ~/.config.
func obsidianConfigsFor(goos, home, config string) []string {
	var files []string
	if config != "" {
		files = append(files, filepath.Join(config, "obsidian", "obsidian.json"))
	}
	if goos != "darwin" && goos != "windows" && home != "" {
		files = append(files,
			filepath.Join(home, ".config", "obsidian", "obsidian.json"),
			filepath.Join(home, ".var", "app", "md.obsidian.Obsidian", "config", "obsidian", "obsidian.json"), // Flatpak
			filepath.Join(home, "snap", "obsidian", "current", ".config", "obsidian", "obsidian.json"),       // Snap
		)
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range files {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

type obsidianVault struct {
	Path string  `json:"path"`
	TS   float64 `json:"ts"`
	Open bool    `json:"open"`
}

// detectVault picks the person's Obsidian vault: open first, then most recently used. Vaults that are
// missing, hold a wiki.toml, or sit inside or around root are skipped. It returns the vault and the
// obsidian.json it came from.
func detectVault(root string) (string, string) {
	return detectVaultIn(root, obsidianConfigs())
}

func detectVaultIn(root string, files []string) (string, string) {
	type candidate struct {
		obsidianVault
		from string
	}
	var found []candidate
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var cfg struct {
			Vaults map[string]obsidianVault `json:"vaults"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(string(data), "\ufeff")), &cfg) != nil {
			continue
		}
		for _, v := range cfg.Vaults {
			if v.Path == "" || !filepath.IsAbs(v.Path) || vaultProblem(v.Path, root) != "" {
				continue
			}
			found = append(found, candidate{v, file})
		}
	}
	if len(found) == 0 {
		return "", ""
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.Open != b.Open {
			return a.Open
		}
		if a.TS != b.TS {
			return a.TS > b.TS
		}
		return a.Path < b.Path
	})
	return filepath.Clean(found[0].Path), found[0].from
}
