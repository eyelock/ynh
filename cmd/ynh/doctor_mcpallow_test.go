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
	settings := filepath.Join(p.cwd, ".claude", "settings.json")
	const rule = "mcp__plugin_desk_ynh-delegates"
	cases := []struct {
		name, body string
		want       string // "" is quiet
	}{
		{"neither", `{}`, "no allow rule"},
		{"another harness's allow", `{"permissions":{"allow":["mcp__plugin_other_ynh-delegates"]}}`, "no allow rule"},
		{"allow only", `{"permissions":{"allow":["` + rule + `"]}}`, ""},
		{"per-tool allow", `{"permissions":{"allow":["` + rule + `__delegate_list"]}}`, ""},
		{"deny only", `{"permissions":{"deny":["` + rule + `"]}}`, "deny wins"},
		{"both", `{"permissions":{"allow":["` + rule + `"],"deny":["` + rule + `__delegate_list"]}}`, "deny wins"},
		{"another harness's deny", `{"permissions":{"allow":["` + rule + `"],"deny":["mcp__plugin_other_ynh-delegates"]}}`, ""},
	}
	for _, tc := range cases {
		write(t, settings, tc.body)
		got := findingsOf(checkMCPAllowWith(p))
		if tc.want == "" && got != "" && !strings.Contains(got, "no findings") {
			// findingsOf prints nothing for a clean check
			t.Errorf("%s: want quiet:\n%s", tc.name, got)
		}
		if tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s: want %q:\n%s", tc.name, tc.want, got)
		}
	}
	// Allow in one file, deny in another: deny still wins.
	write(t, settings, `{"permissions":{"allow":["`+rule+`"]}}`)
	write(t, filepath.Join(p.cwd, ".claude", "settings.local.json"), `{"permissions":{"deny":["`+rule+`"]}}`)
	if got := findingsOf(checkMCPAllowWith(p)); !strings.Contains(got, "deny wins") {
		t.Errorf("deny in another file wins:\n%s", got)
	}
}

func TestDoctorMCPAllow_ClaudeMalformedSettings(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "claude")
	p := probeFor(t)
	write(t, filepath.Join(p.cwd, ".claude", "settings.json"), `{"permissions": {`)
	c := checkMCPAllowWith(p)
	got := findingsOf(c)
	if !strings.Contains(got, "could not read Claude Code's settings as JSON") || !strings.Contains(got, "no allow rule") {
		t.Errorf("want a note about the file and the missing-rule warning:\n%s", got)
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
