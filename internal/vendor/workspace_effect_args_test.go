package vendor

import (
	"io"
	"strings"
	"testing"
)

// These tests pin the arguments ynh finally hands Claude Code and Codex for a
// granted workspace folder, on every launch path (one-shot, interactive,
// resume). The live proof that the grant is what allows the access, and that
// --no-workspace is what refuses it, is scripts/workspace-effect-live.sh: a
// unit test cannot show a vendor honours a flag, only that ynh sends it where
// the vendor reads it.

const grantedDir = "/granted/shared"

// hasPair reports whether args holds flag immediately followed by value.
func hasPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func TestClaudeGrantReachesEveryLaunchPath(t *testing.T) {
	grant, _, err := WorkspaceArgs("claude", []string{grantedDir})
	if err != nil {
		t.Fatal(err)
	}
	cfg := t.TempDir()
	resumeInitial, resumeArgs := claudeResumeArgs("sid", "go", false, grant)
	paths := map[string][]string{
		"one-shot":     buildClaudeArgs(cfg, "", append([]string{"-p", "go"}, grant...)),
		"interactive":  buildClaudeArgs(cfg, "", grant),
		"first-prompt": buildClaudeArgs(cfg, "go", grant),
		"resume":       buildClaudeArgs(cfg, resumeInitial, resumeArgs),
	}
	for name, args := range paths {
		if !hasPair(args, "--add-dir", grantedDir) {
			t.Errorf("%s: no --add-dir %s in %v", name, grantedDir, args)
		}
		// The run directory's own --add-dir must still be there beside it.
		if !hasPair(args, "--add-dir", cfg) {
			t.Errorf("%s: lost the run directory --add-dir in %v", name, args)
		}
	}
	// Without a grant (--no-workspace) the folder never appears.
	for name, args := range map[string][]string{
		"one-shot": buildClaudeArgs(cfg, "", []string{"-p", "go"}),
		"resume":   func() []string { i, a := claudeResumeArgs("sid", "go", false, nil); return buildClaudeArgs(cfg, i, a) }(),
	} {
		if strings.Contains(strings.Join(args, "\x00"), grantedDir) {
			t.Errorf("%s: folder present without a grant: %v", name, args)
		}
	}
}

func TestCodexGrantReachesEveryLaunchPath(t *testing.T) {
	grant, _, err := WorkspaceArgs("codex", []string{grantedDir})
	if err != nil {
		t.Fatal(err)
	}
	run := t.TempDir()

	// One-shot: --add-dir is a global flag, so it sits before the prompt.
	oneShot := codexExecArgs(run, "the prompt", grant, io.Discard)
	if !hasPair(oneShot, "--add-dir", grantedDir) {
		t.Errorf("one-shot: no --add-dir %s in %v", grantedDir, oneShot)
	}
	if oneShot[len(oneShot)-1] != "the prompt" || indexOf(oneShot, "--add-dir") > len(oneShot)-2 {
		t.Errorf("one-shot: --add-dir must precede the prompt: %v", oneShot)
	}
	if !hasPair(codexInteractiveArgs(run, grant, io.Discard), "--add-dir", grantedDir) {
		t.Error("interactive: no --add-dir")
	}

	// Interactive resume keeps --add-dir.
	interactive, err := codexResumeArgs("sid", "", true, grant, io.Discard)
	if err != nil || !hasPair(interactive, "--add-dir", grantedDir) {
		t.Errorf("interactive resume: %v, %v", interactive, err)
	}

	// Resume with a prompt is `codex exec resume`, which rejects --add-dir
	// (0.166.0), so the folder must arrive as the writable_roots override.
	cfg := writeCodexConfig(t, "sandbox_mode = \"workspace-write\"\n")
	t.Setenv("CODEX_HOME", strings.TrimSuffix(cfg, "/config.toml"))
	prompted, err := codexResumeArgs("sid", "go", false, grant, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(prompted, "--add-dir") >= 0 {
		t.Errorf("exec resume must not carry --add-dir: %v", prompted)
	}
	if !hasPair(prompted, "-c", `sandbox_workspace_write.writable_roots=["`+grantedDir+`"]`) {
		t.Errorf("exec resume: folder not passed as writable_roots: %v", prompted)
	}

	// No grant (--no-workspace): no folder on any path.
	for name, args := range map[string][]string{
		"one-shot": codexExecArgs(run, "p", nil, io.Discard),
		"resume":   mustArgs(codexResumeArgs("sid", "go", false, nil, io.Discard)),
	} {
		if strings.Contains(strings.Join(args, "\x00"), "--add-dir") || strings.Contains(strings.Join(args, "\x00"), "writable_roots") {
			t.Errorf("%s: a folder grant is present without one: %v", name, args)
		}
	}
}

func mustArgs(args []string, err error) []string {
	if err != nil {
		panic(err)
	}
	return args
}
