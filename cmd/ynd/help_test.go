package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// dispatchedCommands reads command names out of main's dispatch switch.
//
// Parsed from source rather than hand-listed: a hand-written list is a second
// thing to update, and the failure it exists to catch (a command added to
// dispatch and nowhere else) is exactly what would leave the list stale.
func dispatchedCommands(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate main.go")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	start := bytes.Index(src, []byte("switch os.Args[1] {"))
	if start < 0 {
		t.Fatal("could not find the dispatch switch in main.go")
	}
	end := bytes.Index(src[start:], []byte("\n\t}"))
	if end < 0 {
		t.Fatal("could not find the end of the dispatch switch")
	}
	block := string(src[start : start+end])

	re := regexp.MustCompile(`(?m)^\tcase (.+):$`)
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		for _, raw := range strings.Split(m[1], ",") {
			name := strings.Trim(strings.TrimSpace(raw), `"`)
			// --version and --help are flag spellings, not commands someone
			// asks for help about.
			if name == "" || strings.HasPrefix(name, "-") || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	if len(out) < 13 {
		t.Fatalf("parsed only %d commands from the dispatch switch; the parser is wrong, "+
			"not the code: %v", len(out), out)
	}
	return out
}

func TestEveryDispatchedCommandHasHelp(t *testing.T) {
	for _, name := range dispatchedCommands(t) {
		if _, ok := commandHelp[canonicalCommand(name)]; !ok {
			t.Errorf("command %q is dispatched but has no help entry", name)
		}
	}
}

func TestNoOrphanHelpTopics(t *testing.T) {
	dispatched := map[string]bool{}
	for _, n := range dispatchedCommands(t) {
		dispatched[canonicalCommand(n)] = true
	}
	for _, topic := range helpTopics() {
		if !dispatched[topic] {
			t.Errorf("help topic %q matches no dispatched command", topic)
		}
	}
}

// A help page that does not name its own command is one someone pasted from
// elsewhere and forgot to edit.
func TestHelpNamesItsCommand(t *testing.T) {
	for _, topic := range helpTopics() {
		first := firstLine(commandHelp[topic])
		if !strings.HasPrefix(first, "ynd "+topic) {
			t.Errorf("help for %q opens with %q, which does not name the command", topic, first)
		}
	}
}

func TestAliasesResolveToCanonicalHelp(t *testing.T) {
	cases := map[string]string{"--version": "version", "-v": "version", "--help": "help", "-h": "help"}
	for alias, want := range cases {
		if got := canonicalCommand(alias); got != want {
			t.Errorf("canonicalCommand(%q) = %q, want %q", alias, got, want)
		}
		var buf bytes.Buffer
		if !printCommandHelp(&buf, alias) {
			t.Errorf("alias %q resolved to no help", alias)
		}
	}
}

func TestWantsHelp(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--help"}, true},
		{[]string{"-h"}, true},
		{[]string{"file.md", "--help"}, true},
		{[]string{"-n"}, false},
		// Everything after -- belongs to the thing being invoked. `ynd fmt --
		// --help` formats a file called --help; it does not print a page.
		{[]string{"--", "--help"}, false},
		{[]string{"--", "-h"}, false},
		{[]string{"--help", "--"}, true},
	}
	for _, tc := range cases {
		if got := wantsHelp(tc.args); got != tc.want {
			t.Errorf("wantsHelp(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestHelpIsRecognisedForEveryDispatchedCommand(t *testing.T) {
	for _, name := range dispatchedCommands(t) {
		var buf bytes.Buffer
		if !printCommandHelp(&buf, name) {
			t.Errorf("%s --help printed nothing", name)
			continue
		}
		if buf.Len() == 0 {
			t.Errorf("%s --help produced empty output", name)
		}
	}
}

// The reason help is intercepted centrally. `ynh prune --help` used to delete
// run directories; ynd's writing commands are compress, inspect, export and
// marketplace. Asking for documentation must never act.
func TestHelpTouchesNothingOnDisk(t *testing.T) {
	for _, name := range []string{"compress", "inspect", "export", "marketplace", "migrate", "fmt"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("YND_BACKUP_DIR", filepath.Join(root, "backups"))
			t.Chdir(root)

			// Content of the kind these commands rewrite or remove.
			sub := filepath.Join(root, "skills", "demo")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			canary := filepath.Join(sub, "SKILL.md")
			body := []byte("---\nname: demo\ndescription: d\n---\n\n#   ragged   \n")
			if err := os.WriteFile(canary, body, 0o644); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, root)

			var buf bytes.Buffer
			if !wantsHelp([]string{"--help"}) || !printCommandHelp(&buf, name) {
				t.Fatalf("%s --help did not resolve to help", name)
			}

			if after := snapshot(t, root); after != before {
				t.Errorf("%s --help changed the filesystem\nbefore:\n%s\nafter:\n%s",
					name, before, after)
			}
			got, err := os.ReadFile(canary)
			if err != nil {
				t.Fatalf("%s --help removed the canary: %v", name, err)
			}
			if !bytes.Equal(got, body) {
				t.Errorf("%s --help rewrote the canary", name)
			}
		})
	}
}

// snapshot renders every path under root, so any create or delete shows as a
// diff rather than needing a per-file assertion.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var paths []string
	err := filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rErr := filepath.Rel(root, p)
		if rErr != nil {
			return rErr
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(paths)
	return strings.Join(paths, "\n")
}

func TestUnknownCommandHasNoHelp(t *testing.T) {
	var buf bytes.Buffer
	if printCommandHelp(&buf, "nosuchcommand") {
		t.Error("an unknown command must not resolve to a help page")
	}
	if buf.Len() != 0 {
		t.Errorf("nothing should have been written, got %q", buf.String())
	}
}

// Every command in the dispatch switch must appear on the global usage page.
func TestEveryDispatchedCommandAppearsInUsage(t *testing.T) {
	var buf bytes.Buffer
	printUsageTo(&buf)
	page := buf.String()
	for _, name := range dispatchedCommands(t) {
		if !regexp.MustCompile(`(?m)^ {2,}` + regexp.QuoteMeta(name) + `\b`).MatchString(page) {
			t.Errorf("command %q is dispatched but does not appear in the usage page", name)
		}
	}
}
