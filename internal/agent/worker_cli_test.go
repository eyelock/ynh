package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/vendor"
)

// shadowVendorCLIs writes, into dir, a failing stub for every vendor CLI that
// dir does not already hold a stub for, so a test that puts dir first on PATH
// cannot fall through to a real install, which would run on the developer's
// account. Without it, a stub named for the wrong binary is silently skipped:
// when the cursor worker moved to "agent", the tests that stubbed "cursor"
// started the real Cursor CLI (#524). "cursor" is the Cursor editor's
// launcher and "cursor-agent" the CLI's older alias.
func shadowVendorCLIs(t *testing.T, dir string) {
	t.Helper()
	names := []string{"cursor", "cursor-agent"}
	for _, v := range vendor.Available() {
		adapter, err := vendor.Get(v)
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, adapter.CLIName())
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		script := "#!/bin/sh\necho 'test stub: " + name + " is a vendor CLI and must not run here' >&2\nexit 97\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// onlyStubOnPath makes PATH a fresh directory holding one stub, named name,
// and nothing else, so no real vendor CLI can be found whatever the code under
// test looks up. The stub uses shell builtins only, since PATH has no tools.
func onlyStubOnPath(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

// Each worker runs the CLI its vendor adapter names, the one `ynh run -v
// <backend>` launches, and no other. With only that binary on PATH, every
// backend must start. The cursor worker used to look up "cursor", the Cursor
// editor's launcher, while Cursor's CLI is "agent" (#524).
func TestBackendsRunTheirVendorAdaptersCLI(t *testing.T) {
	for _, name := range []string{"claude", "codex", "cursor"} {
		t.Run(name, func(t *testing.T) {
			adapter, err := vendor.Get(name)
			if err != nil {
				t.Fatal(err)
			}
			onlyStubOnPath(t, adapter.CLIName(), "exit 0\n")

			backend, err := selectBackend(name)
			if err != nil {
				t.Fatal(err)
			}
			sess, err := backend.Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
			if err != nil {
				t.Fatalf("Start with only %q on PATH: %v", adapter.CLIName(), err)
			}
			_ = sess.Close()
		})
	}
}

// A backend whose CLI is missing names the binary it looked for, so the
// operator knows what to install.
func TestBackendStartNamesTheMissingCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := (&CursorBackend{}).Start(context.Background(), StartOptions{})
	if err == nil {
		t.Fatal("Start must fail when the CLI is not on PATH")
	}
	if !strings.Contains(err.Error(), `"agent"`) {
		t.Errorf("error = %q, want it to name the \"agent\" binary", err)
	}
}

// The cursor worker invokes Cursor's CLI with its documented headless flags
// (cursor.com/docs/cli/reference/parameters). The binary is the CLI itself,
// so there is no leading "agent" subcommand: that word belonged to the editor
// launcher's "cursor agent" form, and Cursor's CLI would take it as part of
// the prompt.
func TestCursorSession_InvokesTheAgentCLI(t *testing.T) {
	records := t.TempDir()
	result := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}` + "\n" +
		`{"type":"result","subtype":"success"}`
	onlyStubOnPath(t, "agent",
		"n=0; while [ -e "+records+"/args$n ]; do n=$((n+1)); done\n"+
			"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > "+records+"/args$n\n"+
			"printf '%s\\n' '"+strings.ReplaceAll(result, "\n", "' '")+"'\n")

	work := t.TempDir()
	sess, err := (&CursorBackend{}).Start(context.Background(), StartOptions{
		WorktreeDir: work, Model: "m1", AutoApprove: AutoApproveAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()

	for _, msg := range []string{"first task", "second task"} {
		if err := sess.Send(msg); err != nil {
			t.Fatal(err)
		}
		turn, err := sess.Next()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		if turn.Content != "done" {
			t.Errorf("content = %q, want %q", turn.Content, "done")
		}
	}

	base := []string{"--print", "--output-format", "stream-json", "--trust", "--workspace", work, "--model", "m1", "--force"}
	if got, want := readLines(t, filepath.Join(records, "args0")), append(slices.Clone(base), "first task"); !slices.Equal(got, want) {
		t.Errorf("first turn args = %q, want %q", got, want)
	}
	want := append(slices.Clone(base), "--resume", sess.ResumeToken(), "second task")
	if got := readLines(t, filepath.Join(records, "args1")); !slices.Equal(got, want) {
		t.Errorf("second turn args = %q, want %q", got, want)
	}
}
