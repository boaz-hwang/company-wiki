package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain points every home and config location at a temporary folder, so no test can read or
// write the real ~/company-wiki or the person's Obsidian settings.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "company-wiki-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	home = resolvePath(home, "")
	for k, v := range map[string]string{"HOME": home, "USERPROFILE": home, "APPDATA": filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA": filepath.Join(home, "AppData", "Local"), "XDG_CONFIG_HOME": "", "WIKI_CONFIG": ""} {
		os.Setenv(k, v)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
