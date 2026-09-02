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

// dispatchedCommands reads the command names out of main's dispatch switch.
//
// Parsed from source rather than hand-listed on purpose. A hand-written list
// is a second thing to update, and the failure it is meant to catch — a
// command added to dispatch and nowhere else — is exactly the failure that
// would leave the list stale. `ynh help` already drifted this way: `migrate`
// and `quarantine` were dispatched for releases while the usage text never
// mentioned them.
func dispatchedCommands(t *testing.T) []string {
	t.Helper()
	// Resolved from this file's own location, not the working directory:
	// other tests in this package chdir, and a relative path here would read
	// whatever directory happened to be current.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate main.go")
	}
	src, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	// The dispatch switch runs from `switch os.Args[1] {` to its closing brace.
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
			// --version and --help are flag spellings of version and help,
			// not commands a user asks for help about.
			if name == "" || strings.HasPrefix(name, "-") || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	if len(out) < 25 {
		t.Fatalf("parsed only %d commands from the dispatch switch; the parser is wrong, "+
			"not the code: %v", len(out), out)
	}
	return out
}

// Every dispatched command must have help. This is the check that stops the
// drift `ynh help` already suffered.
func TestEveryDispatchedCommandHasHelp(t *testing.T) {
	for _, name := range dispatchedCommands(t) {
		if _, ok := commandHelp[canonicalCommand(name)]; !ok {
			t.Errorf("command %q is dispatched but has no help entry in commandHelp", name)
		}
	}
}

// And nothing may have help that is not a command — a stale entry is a
// promise about a command that no longer exists.
func TestNoOrphanHelpTopics(t *testing.T) {
	dispatched := map[string]bool{}
	for _, name := range dispatchedCommands(t) {
		dispatched[canonicalCommand(name)] = true
	}
	for _, topic := range helpTopics() {
		if !dispatched[topic] {
			t.Errorf("commandHelp has %q, which is not in the dispatch switch", topic)
		}
	}
}

// Help must name the command it describes, so a reader can tell they got the
// page they asked for.
func TestHelpNamesItsCommand(t *testing.T) {
	for _, topic := range helpTopics() {
		var buf bytes.Buffer
		if !printCommandHelp(&buf, topic) {
			t.Fatalf("printCommandHelp(%q) reported unknown", topic)
		}
		want := "ynh " + topic
		if !strings.Contains(buf.String(), want) {
			t.Errorf("help for %q never mentions %q:\n%s", topic, want, buf.String())
		}
	}
}

func TestAliasesResolveToCanonicalHelp(t *testing.T) {
	for alias, canonical := range commandAliases {
		var aliasBuf, canonBuf bytes.Buffer
		if !printCommandHelp(&aliasBuf, alias) {
			t.Errorf("alias %q has no help", alias)
			continue
		}
		if !printCommandHelp(&canonBuf, canonical) {
			t.Errorf("canonical %q has no help", canonical)
			continue
		}
		if aliasBuf.String() != canonBuf.String() {
			t.Errorf("alias %q and %q print different help", alias, canonical)
		}
	}
}

func TestWantsHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"bare --help", []string{"--help"}, true},
		{"bare -h", []string{"-h"}, true},
		{"after a positional", []string{"myharness", "--help"}, true},
		{"among flags", []string{"-v", "codex", "--help"}, true},
		{"absent", []string{"myharness", "-v", "codex"}, false},
		{"no args", nil, false},
		// The separator is the user saying what follows is not for ynh.
		{"after --", []string{"myharness", "--", "--help"}, false},
		{"-- alone", []string{"--"}, false},
		// Not a flag spelling ynh recognises.
		{"substring", []string{"--helpful"}, false},
		{"help as a value", []string{"--focus", "--help"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wantsHelp(c.args); got != c.want {
				t.Errorf("wantsHelp(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

// The defect this whole file exists for: asking a command what it does must
// never make it do it.
//
// `ynh prune --help` used to delete run directories and `ynh run <x> --help`
// used to launch a vendor session. This asserts the interception happens
// before any command sees its arguments, by proving the flag is recognised for
// every dispatched command — including the four that used to act on it.
func TestHelpIsRecognisedForEveryDispatchedCommand(t *testing.T) {
	for _, name := range dispatchedCommands(t) {
		for _, flag := range []string{"--help", "-h"} {
			if !wantsHelp([]string{flag}) {
				t.Fatalf("wantsHelp(%q) is false; interception cannot work", flag)
			}
			var buf bytes.Buffer
			if !printCommandHelp(&buf, name) {
				t.Errorf("%s %s would fall through to the command", name, flag)
			}
			if buf.Len() == 0 {
				t.Errorf("%s %s printed nothing", name, flag)
			}
		}
	}
}

// A stronger version of the same claim, for the commands that used to act:
// run the real binary path through an isolated YNH_HOME and assert the
// filesystem is untouched.
//
// The four that executed were prune, run, search and init. prune is the one
// that deleted; it is included here rather than in a mutation test, because
// this only ever calls the help path — the deleting code is never reached,
// which is the property under test.
func TestHelpTouchesNothingOnDisk(t *testing.T) {
	for _, name := range []string{"prune", "run", "search", "init"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("YNH_HOME", home)

			// A run directory of the kind `ynh prune` removes.
			runDir := filepath.Join(home, "run", "_inline-deadbeef")
			if err := os.MkdirAll(runDir, 0o755); err != nil {
				t.Fatal(err)
			}
			canary := filepath.Join(runDir, "canary.txt")
			if err := os.WriteFile(canary, []byte("must survive"), 0o644); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, home)

			var buf bytes.Buffer
			if !wantsHelp([]string{"--help"}) || !printCommandHelp(&buf, name) {
				t.Fatalf("%s --help did not resolve to help", name)
			}

			if after := snapshot(t, home); after != before {
				t.Errorf("%s --help changed the filesystem\nbefore:\n%s\nafter:\n%s",
					name, before, after)
			}
			if _, err := os.Stat(canary); err != nil {
				t.Errorf("%s --help removed the canary: %v", name, err)
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

// An unknown command with --help must not print a help page for something
// else, and must not exit 0.
func TestUnknownCommandHasNoHelp(t *testing.T) {
	var buf bytes.Buffer
	if printCommandHelp(&buf, "nosuchcommand") {
		t.Error("printCommandHelp claimed to know an unknown command")
	}
	if buf.Len() != 0 {
		t.Errorf("printCommandHelp wrote output for an unknown command: %q", buf.String())
	}
	msg := helpForUnknown("nosuchcommand")
	if !strings.Contains(msg, "nosuchcommand") || !strings.Contains(msg, "ynh help") {
		t.Errorf("unknown-command message is unhelpful: %q", msg)
	}
}

// Every dispatched command must also appear in the global usage page.
//
// `migrate` and `quarantine` were dispatched and functional for releases while
// `ynh help` never mentioned them, so the binary advertised 29 commands and had
// 31. Nothing failed, because nothing checked. This is that check — and it is
// derived from the dispatch switch, so a command added tomorrow is covered
// without anyone remembering to update a list.
func captureUsage(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	printUsageTo(&buf)
	return buf.String()
}

func TestEveryDispatchedCommandAppearsInUsage(t *testing.T) {
	usage := captureUsage(t)
	for _, name := range dispatchedCommands(t) {
		if canonicalCommand(name) != name {
			continue // aliases need not be listed separately
		}
		// Match the command at the start of a usage line, so "install" is not
		// satisfied by the word appearing inside another command's blurb.
		//
		// Two or more spaces: commands are indented under an intent heading
		// ("Getting a harness", "Gating"), and the exact depth is presentation.
		// What must hold is that the name begins a line rather than appearing
		// mid-sentence — anchoring to a fixed indent would make this fail on a
		// reformat while missing the drift it exists to catch.
		re := regexp.MustCompile(`(?m)^ {2,}` + regexp.QuoteMeta(name) + `\b`)
		if !re.MatchString(usage) {
			t.Errorf("command %q is dispatched but never listed in `ynh help`", name)
		}
	}
}
