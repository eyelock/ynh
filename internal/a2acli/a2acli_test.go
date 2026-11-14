package a2acli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteSkillsWritesThePinnedSkillAndTheCompanion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	if err := WriteSkills(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, SkillName, "SKILL.md"))
	if err != nil || string(got) != string(Skill()) {
		t.Fatalf("a2a-cli skill not written unchanged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, SkillName, "LICENSE")); err != nil {
		t.Error("the upstream licence must travel with the skill")
	}
	comp, err := os.ReadFile(filepath.Join(dir, CompanionName, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: " + CompanionName, "YNH_DELEGATE_<NAME>_CARD", "YNH_DELEGATE_<NAME>_AUTH", "A2ACLI_AGENT_CARD", "--async", "task get", "--task-id", "Never invent an id", "Never print"} {
		if !strings.Contains(string(comp), want) {
			t.Errorf("companion skill lacks %q", want)
		}
	}
}

func TestVendoredSkillIsThePinnedOne(t *testing.T) {
	s := string(Skill())
	if !strings.Contains(s, `version: "`+SkillVersion+`"`) || !strings.Contains(s, "name: a2a-cli") {
		t.Error("the vendored skill is not the pinned version")
	}
	up, err := files.ReadFile("vendored/a2a-cli/UPSTREAM")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{SkillCommit, SkillVersion, CLIVersion} {
		if !strings.Contains(string(up), want) {
			t.Errorf("UPSTREAM does not record %s", want)
		}
	}
}

func TestCheckSkill(t *testing.T) {
	dir := t.TempDir()
	if CheckSkill(dir) != SkillMissing {
		t.Error("an empty folder is missing the skill")
	}
	if err := WriteSkills(dir); err != nil {
		t.Fatal(err)
	}
	if CheckSkill(dir) != SkillCurrent {
		t.Error("a freshly written skill is current")
	}
	if err := os.WriteFile(filepath.Join(dir, SkillName, "SKILL.md"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if CheckSkill(dir) != SkillDiffers {
		t.Error("an edited skill differs from the pin")
	}
}

func TestVersions(t *testing.T) {
	if v, ok := ParseVersion("a2a version 0.4.0 (commit abc)"); !ok || v != "0.4.0" {
		t.Errorf("ParseVersion = %q %v", v, ok)
	}
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.3.9", "0.4.0", true}, {"0.4.0", "0.4.0", false}, {"0.10.0", "0.4.0", false}, {"0.4.1", "0.4.0", false}} {
		if Older(c.a, c.b) != c.want {
			t.Errorf("Older(%s,%s) != %v", c.a, c.b, c.want)
		}
	}
}

func TestCredentialHelperAnswersEachEndpointWithItsOwnToken(t *testing.T) {
	path, cleanup, err := WriteCredentialHelper([]Credential{
		{Card: "http://127.0.0.1:5068", Token: "tok-short"},
		{Card: "http://127.0.0.1:50682", Token: "tok-long"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if st, _ := os.Stat(path); st == nil || st.Mode().Perm() != 0o700 {
		t.Errorf("the helper must be 0700, got %v", st)
	}
	run := func(arg, in string) string {
		cmd := exec.Command(path, arg)
		cmd.Stdin = strings.NewReader(in)
		out, _ := cmd.CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	for _, c := range []struct{ in, want string }{
		{`{"uri":"http://127.0.0.1:5068"}`, `{"headers":{"Authorization":["Bearer tok-short"]}}`},
		{`{"uri":"http://127.0.0.1:5068/x"}`, `{"headers":{"Authorization":["Bearer tok-short"]}}`},
		{`{"uri":"http://127.0.0.1:50682"}`, `{"headers":{"Authorization":["Bearer tok-long"]}}`},
		{`{"uri":"http://127.0.0.1:506"}`, `{}`},
		{`{"uri":"https://example.com"}`, `{}`},
	} {
		if got := run("get", c.in); got != c.want {
			t.Errorf("helper get %s = %s, want %s", c.in, got, c.want)
		}
	}
	if got := run("store", `{}`); got != "" {
		t.Errorf("a verb other than get must fail quietly, got %q", got)
	}
	cleanup()
	if _, err := os.Stat(path); err == nil {
		t.Error("cleanup left the helper behind")
	}
}

func TestLiveCompanionListsEachCardAddressAndNeedsNoVariable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills")
	if err := WriteSkills(dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteCompanion(dir, []Delegate{{"alpha", "http://127.0.0.1:1"}, {"beta", "http://127.0.0.1:2"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, CompanionName, "SKILL.md"))
	got := string(b)
	for _, want := range []string{"| `alpha` | `http://127.0.0.1:1` |", "| `beta` | `http://127.0.0.1:2` |", "a2a send -a <card address> --async", "Do not pass `--auth`", "--task-id"} {
		if !strings.Contains(got, want) {
			t.Errorf("live companion lacks %q", want)
		}
	}
	if strings.Contains(got, "A2ACLI_AUTH=") || strings.Contains(got, "{{REACH}}") {
		t.Errorf("live companion teaches a variable prefix or leaves the placeholder:\n%s", got)
	}
	if strings.Contains(string(Companion()), "{{REACH}}") {
		t.Error("the generic companion leaves the placeholder")
	}
}
