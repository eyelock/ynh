package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var flagNameRE = regexp.MustCompile(`-{1,2}[A-Za-z][-A-Za-z0-9]*`)

// parserFuncs parses the non-test sources of this package once and indexes
// every function by name.
func parserFuncs(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	// Resolved from this file's location: other tests in the package chdir.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	funcs := map[string]*ast.FuncDecl{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", e.Name(), err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil {
				funcs[fn.Name.Name] = fn
			}
		}
	}
	return funcs
}

// acceptedFlags returns the flag names a function's `case "--x", "-y":` clauses
// accept. Reading the parser's own source is what keeps the help table from
// drifting away from it.
func acceptedFlags(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(fn, func(n ast.Node) bool {
		cc, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		for _, e := range cc.List {
			lit, ok := e.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			if s, err := strconv.Unquote(lit.Value); err == nil && strings.HasPrefix(s, "-") {
				out[s] = true
			}
		}
		return true
	})
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Every flag a subcommand's parser accepts must be in its help, and every flag
// its help lists must be accepted by the parser (#587).
func TestSubcommandHelpMatchesParsers(t *testing.T) {
	funcs := parserFuncs(t)
	for key, h := range subcommandHelp {
		t.Run(key, func(t *testing.T) {
			if len(h.Parsers) == 0 {
				t.Fatal("no Parsers named: the entry cannot be checked against code")
			}
			parsed := map[string]bool{}
			for _, name := range h.Parsers {
				fn, ok := funcs[name]
				if !ok {
					t.Fatalf("parser %q does not exist", name)
				}
				for f := range acceptedFlags(fn) {
					parsed[f] = true
				}
			}
			documented := map[string]bool{}
			for _, f := range h.Flags {
				names := flagNameRE.FindAllString(f.Flag, -1)
				if len(names) == 0 {
					t.Errorf("flag entry %q names no flag", f.Flag)
				}
				if strings.TrimSpace(f.Desc) == "" {
					t.Errorf("flag %q has no description", f.Flag)
				}
				for _, n := range names {
					documented[n] = true
				}
			}
			for _, f := range sortedKeys(parsed) {
				if !documented[f] {
					t.Errorf("parser accepts %s but help does not list it", f)
				}
			}
			for _, f := range sortedKeys(documented) {
				if !parsed[f] {
					t.Errorf("help lists %s but the parser does not accept it", f)
				}
			}
		})
	}
}

// The key must be spelled the way the group dispatches it, and the usage line
// must name it, so a help page is always the one that was asked for.
func TestSubcommandHelpEntriesAreWellFormed(t *testing.T) {
	for key, h := range subcommandHelp {
		if !strings.HasPrefix(h.Usage, "ynh "+key) {
			t.Errorf("%q: usage %q must start with %q", key, h.Usage, "ynh "+key)
		}
		if strings.TrimSpace(h.Summary) == "" {
			t.Errorf("%q: no summary", key)
		}
		if strings.Contains(h.Usage+h.Summary+h.Notes, "\u2014") {
			t.Errorf("%q: contains an em-dash", key)
		}
		if _, ok := commandHelp[strings.Fields(key)[0]]; !ok {
			t.Errorf("%q: group %q has no command help", key, strings.Fields(key)[0])
		}
	}
}

func TestSubcommandKey(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"include update", []string{"include", "update", "--help"}, "include update"},
		{"short flag", []string{"include", "update", "-h"}, "include update"},
		{"flag after positionals", []string{"include", "add", "h", "u", "--help"}, "include add"},
		{"group only", []string{"include", "--help"}, ""},
		{"nested", []string{"profile", "include", "update", "--help"}, "profile include update"},
		{"nested group only", []string{"profile", "hook", "--help"}, ""},
		{"list spelled out for focus", []string{"focus", "list", "--help"}, "focus ls"},
		{"ls for sources", []string{"sources", "ls", "--help"}, "sources list"},
		{"rm", []string{"backend", "rm", "--help"}, "backend remove"},
		{"harness named like a subcommand", []string{"include", "add", "update", "--help"}, "include add"},
		{"unknown subcommand", []string{"include", "frobnicate", "--help"}, ""},
		{"not a group", []string{"install", "update", "--help"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := subcommandKey(tt.args[0], tt.args[1:]); got != tt.want {
				t.Errorf("subcommandKey(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// `ynh include update --help` must show the flags the issue reports as
// undiscoverable. Before the fix it printed the group summary only.
func TestIncludeUpdateHelpListsItsFlags(t *testing.T) {
	var buf bytes.Buffer
	if !printHelpFor(&buf, []string{"include", "update", "--help"}) {
		t.Fatal("no help printed")
	}
	out := buf.String()
	for _, want := range []string{
		"ynh include update <harness> <url>",
		"Arguments:", "<harness>", "<url>",
		"Flags:", "--path <subdir>", "--pick <items>", "--ref <ref>", "--from-path <subdir>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<add|remove|update>") {
		t.Errorf("printed the group page rather than the subcommand's:\n%s", out)
	}
}

// Naming only the group still gets the group page.
func TestGroupHelpIsUnchangedWithoutASubcommand(t *testing.T) {
	var buf bytes.Buffer
	if !printHelpFor(&buf, []string{"include", "--help"}) {
		t.Fatal("no help printed")
	}
	if !strings.Contains(buf.String(), "<add|remove|update>") {
		t.Errorf("group help lost:\n%s", buf.String())
	}
}

// Every subcommand the group dispatch accepts has a page. The dispatch is read
// from source for the same reason dispatchedCommands is.
func TestEveryGroupSubcommandHasHelp(t *testing.T) {
	funcs := parserFuncs(t)
	groups := map[string]string{
		"include": "cmdIncludeTo", "mcp": "cmdMCPTo", "delegate": "cmdDelegateTo",
		"profile": "cmdProfileTo", "hook": "cmdHookTo", "focus": "cmdFocusTo",
		"sources": "cmdSourcesTo", "registry": "cmdRegistryTo", "backend": "cmdBackendTo",
		"profile hook": "cmdProfileHook", "profile mcp": "cmdProfileMCP", "profile include": "cmdProfileInclude",
	}
	for group, fnName := range groups {
		fn, ok := funcs[fnName]
		if !ok {
			t.Fatalf("dispatcher %s not found", fnName)
		}
		subs := map[string]bool{}
		ast.Inspect(fn, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, _ := strconv.Unquote(lit.Value)
					subs[s] = true
				}
			}
			return true
		})
		if len(subs) == 0 {
			t.Fatalf("no subcommands parsed from %s", fnName)
		}
		for sub := range subs {
			if subcommandKey(strings.Fields(group)[0], append(strings.Fields(group)[1:], sub)) == "" &&
				!isGroupWord(group, sub) {
				t.Errorf("%s %s is dispatched but has no subcommand help", group, sub)
			}
		}
	}
}

// isGroupWord reports whether sub is itself a nested group (profile hook), whose
// own subcommands are checked separately.
func isGroupWord(group, sub string) bool {
	return hasKeyPrefix(group + " " + sub)
}
