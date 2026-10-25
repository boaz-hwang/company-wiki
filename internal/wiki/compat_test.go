package wiki

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The fixtures were produced by the Python tool this binary replaced; these tests keep the formats identical.

func TestFrontmatterMatchesPythonParser(t *testing.T) {
	raw, err := os.ReadFile("testdata/frontmatter.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input string         `json:"input"`
		FM    map[string]any `json:"fm"`
		Body  string         `json:"body"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		fm, body := ParseFrontmatter(c.Input)
		var got map[string]any
		if fm != nil {
			got = map[string]any(fm)
		}
		if !reflect.DeepEqual(got, c.FM) || body != c.Body {
			t.Errorf("input %q\n got %#v %q\nwant %#v %q", c.Input, got, body, c.FM, c.Body)
		}
	}
}

func TestUnifiedDiffMatchesDifflib(t *testing.T) {
	raw, err := os.ReadFile("testdata/difflib.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		A    []string `json:"a"`
		B    []string `json:"b"`
		Diff string   `json:"diff"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		if got := unifiedDiff(c.A, c.B, "before/x", "after/x"); got != c.Diff {
			t.Fatalf("case %d differs from difflib\n got:\n%s\nwant:\n%s", i, got, c.Diff)
		}
	}
}

func TestPyJSONMatchesJSONDumps(t *testing.T) {
	v, err := decodeOrdered([]byte(`{"b": [1, 2.5, true, null], "a": "<&> \"q\" \\ \n 한글", "c": {}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pyJSON(v, true), `{"a": "<&> \"q\" \\ \n 한글", "b": [1, 2.5, true, null], "c": {}}`; got != want {
		t.Errorf("sorted\n got %s\nwant %s", got, want)
	}
	if got, want := pyJSON(v, false), `{"b": [1, 2.5, true, null], "a": "<&> \"q\" \\ \n 한글", "c": {}}`; got != want {
		t.Errorf("ordered\n got %s\nwant %s", got, want)
	}
	want := "{\n  \"b\": [\n    1,\n    2.5,\n    true,\n    null\n  ],\n  \"a\": \"<&> \\\"q\\\" \\\\ \\n 한글\",\n  \"c\": {}\n}"
	if got := pyJSONIndent(v); got != want {
		t.Errorf("indent\n got %s\nwant %s", got, want)
	}
}

func TestTOMLSubset(t *testing.T) {
	data, err := parseTOML("\ufeff# c\n[wiki]\npath = \"llm-wiki\" # trailing\nreviewer='human:a'\n[sources.claude-sessions]\nkind = \"claude\"\npath = 'C:\\Users\\x'\ninclude = [\"**/*.md\",\n  \"*.mdx\", ]\nenabled = false\nn = 1_000\n")
	if err != nil {
		t.Fatal(err)
	}
	s := data["sources"].(map[string]any)["claude-sessions"].(map[string]any)
	if s["path"] != `C:\Users\x` || s["enabled"] != false || len(s["include"].([]any)) != 2 || s["n"] != int64(1000) {
		t.Errorf("unexpected %#v", s)
	}
	if data["wiki"].(map[string]any)["path"] != "llm-wiki" {
		t.Errorf("unexpected %#v", data["wiki"])
	}
	for _, bad := range []string{"[wiki]\npath = \"x\"\npath = \"y\"\n", "key = value\n", "a = \"open\n"} {
		if _, err := parseTOML(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
