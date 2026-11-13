package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorMCPAllow_SilentWithoutA2ADelegates(t *testing.T) {
	fakeHome(t)
	installHarnessWithVendor(t, "plain", "claude")
	if c := checkMCPAllowWith(probeFor(t)); len(c.Findings) != 0 {
		t.Errorf("no A2A delegates, no findings:\n%s", findingsOf(c))
	}
}

func TestDoctorMCPAllow_SilentForNativeTransport(t *testing.T) {
	fakeHome(t)
	body := `{"name":"desk","version":"0.1.0","default_vendor":"claude","delegates_to":[{"git":"github.com/example/reviewer","transport":"native"}]}`
	installRaw(t, "desk", body)
	if c := checkMCPAllowWith(probeFor(t)); len(c.Findings) != 0 {
		t.Errorf("native transport only, no findings:\n%s", findingsOf(c))
	}
}

func TestDoctorMCPAllow_Claude(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "claude")
	p := probeFor(t)
	got := findingsOf(checkMCPAllowWith(p))
	if !strings.Contains(got, "mcp__plugin_desk_ynh-delegates") || !strings.Contains(got, "not seen") {
		t.Errorf("want an allow-rule warning naming the rule:\n%s", got)
	}
	write(t, filepath.Join(p.cwd, ".claude", "settings.json"), `{"permissions":{"allow":["mcp__plugin_other_ynh-delegates"]}}`)
	if len(checkMCPAllowWith(p).Findings) == 0 {
		t.Error("another harness's rule must not silence it")
	}
	write(t, filepath.Join(p.cwd, ".claude", "settings.json"), `{"permissions":{"allow":["mcp__plugin_desk_ynh-delegates"]}}`)
	if c := checkMCPAllowWith(p); len(c.Findings) != 0 {
		t.Errorf("the rule silences it:\n%s", findingsOf(c))
	}
	write(t, filepath.Join(p.cwd, ".claude", "settings.json"), `{"permissions":{"allow":["mcp__plugin_desk_ynh-delegates__delegate_list"]}}`)
	if c := checkMCPAllowWith(p); len(c.Findings) != 0 {
		t.Errorf("a per-tool rule silences it:\n%s", findingsOf(c))
	}
}

func TestDoctorMCPAllow_Copilot(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "copilot")
	p := probeFor(t)
	if got := findingsOf(checkMCPAllowWith(p)); !strings.Contains(got, "--allow-tool=ynh-delegates") {
		t.Errorf("want an allow-rule warning:\n%s", got)
	}
	write(t, filepath.Join(p.home, ".copilot", "config.json"), `{"allowedTools":["ynh-delegates"]}`)
	if c := checkMCPAllowWith(p); len(c.Findings) != 0 {
		t.Errorf("the allow rule silences it:\n%s", findingsOf(c))
	}
}

func TestDoctorMCPAllow_Codex(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "codex")
	p := probeFor(t)
	t.Setenv("CODEX_HOME", filepath.Join(p.home, ".codex"))
	cfg := filepath.Join(p.home, ".codex", "config.toml")
	if c := checkMCPAllowWith(p); len(c.Findings) != 0 {
		t.Errorf("approved by ynh, nothing set: quiet:\n%s", findingsOf(c))
	}
	write(t, cfg, "[mcp_servers.ynh-delegates]\ndefault_tools_approval_mode = \"prompt\"\n")
	if got := findingsOf(checkMCPAllowWith(p)); !strings.Contains(got, `"prompt"`) || !strings.Contains(got, "-c override") {
		t.Errorf("config away from approve warns:\n%s", got)
	}
	write(t, cfg, "[mcp_servers.ynh-delegates]\ndefault_tools_approval_mode = \"approve\"\n")
	if c := checkMCPAllowWith(p); len(c.Findings) != 0 {
		t.Errorf("approve is quiet:\n%s", findingsOf(c))
	}
	write(t, cfg, "[mcp_servers.other]\ndefault_tools_approval_mode = \"prompt\"\n")
	if c := checkMCPAllowWith(p); len(c.Findings) != 0 {
		t.Errorf("another server's mode is not ours:\n%s", findingsOf(c))
	}
	write(t, cfg, "mcp_servers.ynh-delegates.default_tools_approval_mode = \"never\"\n")
	if len(checkMCPAllowWith(p).Findings) == 0 {
		t.Error("the dotted key warns too")
	}
}

func TestDoctorMCPAllow_CodexManualApproval(t *testing.T) {
	fakeHome(t)
	installRaw(t, "desk", `{"name":"desk","version":"0.1.0","default_vendor":"codex","mcp_manual_approval":true,"delegates_to":[{"git":"github.com/example/reviewer"}]}`)
	p := probeFor(t)
	t.Setenv("CODEX_HOME", filepath.Join(p.home, ".codex"))
	if got := findingsOf(checkMCPAllowWith(p)); !strings.Contains(got, "mcp_manual_approval") {
		t.Errorf("manual approval warns:\n%s", got)
	}
	write(t, filepath.Join(p.home, ".codex", "config.toml"), "[mcp_servers.ynh-delegates]\ndefault_tools_approval_mode = \"approve\"\n")
	if c := checkMCPAllowWith(p); len(c.Findings) != 0 {
		t.Errorf("config approves it: quiet:\n%s", findingsOf(c))
	}
}

func TestDoctorMCPAllow_OpenCodeNeedsNothingCursorSaysSo(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "opencode")
	if c := checkMCPAllowWith(probeFor(t)); len(c.Findings) != 0 {
		t.Errorf("OpenCode needs no rule:\n%s", findingsOf(c))
	}
	installA2AHarness(t, "desk", "cursor")
	c := checkMCPAllowWith(probeFor(t))
	if got := findingsOf(c); !strings.Contains(got, "not checked on Cursor") || c.Status == sevWarn {
		t.Errorf("Cursor is a note, not a warning:\n%s", got)
	}
}
