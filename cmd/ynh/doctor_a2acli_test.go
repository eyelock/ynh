package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/a2acli"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

// installA2AHarness installs a harness with one A2A delegate (the default
// transport) and the given default vendor.
func installA2AHarness(t *testing.T, name, vendorName string) {
	t.Helper()
	installRaw(t, name, fmt.Sprintf(`{"name":%q,"version":"0.1.0","default_vendor":%q,"delegates_to":[{"git":"github.com/example/reviewer"}]}`, name, vendorName))
}

// installRaw installs a harness from its plugin.json text.
func installRaw(t *testing.T, name, body string) {
	t.Helper()
	for _, dir := range []string{harness.InstalledDir(name), harness.InstalledDirByID("local/" + name)} {
		if err := writePluginJSONFile(dir, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
}

// probeFor is a machine with the a2a CLI at the pin, nothing configured, and
// no run dir yet.
func probeFor(t *testing.T) a2aProbe {
	t.Helper()
	return a2aProbe{
		binary:    func() (bool, string) { return true, a2acli.CLIVersion },
		home:      t.TempDir(),
		cwd:       t.TempDir(),
		skillsDir: func(string, vendor.Adapter) string { return "" },
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorA2ACLI_SilentForAHarnessWithoutA2ADelegates(t *testing.T) {
	fakeHome(t)
	installHarnessWithVendor(t, "plain", "codex")
	p := probeFor(t)
	p.binary = func() (bool, string) { return false, "" }
	if c := checkA2ACLIWith(p); len(c.Findings) != 0 {
		t.Errorf("no A2A delegates, no findings:\n%s", findingsOf(c))
	}
}

func TestDoctorA2ACLI_BinaryMissing(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "opencode")
	p := probeFor(t)
	p.binary = func() (bool, string) { return false, "" }
	c := checkA2ACLIWith(p)
	got := findingsOf(c)
	if c.Status != sevWarn || !strings.Contains(got, "not on PATH") || !strings.Contains(got, "A2A_VERSION=0.4.0") || !strings.Contains(got, "brew") {
		t.Errorf("want a warning that says how to install:\n%s", got)
	}
}

func TestDoctorA2ACLI_BinaryOlderThanThePin(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "opencode")
	p := probeFor(t)
	p.binary = func() (bool, string) { return true, "0.3.2" }
	c := checkA2ACLIWith(p)
	if c.Status != sevWarn || !strings.Contains(findingsOf(c), "0.3.2, older than the 0.4.0") {
		t.Errorf("want an older-version warning:\n%s", findingsOf(c))
	}
}

func TestDoctorA2ACLI_OpenCodeAtThePinIsClean(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "opencode")
	if c := checkA2ACLIWith(probeFor(t)); len(c.Findings) != 0 {
		t.Errorf("OpenCode needs no allow rule:\n%s", findingsOf(c))
	}
}

func TestDoctorA2ACLI_SkillMissingAndDiffering(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "opencode")
	skills := filepath.Join(t.TempDir(), ".opencode", "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	p := probeFor(t)
	p.skillsDir = func(string, vendor.Adapter) string { return skills }
	if got := findingsOf(checkA2ACLIWith(p)); !strings.Contains(got, "is missing from") {
		t.Errorf("want a missing-skill warning:\n%s", got)
	}
	write(t, filepath.Join(skills, "a2a-cli", "SKILL.md"), "tampered")
	if got := findingsOf(checkA2ACLIWith(p)); !strings.Contains(got, "differs from the pin") {
		t.Errorf("want a differs-from-pin warning:\n%s", got)
	}
	if err := a2acli.WriteSkills(skills); err != nil {
		t.Fatal(err)
	}
	if c := checkA2ACLIWith(p); len(c.Findings) != 0 {
		t.Errorf("the pinned skill is clean:\n%s", findingsOf(c))
	}
}

func TestDoctorA2ACLI_CodexSandbox(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "codex")
	p := probeFor(t)
	t.Setenv("CODEX_HOME", filepath.Join(p.home, ".codex"))
	c := checkA2ACLIWith(p)
	got := findingsOf(c)
	for _, want := range []string{"operation not permitted", "no longer sandboxed from localhost", "network_access = true"} {
		if !strings.Contains(got, want) {
			t.Errorf("default sandbox: warning lacks %q:\n%s", want, got)
		}
	}
	cfg := filepath.Join(p.home, ".codex", "config.toml")
	write(t, cfg, "sandbox_mode = \"workspace-write\"\n")
	if !strings.Contains(findingsOf(checkA2ACLIWith(p)), "network_access") {
		t.Error("workspace-write without network_access still warns")
	}
	write(t, cfg, "sandbox_mode = \"workspace-write\"\n\n[sandbox_workspace_write]\nnetwork_access = true\n")
	if c := checkA2ACLIWith(p); len(c.Findings) != 0 {
		t.Errorf("workspace-write with network_access is clean:\n%s", findingsOf(c))
	}
	write(t, cfg, "[sandbox_workspace_write]\nnetwork_access = true\n")
	if len(checkA2ACLIWith(p).Findings) == 0 {
		t.Error("network_access without sandbox_mode = workspace-write still warns")
	}
}

func TestDoctorA2ACLI_ClaudeAllowRule(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "claude")
	p := probeFor(t)
	got := findingsOf(checkA2ACLIWith(p))
	if !strings.Contains(got, "Bash(a2a:*)") || !strings.Contains(got, "denied") {
		t.Errorf("want an allow-rule warning:\n%s", got)
	}
	write(t, filepath.Join(p.cwd, ".claude", "settings.local.json"), `{"permissions":{"allow":["Bash(a2a:*)"]}}`)
	if c := checkA2ACLIWith(p); len(c.Findings) != 0 {
		t.Errorf("the allow rule silences it:\n%s", findingsOf(c))
	}
}

func TestDoctorA2ACLI_CopilotAllowRule(t *testing.T) {
	fakeHome(t)
	installA2AHarness(t, "desk", "copilot")
	p := probeFor(t)
	if got := findingsOf(checkA2ACLIWith(p)); !strings.Contains(got, "shell(a2a:*)") {
		t.Errorf("want an allow-rule warning:\n%s", got)
	}
	write(t, filepath.Join(p.home, ".copilot", "config.json"), `{"allowedTools":["shell(a2a:*)"]}`)
	if c := checkA2ACLIWith(p); len(c.Findings) != 0 {
		t.Errorf("the allow rule silences it:\n%s", findingsOf(c))
	}
}

func TestWriteA2ACLISkills(t *testing.T) {
	adapter, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	run := t.TempDir()
	if err := writeA2ACLISkills(&harness.Harness{Name: "plain"}, adapter, run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(run, adapter.ConfigDir())); err == nil {
		t.Error("a harness without A2A delegates gets no skills")
	}
	h := &harness.Harness{Name: "desk", DelegatesTo: []harness.Delegate{{GitSource: harness.GitSource{Git: "github.com/example/reviewer"}}}}
	if err := writeA2ACLISkills(h, adapter, run); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(run, adapter.ConfigDir(), adapter.ArtifactDirs()["skills"])
	if a2acli.CheckSkill(skills) != a2acli.SkillCurrent {
		t.Error("the vendored skill is not in the vendor's skill folder")
	}
	if _, err := os.Stat(filepath.Join(skills, a2acli.CompanionName, "SKILL.md")); err != nil {
		t.Error("the companion skill is missing")
	}
	// native delegates are not the shell route's.
	n := &harness.Harness{Name: "n", DelegatesTo: []harness.Delegate{{GitSource: harness.GitSource{Git: "github.com/example/r"}, Transport: "native"}}}
	run2 := t.TempDir()
	if err := writeA2ACLISkills(n, adapter, run2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(run2, adapter.ConfigDir())); err == nil {
		t.Error("a native-only harness gets no a2a skills")
	}
}
