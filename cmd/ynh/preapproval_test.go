//go:build unix

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/vendor"
)

// Pre-approval follows trust (ynh #952): MCP tool approval, Copilot's one-shot
// approval of every tool and --auto-approve are given only to a harness the
// gate vouched for.

const mcpCodexHarness = `{"name":"mcp","version":"0.1.0","default_vendor":"codex",` +
	`"mcp_servers":{"web":{"url":"https://x/mcp"}}}`

const mcpCopilotHarness = `{"name":"cop","version":"0.1.0","default_vendor":"copilot",` +
	`"mcp_servers":{"web":{"url":"https://x/mcp"}}}`

func TestGateTrusted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	accepted := installTrustedTestHarness(t, home, "mcp", mcpCodexHarness)
	unaccepted := installListTestHarness(t, home, "mcp2", strings.Replace(mcpCodexHarness, `"mcp"`, `"mcp2"`, 1))
	plain := installListTestHarness(t, home, "plain", plainHarness)

	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"accepted install", accepted, true},
		{"unaccepted install", unaccepted, false},
		{"install that executes nothing", plain, true},
		{"folder run", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := loadInstalled(t, accepted)
			if tc.id != "" {
				p = loadInstalled(t, tc.id)
			}
			if got := gateTrusted(tc.id, p); got != tc.want {
				t.Errorf("gateTrusted = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("accepted then changed", func(t *testing.T) {
		installListTestHarness(t, home, "mcp", strings.Replace(mcpCodexHarness, "x/mcp", "evil/mcp", 1))
		if gateTrusted(accepted, loadInstalled(t, accepted)) {
			t.Error("a harness changed since acceptance is trusted")
		}
	})
}

// captureLaunch stands in for the vendor launch and records the arguments the
// vendor would get.
func captureLaunch(t *testing.T) *[]string {
	t.Helper()
	var got []string
	stubLaunchArgs(t, func(args []string) error { got = args; return nil })
	return &got
}

func stubLaunchArgs(t *testing.T, fn func(args []string) error) {
	t.Helper()
	old := launchSessionFn
	t.Cleanup(func() { launchSessionFn = old })
	launchSessionFn = func(_ vendor.Adapter, _, _ string, _ runArgs, _, _ string, vendorArgs []string, _ io.Writer) error {
		return fn(vendorArgs)
	}
}

const approveMCP = `mcp_servers.web.default_tools_approval_mode="approve"`

func TestRunOneShotMCPApprovalFollowsTrust(t *testing.T) {
	t.Run("accepted install is pre-approved", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		t.Setenv("CODEX_HOME", t.TempDir())
		home := os.Getenv("YNH_HOME")
		id := installTrustedTestHarness(t, home, "mcp", mcpCodexHarness)
		args := captureLaunch(t)
		if err := cmdRun([]string{id, "--", "hi"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(*args, "\n"), approveMCP) {
			t.Errorf("no MCP approval in %q", *args)
		}
	})

	t.Run("accepted install run interactively is not pre-approved", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		t.Setenv("CODEX_HOME", t.TempDir())
		home := os.Getenv("YNH_HOME")
		id := installTrustedTestHarness(t, home, "mcp", mcpCodexHarness)
		args := captureLaunch(t)
		if err := cmdRun([]string{id, "--interactive", "--", "hi"}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(*args, "\n"), approveMCP) {
			t.Errorf("interactive run approved MCP tools: %q", *args)
		}
	})

	t.Run("folder run gets none", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		t.Setenv("CODEX_HOME", t.TempDir())
		dir := filepath.Join(t.TempDir(), "mcp")
		writeSkillHarness(t, dir, "mcp")
		if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(mcpCodexHarness), 0o644); err != nil {
			t.Fatal(err)
		}
		args := captureLaunch(t)
		if err := cmdRun([]string{dir, "--install", "--", "hi"}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(*args, "\n"), "default_tools_approval_mode") {
			t.Errorf("a folder run got MCP approval: %q", *args)
		}
	})
}

func TestRunOneShotNoteOnlyWithMCPServers(t *testing.T) {
	run := func(t *testing.T, name, manifest string) string {
		isolateRun(t)
		t.Chdir(t.TempDir())
		t.Setenv("CODEX_HOME", t.TempDir())
		dir := filepath.Join(t.TempDir(), name)
		writeSkillHarness(t, dir, name)
		if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		captureLaunch(t)
		// Codex needs its symlinks consented to once; --install records it.
		if err := cmdRun([]string{dir, "-v", "codex", "--install", "--", "hi"}); err != nil {
			t.Fatal(err)
		}
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stderr
		os.Stderr = w
		runErr := cmdRun([]string{dir, "-v", "codex", "--trust", "--", "hi"})
		os.Stderr = old
		_ = w.Close()
		out, _ := io.ReadAll(r)
		if runErr != nil {
			t.Fatal(runErr)
		}
		return string(out)
	}
	t.Run("harness with servers is told", func(t *testing.T) {
		if out := run(t, "mcp", mcpCodexHarness); !strings.Contains(out, "MCP tools are not pre-approved") {
			t.Errorf("no note in %q", out)
		}
	})
	t.Run("harness with no servers is not", func(t *testing.T) {
		if out := run(t, "nomcp", `{"name":"nomcp","version":"0.1.0","default_vendor":"codex"}`); strings.Contains(out, "not pre-approved") {
			t.Errorf("note printed with no MCP servers: %q", out)
		}
	})
}

func TestRunCopilotOneShotFollowsTrust(t *testing.T) {
	t.Run("folder run is refused before launch", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		dir := filepath.Join(t.TempDir(), "cop")
		writeSkillHarness(t, dir, "cop")
		launched := false
		stubLaunchArgs(t, func([]string) error { launched = true; return nil })
		err := cmdRun([]string{dir, "-v", "copilot", "--", "hi"})
		if !isUntrusted(err) {
			t.Fatalf("err = %v, want an untrusted refusal", err)
		}
		if launched {
			t.Error("copilot was launched")
		}
		if !strings.Contains(err.Error(), "--trust") {
			t.Errorf("refusal does not say why: %v", err)
		}
	})

	t.Run("folder run with --trust opens at the level on copilot", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		dir := filepath.Join(t.TempDir(), "cop")
		writeSkillHarness(t, dir, "cop")
		var got []string
		stubLaunchArgs(t, func(a []string) error { got = a; return nil })
		if err := cmdRun([]string{dir, "-v", "copilot", "--trust", "--auto-approve", "edits", "--", "hi"}); err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, " ") != "--no-ask-user --allow-tool write --deny-tool shell" {
			t.Errorf("vendor args = %q", got)
		}
	})

	t.Run("unaccepted install is refused by the gate before launch", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		id := installListTestHarness(t, os.Getenv("YNH_HOME"), "cop", mcpCopilotHarness)
		launched := false
		stubLaunchArgs(t, func([]string) error { launched = true; return nil })
		if err := cmdRun([]string{id, "--", "hi"}); !isUntrusted(err) {
			t.Fatalf("err = %v, want an untrusted refusal", err)
		}
		if launched {
			t.Error("copilot was launched")
		}
	})

	t.Run("accepted install keeps one-shot", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		id := installTrustedTestHarness(t, os.Getenv("YNH_HOME"), "cop", mcpCopilotHarness)
		launched := false
		stubLaunchArgs(t, func([]string) error { launched = true; return nil })
		if err := cmdRun([]string{id, "--", "hi"}); err != nil {
			t.Fatal(err)
		}
		if !launched {
			t.Error("a trusted one-shot copilot run did not launch")
		}
	})

	t.Run("folder run interactive is not refused", func(t *testing.T) {
		isolateRun(t)
		t.Chdir(t.TempDir())
		dir := filepath.Join(t.TempDir(), "cop")
		writeSkillHarness(t, dir, "cop")
		stubLaunchArgs(t, func([]string) error { return nil })
		if err := cmdRun([]string{dir, "-v", "copilot", "--interactive", "--", "hi"}); err != nil {
			t.Fatalf("an interactive folder run was refused: %v", err)
		}
	})
}
