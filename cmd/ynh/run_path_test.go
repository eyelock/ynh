package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/runsnap"
	"github.com/eyelock/ynh/internal/vendor"
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
			// The snapshot store sits beside the stable link and is not a harness.
			var dirs []os.DirEntry
			for _, e := range entries {
				if !runsnap.IsSnapshotsDir(e.Name()) {
					dirs = append(dirs, e)
				}
			}
			entries = dirs
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

// Without a terminal, `ynh run -v cursor` refuses to install symlinks: it
// returns the consent error, links nothing and records nothing.
func TestCmdRun_NoTerminalRefusesSymlinkInstall(t *testing.T) {
	isolateRun(t)
	work := t.TempDir()
	harnessDir := filepath.Join(work, "my-dev")
	writeSkillHarness(t, harnessDir, "my-dev")
	t.Chdir(work)

	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	defer func() { stdinIsTerminal = orig }()

	err := cmdRun([]string{harnessDir, "-v", "cursor"})
	if err == nil || !strings.Contains(err.Error(), "no terminal") || !strings.Contains(err.Error(), "--install") {
		t.Fatalf("err = %v, want the consent refusal naming --install", err)
	}
	if _, statErr := os.Lstat(filepath.Join(work, ".cursor")); !os.IsNotExist(statErr) {
		t.Errorf("a .cursor/ was created despite the refusal: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(os.Getenv("YNH_HOME"), "symlinks.json")); !os.IsNotExist(statErr) {
		t.Errorf("symlinks.json was written despite the refusal: %v", statErr)
	}
}

// Each run seals its assembled tree as a snapshot behind the stable
// <run>/<name> link: an unchanged rebuild reuses the snapshot, changed inputs
// make a new one, and the earlier snapshot is left as it was (ynh #765).
func TestCmdRun_SnapshotsReusedAndKept(t *testing.T) {
	isolateRun(t)
	work := t.TempDir()
	harnessDir := filepath.Join(work, "my-dev")
	writeSkillHarness(t, harnessDir, "my-dev")
	t.Chdir(work)

	snaps := func() []string {
		var out []string
		root := filepath.Join(os.Getenv("YNH_HOME"), "run", runsnap.SnapshotsDirName)
		names, _ := os.ReadDir(root)
		for _, n := range names {
			hs, _ := os.ReadDir(filepath.Join(root, n.Name()))
			for _, h := range hs {
				out = append(out, filepath.Join(root, n.Name(), h.Name()))
			}
		}
		return out
	}
	run := func() {
		t.Helper()
		if err := cmdRun([]string{harnessDir, "-v", "cursor", "--install"}); err != nil {
			t.Fatal(err)
		}
	}

	run()
	first := snaps()
	if len(first) != 1 {
		t.Fatalf("snapshots after first run = %v", first)
	}
	run()
	if got := snaps(); len(got) != 1 || got[0] != first[0] {
		t.Fatalf("unchanged rebuild made a new snapshot: %v, want %v", got, first)
	}

	skill := filepath.Join(harnessDir, "skills", "hello", "SKILL.md")
	if err := os.WriteFile(skill, []byte("---\nname: hello\ndescription: Changed.\n---\n\n# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run()
	if got := snaps(); len(got) != 2 {
		t.Fatalf("changed input should add a snapshot: %v", got)
	}
	old, err := os.ReadFile(filepath.Join(first[0], ".cursor", "skills", "hello", "SKILL.md"))
	if err != nil || !strings.Contains(string(old), "A trivial skill.") {
		t.Fatalf("the first snapshot changed under a later run: %q %v", old, err)
	}
}

// Cursor and Codex project symlinks are planted through the stable link
// <run>/<name>, not at the snapshot's real path: a snapshot is never deleted,
// so a link at a snapshot path would stay intact and show the first snapshot
// for good. After an input changes, the next run shows the new content through
// the project's own link (ynh #765).
func TestCmdRun_ProjectSymlinksFollowTheStableLink(t *testing.T) {
	isolateRun(t)
	work := t.TempDir()
	harnessDir := filepath.Join(work, "my-dev")
	writeSkillHarness(t, harnessDir, "my-dev")
	project := filepath.Join(work, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)

	run := func() {
		t.Helper()
		if err := cmdRun([]string{harnessDir, "-v", "cursor", "--install"}); err != nil {
			t.Fatal(err)
		}
	}
	run()

	projLink := filepath.Join(project, ".cursor", "skills", "hello")
	target, err := os.Readlink(projLink)
	if err != nil {
		t.Fatal(err)
	}
	runRoot := filepath.Join(os.Getenv("YNH_HOME"), "run")
	if strings.Contains(target, runsnap.SnapshotsDirName) {
		t.Fatalf("project link points at a snapshot path: %s", target)
	}
	if !strings.HasPrefix(target, runRoot+string(filepath.Separator)) {
		t.Fatalf("project link %s is not under the run dir %s", target, runRoot)
	}

	skill := filepath.Join(harnessDir, "skills", "hello", "SKILL.md")
	if err := os.WriteFile(skill, []byte("---\nname: hello\ndescription: Changed.\n---\n\n# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run()
	got, err := os.ReadFile(filepath.Join(projLink, "SKILL.md"))
	if err != nil || !strings.Contains(string(got), "Changed.") {
		t.Fatalf("the project link did not follow the newer snapshot: %q %v", got, err)
	}
}

// An OpenCode workspace grant is a rule in the agent files, so it is part of
// the snapshot's hash: a later run that was not granted the folder
// (--no-workspace, or the grant withdrawn) gets a snapshot without the rule,
// and the first run's snapshot keeps its own.
func TestCmdRun_OpenCodeWorkspaceGrantDoesNotOutliveItsRun(t *testing.T) {
	isolateRun(t)
	work := t.TempDir()
	harnessDir := filepath.Join(work, "oc")
	if err := os.MkdirAll(filepath.Join(harnessDir, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(harnessDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"oc","version":"0.1.0","default_vendor":"opencode","workspace":["../shared"]}`
	if err := os.WriteFile(filepath.Join(harnessDir, ".agents/harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := "---\ndescription: d\npermissions:\n  - action: \"*\"\n    resource: \"*\"\n    effect: deny\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(harnessDir, "agents", "a.md"), []byte(agent), 0o644); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(work, "shared")
	project := filepath.Join(work, "project")
	for _, d := range []string{shared, project} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(project)
	// A run from a path has no trust record, so the operator's allow-list
	// grants the folder.
	t.Setenv("YNH_WORKSPACE_ROOTS", work)

	var launchedDirs []string
	stubLaunch(t, func(runDir string, _ runArgs) error {
		launchedDirs = append(launchedDirs, runDir)
		return nil
	})
	agentIn := func(runDir string) string {
		b, err := os.ReadFile(filepath.Join(runDir, ".opencode", "agents", "a.md"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	run := func(args ...string) string {
		t.Helper()
		before := len(launchedDirs)
		if err := cmdRun(append([]string{harnessDir, "-v", "opencode"}, args...)); err != nil {
			t.Fatal(err)
		}
		if len(launchedDirs) != before+1 {
			t.Fatalf("launch ran %d times", len(launchedDirs)-before)
		}
		return launchedDirs[len(launchedDirs)-1]
	}

	granted := run()
	if !strings.Contains(agentIn(granted), "external_directory") {
		t.Fatalf("the first run was not granted the folder:\n%s", agentIn(granted))
	}
	// Same inputs: the granted snapshot is reused.
	if again := run(); again != granted {
		t.Errorf("an identical run made a new snapshot: %s vs %s", again, granted)
	}
	// --no-workspace: no snapshot with the rule.
	denied := run("--no-workspace")
	if denied == granted || strings.Contains(agentIn(denied), "external_directory") {
		t.Fatalf("--no-workspace reused or carried the grant (%s):\n%s", denied, agentIn(denied))
	}
	// The grant withdrawn (nothing allows the folder): same.
	t.Setenv("YNH_WORKSPACE_ROOTS", filepath.Join(work, "nowhere"))
	withdrawn := run()
	if strings.Contains(agentIn(withdrawn), "external_directory") {
		t.Fatalf("a withdrawn grant survived into a reused snapshot:\n%s", agentIn(withdrawn))
	}
	// The first snapshot is untouched.
	if !strings.Contains(agentIn(granted), "external_directory") {
		t.Errorf("the granted snapshot lost its rule")
	}
}

// Codex and Cursor start in a stable working folder whose entries link into
// the newest snapshot, so the directory they key sessions and trust by does
// not change when the harness does (ynh #765). Claude starts in the snapshot.
func TestCmdRun_CodexAndCursorStartInAStableWorkDir(t *testing.T) {
	for _, v := range []string{"codex", "cursor"} {
		t.Run(v, func(t *testing.T) {
			isolateRun(t)
			work := t.TempDir()
			harnessDir := filepath.Join(work, "my-dev")
			writeSkillHarness(t, harnessDir, "my-dev")
			t.Chdir(work)

			var started []string
			orig := launchSessionFn
			launchSessionFn = func(_ vendor.Adapter, runDir, _ string, _ runArgs, _, _ string, _ []string, _ io.Writer) error {
				started = append(started, runDir)
				return nil
			}
			defer func() { launchSessionFn = orig }()

			run := func() {
				t.Helper()
				if err := cmdRun([]string{harnessDir, "-v", v, "-p", "hi"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmdRun([]string{harnessDir, "-v", v, "--install"}); err != nil {
				t.Fatal(err)
			}
			run()
			skill := filepath.Join(harnessDir, "skills", "hello", "SKILL.md")
			if err := os.WriteFile(skill, []byte("---\nname: hello\ndescription: Changed.\n---\n\n# hello\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			run()

			if len(started) != 2 || started[0] != started[1] {
				t.Fatalf("working folder changed between runs: %v", started)
			}
			if strings.Contains(started[0], runsnap.SnapshotsDirName+string(filepath.Separator)+"local") {
				t.Errorf("started in a snapshot: %s", started[0])
			}
			got, err := os.ReadFile(filepath.Join(started[1], "."+v, "skills", "hello", "SKILL.md"))
			if err != nil || !strings.Contains(string(got), "Changed.") {
				t.Errorf("the working folder does not show the newest snapshot: %q %v", got, err)
			}
		})
	}
}
