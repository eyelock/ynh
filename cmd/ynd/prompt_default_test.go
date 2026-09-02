package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// promptAction returns choices[0] on empty input AND on EOF. That single line
// decides what happens when an operator presses Enter at a prompt, and what
// happens when stdin is a pipe. If the first choice is the one that writes,
// both of those mean "yes".
//
// On 2026-09-01 five call sites listed the writing action first. The worst was
// `ynd compress`, whose prompt read "Apply? [y/N] " while returning "y" on a
// bare Enter, so the operator was told the default was No by a prompt that
// meant Yes, and their source files were overwritten. The rule this breaks is
// written down in .claude/rules/destructive-operations.md; nothing enforced it.
//
// Reviewing the argument order by eye is exactly the check that failed for
// months, so this asserts it mechanically instead.
func TestPromptDefaultsToTheSafeChoice(t *testing.T) {
	// Choices that decline, skip or stop. A prompt may default to any of these
	// because none of them writes, deletes or spends money on an LLM call.
	safe := map[string]string{
		"n": "no",
		"s": "skip",
		"q": "quit",
	}

	// The one prompt allowed to default to something else, named explicitly so
	// adding a second requires saying why here.
	//
	// "[c]ontinue" only redisplays the analysis and asks again; the prompt it
	// leads to defaults to skip. Nothing is written on the way through.
	allowed := map[string]string{
		"Does this look right? [c]ontinue / [r]efine / [q]uit: ": "c",
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok || ident.Name != "promptAction" || len(call.Args) < 2 {
				return true
			}

			lits := make([]string, 0, len(call.Args))
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true // not a literal call; nothing to assert
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				lits = append(lits, v)
			}

			msg, first := lits[0], lits[1]
			pos := fset.Position(call.Pos())
			where := filepath.Base(pos.Filename) + ":" + strconv.Itoa(pos.Line)
			checked++

			if want, ok := allowed[msg]; ok {
				if first != want {
					t.Errorf("%s: prompt %q is allowed to default to %q, got %q",
						where, msg, want, first)
				}
				return true
			}

			if _, ok := safe[first]; !ok {
				t.Errorf("%s: promptAction(%q, ...) defaults to %q on Enter and on EOF.\n"+
					"  The first choice is what an operator gets for pressing Enter, and what a\n"+
					"  pipe gets for saying nothing. List the declining choice first, or add the\n"+
					"  prompt to the allowed map above with a reason.",
					where, msg, first)
			}
			return true
		})
	}

	// A test that parses no call sites passes for the wrong reason. If this
	// package is refactored so the calls are no longer literal, this fails
	// rather than quietly asserting nothing.
	if checked < 8 {
		t.Fatalf("only inspected %d promptAction call sites; expected at least 8. "+
			"If the calls moved or stopped using string literals, this guard is no "+
			"longer checking anything and needs rewriting, not deleting.", checked)
	}
}

// TestPromptActionReturnsFirstChoiceOnEOF pins the behaviour the guard above
// depends on. If promptAction ever stops defaulting to choices[0], the ordering
// rule stops meaning anything and the guard becomes theatre.
func TestPromptActionReturnsFirstChoiceOnEOF(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stdin string
	}{
		{"eof", ""},
		{"bare enter", "\n"},
		{"whitespace then enter", "   \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stdin")
			if err := os.WriteFile(path, []byte(tc.stdin), 0o600); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()

			orig := os.Stdin
			os.Stdin = f
			t.Cleanup(func() { os.Stdin = orig })

			// Silence the prompt itself.
			devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = devnull.Close() }()
			origOut := os.Stdout
			os.Stdout = devnull
			t.Cleanup(func() { os.Stdout = origOut })

			if got := promptActionImpl("Delete it? [y/N] ", "n", "y"); got != "n" {
				t.Errorf("promptActionImpl returned %q, want the first choice %q", got, "n")
			}
		})
	}
}
