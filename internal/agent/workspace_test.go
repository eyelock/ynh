package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

func TestWorkerArgsRenderWorkspace(t *testing.T) {
	dirs := []string{"/w/one", "/w/two"}
	want := []string{"--add-dir", "/w/one", "--add-dir", "/w/two"}

	t.Run("claude", func(t *testing.T) {
		args := buildClaudeStreamArgs(StartOptions{Workspace: dirs})
		if !containsRun(args, want) {
			t.Errorf("args %v lack %v", args, want)
		}
	})
	t.Run("codex", func(t *testing.T) {
		args := buildCodexArgs(StartOptions{Workspace: dirs}, "")
		if !containsRun(args, want) {
			t.Errorf("args %v lack %v", args, want)
		}
		if args[len(args)-1] != "-" {
			t.Errorf("stdin marker must stay last: %v", args)
		}
	})
	t.Run("cursor carries none", func(t *testing.T) {
		args := buildCursorArgs(StartOptions{Workspace: dirs}, "chat", true, "msg")
		if slices.Contains(args, "--add-dir") || slices.Contains(args, "/w/one") {
			t.Errorf("cursor has no such flag, got %v", args)
		}
	})
	t.Run("none requested adds nothing", func(t *testing.T) {
		if slices.Contains(buildCodexArgs(StartOptions{}, ""), "--add-dir") {
			t.Error("--add-dir without a grant")
		}
	})
	t.Run("claude keeps the staging --add-dir beside the grant", func(t *testing.T) {
		args := buildClaudeStreamArgs(StartOptions{ConfigPath: "/cfg", Workspace: dirs})
		if !containsRun(args, []string{"--add-dir", "/cfg"}) || !containsRun(args, want) {
			t.Errorf("args %v", args)
		}
	})
}

// containsRun reports whether run appears as consecutive elements of args.
func containsRun(args, run []string) bool {
	for i := 0; i+len(run) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

// The worker is started with the folders WorkspaceGrant returned, and the hook
// sees the harness as loaded. With no hook nothing is granted: a request alone
// grants nothing.
func TestRunLoop_WorkspaceGrantReachesWorker(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Setenv("YNH_HOME", t.TempDir())
		dir := filepath.Join(t.TempDir(), "seat")
		writePathHarness(t, dir)
		manifest := filepath.Join(dir, plugin.PluginDir, plugin.PluginFile)
		if err := os.WriteFile(manifest, []byte(`{"name":"seat","version":"0.1.0","default_vendor":"claude","workspace":["../ynh"]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		recordCheck(t, gate.StatusPass)
		return dir, t.TempDir()
	}

	t.Run("hook grants", func(t *testing.T) {
		dir, session := setup(t)
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := resumeOpts(mb, session)
		opts.HarnessName = dir
		var gotID, gotBackend string
		var gotReq []string
		opts.WorkspaceGrant = func(h *harness.Harness, id, backend, worktree string) []string {
			gotID, gotBackend, gotReq = id, backend, h.Workspace
			return []string{"/granted/dir"}
		}
		if _, err := RunLoop(opts); err != nil {
			t.Fatal(err)
		}
		if len(mb.startOpts) != 1 || !reflect.DeepEqual(mb.startOpts[0].Workspace, []string{"/granted/dir"}) {
			t.Fatalf("worker start options: %+v", mb.startOpts)
		}
		if gotID != "" || gotBackend != "claude" || !reflect.DeepEqual(gotReq, []string{"../ynh"}) {
			t.Errorf("hook saw id %q backend %q request %v", gotID, gotBackend, gotReq)
		}
	})

	t.Run("no hook grants nothing", func(t *testing.T) {
		dir, session := setup(t)
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := resumeOpts(mb, session)
		opts.HarnessName = dir
		if _, err := RunLoop(opts); err != nil {
			t.Fatal(err)
		}
		if len(mb.startOpts) != 1 || len(mb.startOpts[0].Workspace) != 0 {
			t.Fatalf("a request alone granted %+v", mb.startOpts)
		}
	})
}
