package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// isolateRun points every ynh location at a temp dir and clears the env
// fallbacks cmdRun reads.
func isolateRun(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", filepath.Join(home, ".ynh"))
	t.Setenv("YNH_FOCUS", "")
	t.Setenv("YNH_PROFILE", "")
	t.Setenv("YNH_VENDOR", "")
	t.Setenv("YNH_HARNESS_FILE", "")
}

// writeSkillHarness drops a current-format harness with one skill at dir.
func writeSkillHarness(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"` + name + `","version":"0.1.0","default_vendor":"cursor"}`
	if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: hello\ndescription: A trivial skill.\n---\n\n# hello\n"
	if err := os.WriteFile(filepath.Join(dir, "skills", "hello", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
}

// `ynh run <path>` runs a local harness directory without installing it
// (#448), whichever path spelling is used.
func TestCmdRun_LocalPath(t *testing.T) {
	tests := []struct {
		name string
		ref  func(harnessDir, base string) string
	}{
		{"absolute", func(h, _ string) string { return h }},
		{"dot-slash", func(_, b string) string { return "./" + b }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateRun(t)
			work := t.TempDir()
			harnessDir := filepath.Join(work, "my-dev")
			writeSkillHarness(t, harnessDir, "my-dev")
			t.Chdir(work)

			if err := cmdRun([]string{tt.ref(harnessDir, "my-dev"), "-v", "cursor", "--install"}); err != nil {
				t.Fatalf("ynh run %s: %v", tt.ref(harnessDir, "my-dev"), err)
			}
			if _, err := os.Stat(filepath.Join(work, ".cursor", "skills", "hello", "SKILL.md")); err != nil {
				t.Errorf("the harness's skill was not reachable from the project: %v", err)
			}
			// A local directory has no canonical id: it is assembled like an
			// inline harness, and nothing is installed.
			entries, err := os.ReadDir(filepath.Join(os.Getenv("YNH_HOME"), "run"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "_inline-") {
				t.Errorf("run dirs = %v, want one _inline-*", entries)
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("YNH_HOME"), "harnesses")); err == nil {
				if hs, _ := os.ReadDir(filepath.Join(os.Getenv("YNH_HOME"), "harnesses")); len(hs) > 0 {
					t.Errorf("running a path installed something: %v", hs)
				}
			}
		})
	}
}

// A path is refused for the same reasons every other path-taking command
// refuses it, and the id hint is not offered for something that was a path.
func TestCmdRun_LocalPathRefusals(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  string
	}{
		{
			name: "legacy .harness.json",
			setup: func(t *testing.T, dir string) {
				if err := os.WriteFile(filepath.Join(dir, plugin.HarnessFile), []byte(`{"name":"old","version":"0.1.0"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: "uses the legacy .harness.json manifest, which ynh no longer reads; convert it with: ynd migrate ",
		},
		{
			name:  "no manifest",
			setup: func(*testing.T, string) {},
			want:  "no harness at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateRun(t)
			dir := t.TempDir()
			tt.setup(t, dir)
			err := cmdRun([]string{dir, "-v", "cursor", "--install"})
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
			if strings.Contains(err.Error(), "not a valid harness id") {
				t.Errorf("a path got the canonical-id message: %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(dir, ".agents")); statErr == nil {
				t.Error("refusal wrote a manifest into the tree")
			}
		})
	}
}

// --harness-file at a legacy .harness.json is refused, naming ynd migrate
// (#449), and nothing is assembled.
func TestCmdRun_HarnessFileLegacyRefused(t *testing.T) {
	isolateRun(t)
	dir := t.TempDir()
	path := filepath.Join(dir, plugin.HarnessFile)
	if err := os.WriteFile(path, []byte(`{"name":"old","version":"0.1.0","default_vendor":"cursor"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cmdRun([]string{"--harness-file", path, "-v", "cursor", "--install"})
	if err == nil {
		t.Fatal("expected --harness-file at a legacy .harness.json to be refused")
	}
	want := "uses the legacy .harness.json manifest, which ynh no longer reads; convert it with: ynd migrate " + dir
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("YNH_HOME"), "run")); statErr == nil {
		if entries, _ := os.ReadDir(filepath.Join(os.Getenv("YNH_HOME"), "run")); len(entries) > 0 {
			t.Errorf("refused run still assembled: %v", entries)
		}
	}
}

// Each command's rejection hint offers only the forms that command accepts
// (#448): run takes a path, info does not.
func TestBadRefHint_PerCommand(t *testing.T) {
	tests := []struct {
		name     string
		call     func() error
		wantPath bool
	}{
		{"run", func() error { return cmdRun([]string{"my-dev", "-v", "cursor", "--install"}) }, true},
		{"info", func() error { return cmdInfoTo([]string{"my-dev"}, io.Discard, io.Discard) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateRun(t)
			err := tt.call()
			if err == nil {
				t.Fatal("expected a bare name to be rejected")
			}
			msg := err.Error()
			if !strings.Contains(msg, "not a valid harness id") {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.Contains(msg, "./<path>"); got != tt.wantPath {
				t.Errorf("hint offers ./<path> = %v, want %v: %s", got, tt.wantPath, msg)
			}
		})
	}
}

// --harness-file keeps taking a current-format manifest, under any name, and
// the harness's own artifacts come from the tree the manifest belongs to.
func TestCmdRun_HarnessFileCurrentFormat(t *testing.T) {
	tests := []struct {
		name string
		file func(t *testing.T, harnessDir string) string
	}{
		{"plugin.json in its manifest dir", func(_ *testing.T, h string) string {
			return filepath.Join(h, ".agents/harness", "plugin.json")
		}},
		{"single file at the root", func(t *testing.T, h string) string {
			path := filepath.Join(h, "ephemeral.json")
			if err := os.WriteFile(path, []byte(`{"name":"eph","default_vendor":"cursor"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateRun(t)
			harnessDir := filepath.Join(t.TempDir(), "my-dev")
			writeSkillHarness(t, harnessDir, "my-dev")
			project := t.TempDir()
			t.Chdir(project)

			if err := cmdRun([]string{"--harness-file", tt.file(t, harnessDir), "-v", "cursor", "--install"}); err != nil {
				t.Fatalf("ynh run --harness-file: %v", err)
			}
			if _, err := os.Stat(filepath.Join(project, ".cursor", "skills", "hello", "SKILL.md")); err != nil {
				t.Errorf("the harness's skill was not reachable from the project: %v", err)
			}
		})
	}
}
