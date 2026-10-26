package wiki

import (
	"fmt"
	"io"
	"os"
)

// Version is set at build time with -ldflags "-X github.com/boaz-hwang/company-wiki/internal/wiki.Version=…".
var Version = "dev"

const usage = `company-wiki — LLM wiki of adopted knowledge (concepts, entities, policies)

  company-wiki setup [--source kind[:alias]=path ...] [--list-kinds]
  company-wiki lint [--wiki-only]
  company-wiki index
  company-wiki registry [record [page ...]] [--source path] [--page slug] [--all] [--json]
  company-wiki review prepare|preview|decide|apply|status|recover ...
  company-wiki inventory <inventory.json> [--require-complete]
  company-wiki session <path> [--start N] [--end M] [--tools] [--max-chars N]
  company-wiki version

wiki.toml is WIKI_CONFIG, or the nearest wiki.toml from the current folder upward, or ~/company-wiki/wiki.toml.
Next to it the layout is fixed: llm-wiki/ (the wiki) and workspace/wiki-changes/ (change materials).
lint --wiki-only checks the current folder (or WIKI_ROOT) as the wiki without wiki.toml or sources,
for the wiki repository's CI; cited files and registry hashes are reported as info.`

// IO bundles a command's environment for tests.
type IO struct {
	Env    Env
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Main runs a command and returns the exit code.
func Main(args []string, x IO) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprintln(x.Stdout, usage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "--version":
		fmt.Fprintln(x.Stdout, "company-wiki "+Version)
		return 0
	case "setup":
		return Setup(rest, x)
	case "session":
		return SessionCmd(rest, x)
	case "lint":
		opts, pos, err := parseFlags(rest, nil, map[string]bool{"wiki-only": true})
		if err != nil || len(pos) > 0 {
			fmt.Fprintf(x.Stderr, "usage: company-wiki lint [--wiki-only]\n")
			return 2
		}
		if opts["wiki-only"] != "" {
			return Lint(WikiOnlyConfig(x.Env, x.Dir), x.Stdout)
		}
	}
	c, err := LoadConfig(x.Env, x.Dir)
	if err != nil {
		fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
		return 1
	}
	switch cmd {
	case "lint":
		return Lint(c, x.Stdout)
	case "index":
		n, err := BuildIndex(c)
		if err != nil {
			fmt.Fprintf(x.Stderr, "ERROR: %v\n", err)
			return 1
		}
		fmt.Fprintf(x.Stdout, "index.md: %d pages\n", n)
		return 0
	case "registry":
		return RegistryCmd(c, rest, x.Dir, x.Stdout, x.Stderr)
	case "review":
		return ReviewCmd(c, rest, x)
	case "inventory":
		return InventoryCmd(c, rest, x.Stdout)
	}
	fmt.Fprintf(x.Stderr, "unknown command %q\n\n%s\n", cmd, usage)
	return 2
}

// OSIO is the real process environment.
func OSIO() IO {
	dir, _ := os.Getwd()
	return IO{Env: os.Getenv, Dir: dir, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
}
