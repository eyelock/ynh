package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDetectVendorCLI(t *testing.T) {
	tests := []struct {
		name      string
		available map[string]bool // binary name → available
		want      string
	}{
		{
			name:      "claude available",
			available: map[string]bool{"claude": true},
			want:      "claude",
		},
		{
			name:      "codex available",
			available: map[string]bool{"codex": true},
			want:      "codex",
		},
		{
			name:      "cursor agent available",
			available: map[string]bool{"agent": true},
			want:      "cursor",
		},
		{
			name:      "claude preferred over codex",
			available: map[string]bool{"claude": true, "codex": true},
			want:      "claude",
		},
		{
			name:      "claude preferred over cursor",
			available: map[string]bool{"claude": true, "agent": true},
			want:      "claude",
		},
		{
			name:      "codex preferred over cursor",
			available: map[string]bool{"codex": true, "agent": true},
			want:      "codex",
		},
		{
			name:      "copilot available",
			available: map[string]bool{"copilot": true},
			want:      "copilot",
		},
		{
			name:      "cursor preferred over copilot",
			available: map[string]bool{"agent": true, "copilot": true},
			want:      "cursor",
		},
		{
			name:      "the Cursor editor's launcher is not Cursor's CLI",
			available: map[string]bool{"cursor": true},
			want:      "",
		},
		{
			name:      "none available",
			available: map[string]bool{},
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origLookPath := lookPathFunc
			t.Cleanup(func() { lookPathFunc = origLookPath })

			lookPathFunc = func(name string) (string, error) {
				if tt.available[name] {
					return "/mock/" + name, nil
				}
				return "", fmt.Errorf("not found")
			}

			got := detectVendorCLI()
			if got != tt.want {
				t.Errorf("detectVendorCLI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQueryLLMImpl_UnsupportedVendor(t *testing.T) {
	_, err := queryLLMImpl("unknown", "test prompt")
	if err == nil {
		t.Fatal("expected error for unsupported vendor")
	}
	want := `unsupported vendor "unknown"`
	if !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// queryLLMImpl runs each vendor's CLI, by the name its vendor adapter gives,
// in that CLI's documented one-shot form: the prompt on stdin, the answer as
// text on stdout. PATH holds only the stub, so no real CLI can run.
func TestQueryLLMImpl_RunsEachVendorsCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	tests := []struct {
		vendor, binary string
		args           []string
	}{
		{"claude", "claude", []string{"-p", "-", "--output-format", "text"}},
		{"codex", "codex", []string{"exec", "--skip-git-repo-check", "-"}},
		{"cursor", "agent", []string{"-p", "-"}},
		{"copilot", "copilot", []string{"--no-auto-update", "-s", "--no-ask-user"}},
	}
	for _, tt := range tests {
		t.Run(tt.vendor, func(t *testing.T) {
			records := t.TempDir()
			bin := t.TempDir()
			// Shell builtins only: PATH has nothing else on it.
			script := "#!/bin/sh\n" +
				"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > " + records + "/args\n" +
				"while IFS= read -r l; do printf '%s\\n' \"$l\"; done > " + records + "/stdin\n" +
				"echo ' the answer '\n"
			if err := os.WriteFile(filepath.Join(bin, tt.binary), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)

			got, err := queryLLMImpl(tt.vendor, "the prompt\nsecond line\n")
			if err != nil {
				t.Fatalf("queryLLMImpl: %v", err)
			}
			if got != "the answer" {
				t.Errorf("answer = %q, want %q", got, "the answer")
			}
			args, err := os.ReadFile(filepath.Join(records, "args"))
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.Join(tt.args, "\n") + "\n"; string(args) != want {
				t.Errorf("args = %q, want %q", args, want)
			}
			stdin, err := os.ReadFile(filepath.Join(records, "stdin"))
			if err != nil {
				t.Fatal(err)
			}
			if string(stdin) != "the prompt\nsecond line\n" {
				t.Errorf("stdin = %q, want the prompt", stdin)
			}
		})
	}
}

// An explicit -v names a vendor, not a binary: `-v cursor` must find Cursor's
// CLI, "agent", where it used to look for "cursor", the editor's launcher.
func TestCmdCompress_ExplicitVendorFindsItsCLI(t *testing.T) {
	var asked []string
	mockLLM(t, func(vendor, prompt string) (string, error) {
		asked = append(asked, vendor)
		return "Compressed.", nil
	})
	lookPathFunc = func(name string) (string, error) {
		if name == "agent" {
			return "/mock/agent", nil
		}
		return "", fmt.Errorf("%s not found", name)
	}

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))
	srcFile := filepath.Join(dir, "test.md")
	writeFile(t, srcFile, []byte("Very verbose content that needs compression.\n"))

	if err := cmdCompress([]string{"-v", "cursor", "-y", srcFile}); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "cursor" {
		t.Errorf("LLM queried as %q, want cursor once", asked)
	}
}

func TestCmdInspect_ExplicitVendorFindsItsCLI(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) {
		if name == "agent" {
			return "/mock/agent", nil
		}
		return "", fmt.Errorf("%s not found", name)
	}
	t.Chdir(t.TempDir())

	if err := cmdInspect([]string{"-v", "cursor"}); err != nil {
		t.Fatal(err)
	}
}

// A known vendor whose CLI is missing names the binary that was looked for.
func TestLookLLMCLI_NamesTheMissingBinary(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) { return "", fmt.Errorf("%s not found", name) }

	err := checkLLMCLI("cursor")
	if err == nil || !strings.Contains(err.Error(), `"agent"`) || !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("err = %v, want it to name \"agent\" as not found on PATH", err)
	}
	if err := checkLLMCLI("nonexistent-vendor-xyz"); err == nil || !strings.Contains(err.Error(), "unsupported vendor") {
		t.Errorf("err = %v, want unsupported vendor", err)
	}
}

// The help printed when no CLI is found lists every vendor ynd can use, under
// the binary to install.
func TestPrintNoLLMCLI_ListsEveryVendor(t *testing.T) {
	var buf strings.Builder
	printNoLLMCLI(&buf, "Compression requires an LLM.", "compress")
	out := buf.String()
	if !strings.Contains(out, "(checked: claude, codex, cursor, copilot)") {
		t.Errorf("help does not list every vendor:\n%s", out)
	}
	for _, line := range []string{"cursor   agent", "copilot  copilot", "ynd compress -v claude"} {
		if !strings.Contains(out, line) {
			t.Errorf("help lacks %q:\n%s", line, out)
		}
	}
}
