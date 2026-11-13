package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/labels"
)

func testRows() []listRow {
	return []listRow{{
		"id":        {text: "github.com/eyelock/assistants/desk"},
		"kind":      {text: "git"},
		"vendor":    {text: "claude"},
		"source":    {text: "eyelock/assistants/harnesses/desk"},
		"artifacts": {text: "3s 1a"},
		"includes": {
			text:  "eyelock/assistants/plugins/wheelhouse, eyelock/assistants/plugins/other, example/skills [2]",
			items: []string{"eyelock/assistants/plugins/wheelhouse", "eyelock/assistants/plugins/other", "example/skills [2]"},
		},
		"delegates": {text: "-", items: []string{}},
		"labels":    {text: "-"},
	}}
}

func lineWidths(out string) (max int) {
	for _, l := range strings.Split(out, "\n") {
		if n := len([]rune(l)); n > max {
			max = n
		}
	}
	return
}

func TestListTable_PipedPrintsEverything(t *testing.T) {
	var b bytes.Buffer
	if err := writeListTable(&b, listColumns, testRows(), 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "example/skills [2]") || strings.Contains(b.String(), "…") {
		t.Errorf("width 0 must print every cell in full:\n%s", b.String())
	}
	// A bytes.Buffer is not a terminal.
	if w := terminalWidth(&b); w != 0 {
		t.Errorf("terminalWidth of a buffer = %d, want 0", w)
	}
}

func TestListTable_TruncationOrderAndCount(t *testing.T) {
	var b bytes.Buffer
	if err := writeListTable(&b, listColumns, testRows(), 150); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if lineWidths(out) > 150 {
		t.Errorf("a line is wider than 150:\n%s", out)
	}
	// INCLUDES shrinks first, ending in the count of what is hidden; the
	// columns that are never cut, and SOURCE, are whole.
	for _, want := range []string{"… (+", "github.com/eyelock/assistants/desk", "claude", "3s 1a", "eyelock/assistants/harnesses/desk"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q:\n%s", want, out)
		}
	}
}

func TestListTable_SourceCutAfterLists(t *testing.T) {
	var b bytes.Buffer
	if err := writeListTable(&b, listColumns, testRows(), 106); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if lineWidths(out) > 106 {
		t.Errorf("a line is wider than 106:\n%s", out)
	}
	if !strings.Contains(out, "github.com/eyelock/assistants/desk") {
		t.Errorf("ID must never be cut:\n%s", out)
	}
	if !strings.Contains(out, "… (+3)") || !strings.Contains(out, "eyelock/assi…") {
		t.Errorf("INCLUDES should be down to the count and SOURCE cut:\n%s", out)
	}
}

func TestFitCell(t *testing.T) {
	c := listCell{text: "a, bb, ccc", items: []string{"a", "bb", "ccc"}}
	for budget, want := range map[int]string{
		10: "a, bb, ccc",
		9:  "a, … (+2)",
		5:  "… (+3)",
	} {
		if got := fitCell(c, budget); got != want {
			t.Errorf("budget %d: %q, want %q", budget, got, want)
		}
	}
	if got := fitCell(listCell{text: "abcdefgh"}, 5); got != "abcd…" {
		t.Errorf("string cut = %q", got)
	}
}

func TestParseListColumns(t *testing.T) {
	cols, err := parseListColumns([]string{"id", "Vendor", "delegates_to", "id"})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, c := range cols {
		keys = append(keys, c.key)
	}
	if strings.Join(keys, ",") != "id,vendor,delegates" {
		t.Errorf("keys = %v", keys)
	}
	if _, err := parseListColumns([]string{"id", "bogus"}); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unknown column should be refused naming it: %v", err)
	}
}

func TestNormalizeRepoAndMatch(t *testing.T) {
	same := []string{"eyelock/assistants", "github.com/eyelock/assistants", "https://github.com/eyelock/assistants",
		"https://github.com/eyelock/assistants.git", "git@github.com:eyelock/assistants.git", "GitHub.com/Eyelock/Assistants/"}
	for _, s := range same {
		if got := normalizeRepo(s, ""); got != "github.com/eyelock/assistants" {
			t.Errorf("normalizeRepo(%q) = %q", s, got)
		}
	}
	cases := []struct {
		candidate, query string
		want             bool
	}{
		{"eyelock/assistants/plugins/wheelhouse", "eyelock/assistants", true},
		{"eyelock/assistants/plugins/wheelhouse", "eyelock/assistants/plugins/wheelhouse", true},
		{"eyelock/assistants/plugins/wheelhouse", "eyelock/assistants/plugins/other", false},
		{"eyelock/assistants", "eyelock/assistants/plugins/wheelhouse", false}, // a subpath narrows
		{"eyelock/assistants", "eyelock/assist", false},                        // not a substring
		{"eyelock/assistants", "eyelock/*", false},                             // not a glob
		{"eyelock/assistants", "gitlab.com/eyelock/assistants", false},
		{"/work/repos/a/sub", "/work/repos/a", true}, // local paths by prefix
		{"/work/repos/ab", "/work/repos/a", false},
	}
	for _, c := range cases {
		got := repoWithin(repoCandidate(c.candidate, "", "/base"), normalizeRepo(c.query, ""))
		if got != c.want {
			t.Errorf("%q within %q = %v, want %v", c.candidate, c.query, got, c.want)
		}
	}
	// An include's relative local path is read against its harness.
	if got := repoCandidate("./extras", "", "/home/h"); got != "path:/home/h/extras" {
		t.Errorf("relative local = %q", got)
	}
}

func TestListFilter_AndAcrossOrWithin(t *testing.T) {
	p := &harness.Harness{
		Labels:        map[string]string{"wheelhouse": "ynw"},
		InstalledFrom: &harness.Provenance{Source: "https://github.com/eyelock/ynw", SourceType: "git"},
		Includes:      []harness.Include{{GitSource: harness.GitSource{Git: "github.com/eyelock/assistants", Path: "plugins/wheelhouse"}}},
		DelegatesTo:   []harness.Delegate{{GitSource: harness.GitSource{Git: "github.com/example/helpers"}}},
	}
	tests := []struct {
		name string
		f    listFilter
		want bool
	}{
		{"none", listFilter{}, true},
		{"source", listFilter{sources: []string{"eyelock/ynw"}}, true},
		{"source other", listFilter{sources: []string{"eyelock/assistants"}}, false},
		{"source or", listFilter{sources: []string{"eyelock/assistants", "eyelock/ynw"}}, true},
		{"includes", listFilter{includes: []string{"eyelock/assistants"}}, true},
		{"includes subpath", listFilter{includes: []string{"eyelock/assistants/plugins/wheelhouse"}}, true},
		{"includes wrong subpath", listFilter{includes: []string{"eyelock/assistants/plugins/x"}}, false},
		{"includes a delegate", listFilter{includes: []string{"example/helpers"}}, true},
		{"and: source and includes", listFilter{sources: []string{"eyelock/ynw"}, includes: []string{"eyelock/assistants"}}, true},
		{"and: one fails", listFilter{sources: []string{"eyelock/ynw"}, includes: []string{"nobody/nothing"}}, false},
		{"and: label and includes", listFilter{labels: labels.Set{"wheelhouse": "ynw"}, includes: []string{"eyelock/assistants"}}, true},
		{"and: wrong label", listFilter{labels: labels.Set{"wheelhouse": "x"}, includes: []string{"eyelock/assistants"}}, false},
	}
	for _, tt := range tests {
		if got := tt.f.matches(p); got != tt.want {
			t.Errorf("%s: matches = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func listTestSetup(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "desk", `{"name":"desk","version":"0.1.0","default_vendor":"claude",
		"includes":[{"git":"github.com/eyelock/assistants","path":"plugins/wheelhouse"}]}`)
	installListTestHarness(t, home, "plain", `{"name":"plain","version":"0.1.0","default_vendor":"claude"}`)
}

func TestCmdList_FiltersTextAndJSON(t *testing.T) {
	listTestSetup(t)

	var text bytes.Buffer
	if err := cmdListTo([]string{"--includes", "https://github.com/eyelock/assistants"}, &text, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "local/desk") || strings.Contains(text.String(), "local/plain") {
		t.Errorf("text should list only desk:\n%s", text.String())
	}

	var raw bytes.Buffer
	if err := cmdListTo([]string{"--format", "json", "--includes", "eyelock/assistants"}, &raw, io.Discard); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Harnesses []struct {
			ID       string `json:"id"`
			Includes []any  `json:"includes"`
		} `json:"harnesses"`
	}
	if err := json.Unmarshal(raw.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Harnesses) != 1 || env.Harnesses[0].ID != "local/desk" {
		t.Errorf("json should hold only desk: %s", raw.String())
	}

	var none bytes.Buffer
	if err := cmdListTo([]string{"--source", "nobody/nothing"}, &none, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(none.String(), "No harnesses match") {
		t.Errorf("no match message: %s", none.String())
	}
}

func TestCmdList_ColumnsAndDashForEmpty(t *testing.T) {
	listTestSetup(t)

	var text bytes.Buffer
	if err := cmdListTo([]string{"--columns", "id,vendor"}, &text, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text.String(), "ID") || strings.Contains(text.String(), "KIND") || strings.Contains(text.String(), "INCLUDES") {
		t.Errorf("only ID and VENDOR expected:\n%s", text.String())
	}

	var full bytes.Buffer
	if err := cmdListTo(nil, &full, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := listTextCell(t, full.String(), "local/plain", "INCLUDES"); got != "-" {
		t.Errorf("empty INCLUDES = %q, want -", got)
	}
	if got := listTextCell(t, full.String(), "local/plain", "DELEGATES TO"); got != "-" {
		t.Errorf("empty DELEGATES TO = %q, want -", got)
	}

	// --columns is text-only: JSON keeps its full shape, an empty list stays one.
	var raw bytes.Buffer
	if err := cmdListTo([]string{"--format", "json", "--columns", "id"}, &raw, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw.String(), `"includes": []`) || !strings.Contains(raw.String(), `"delegates_to": []`) {
		t.Errorf("JSON should keep empty lists:\n%s", raw.String())
	}
}

func TestCmdList_BadFlagValues(t *testing.T) {
	listTestSetup(t)
	for _, args := range [][]string{{"--columns"}, {"--columns", "bogus"}, {"--source"}, {"--includes", " "}} {
		if err := cmdListTo(args, io.Discard, io.Discard); err == nil {
			t.Errorf("%v should be refused", args)
		}
	}
}
