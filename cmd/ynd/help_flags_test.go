package main

import (
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

// parserFuncsByName parses the non-test sources of this package and indexes
// every function by name.
func parserFuncsByName(t *testing.T) map[string]*ast.FuncDecl {
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

// commandParsers names the functions whose `case "--flag":` clauses implement
// each command's flags. A command with flags the parser accepts and the help
// omits (or the reverse) fails TestCommandHelpMatchesParsers.
var commandParsers = map[string][]string{
	"create":          {"cmdCreate", "cmdCreateHarness"},
	"lint":            {"cmdLint"},
	"validate":        {"cmdValidate"},
	"fmt":             {"cmdFmt"},
	"compress":        {"parseCompressArgs"},
	"inspect":         {"parseInspectArgs"},
	"export":          {"cmdExport"},
	"compose":         {"cmdComposeTo"},
	"preview":         {"cmdPreview"},
	"diff":            {"cmdDiff"},
	"marketplace":     {"cmdMarketplaceBuild"},
	"migrate":         {"cmdMigrate"},
	"validate-output": {"cmdValidateOutputTo"},
	"version":         {"cmdVersionTo"},
}

// helpFlagRE finds the flags a help page lists: a line that begins, after the
// indent, with a flag, optionally followed by ", --long".
var helpFlagRE = regexp.MustCompile(`(?m)^ {2}(-{1,2}[A-Za-z][-A-Za-z0-9]*)(?:, (-{1,2}[A-Za-z][-A-Za-z0-9]*))?`)

func TestCommandHelpMatchesParsers(t *testing.T) {
	funcs := parserFuncsByName(t)
	for _, cmd := range helpTopics() {
		parsers, ok := commandParsers[cmd]
		if !ok {
			if cmd == "help" {
				continue
			}
			t.Errorf("%q has help but no entry in commandParsers", cmd)
			continue
		}
		t.Run(cmd, func(t *testing.T) {
			parsed := map[string]bool{}
			for _, name := range parsers {
				fn, ok := funcs[name]
				if !ok {
					t.Fatalf("parser %q does not exist", name)
				}
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
						s, err := strconv.Unquote(lit.Value)
						// -h/--help is answered centrally; "-" alone and "--"
						// are not flags.
						if err != nil || len(s) < 2 || s == "--" || s == "-h" || s == "--help" || s == "---" {
							continue
						}
						if strings.HasPrefix(s, "-") {
							parsed[s] = true
						}
					}
					return true
				})
			}
			documented := map[string]bool{}
			for _, m := range helpFlagRE.FindAllStringSubmatch(commandHelp[cmd], -1) {
				for _, f := range m[1:] {
					if f != "" && f != "-h" && f != "--help" {
						documented[f] = true
					}
				}
			}
			for _, f := range keysOf(parsed) {
				if !documented[f] {
					t.Errorf("parser accepts %s but help does not list it", f)
				}
			}
			for _, f := range keysOf(documented) {
				if !parsed[f] {
					t.Errorf("help lists %s but the parser does not accept it", f)
				}
			}
		})
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
