package agent

import (
	"errors"
	"fmt"
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
	t.Run("cursor", func(t *testing.T) {
		args := buildCursorArgs(StartOptions{Workspace: dirs}, "chat", true, "msg")
		if !containsRun(args, want) {
			t.Errorf("args %v lack %v", args, want)
		}
		if args[len(args)-1] != "msg" {
			t.Errorf("the message must stay last: %v", args)
		}
	})
	t.Run("opencode carries none, it allows folders by default", func(t *testing.T) {
		args := buildOpenCodeArgs(StartOptions{Workspace: dirs}, "", "msg")
		if slices.Contains(args, "--add-dir") || slices.Contains(args, "/w/one") {
			t.Errorf("opencode has no such flag, got %v", args)
		}
		if args[len(args)-2] != "--" || args[len(args)-1] != "msg" {
			t.Errorf("the message must stay last, after --: %v", args)
		}
	})
	t.Run("none requested adds nothing", func(t *testing.T) {
		if slices.Contains(buildCodexArgs(StartOptions{}, ""), "--add-dir") {
			t.Error("--add-dir without a grant")
		}
	})
	t.Run("claude reads the staging dir by rule, not --add-dir, beside the grant", func(t *testing.T) {
		args := buildClaudeStreamArgs(StartOptions{ConfigPath: "/cfg", Workspace: dirs})
		if containsRun(args, []string{"--add-dir", "/cfg"}) || !slices.Contains(args, "--allowedTools=Read(//cfg/**)") || !containsRun(args, want) {
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

func TestRunLoop_HookTrustReachesWorker(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Setenv("YNH_HOME", t.TempDir())
		dir := filepath.Join(t.TempDir(), "seat")
		writePathHarness(t, dir)
		recordCheck(t, gate.StatusPass)
		return dir, t.TempDir()
	}

	for _, trusted := range []bool{true, false} {
		name := "hook declines"
		if trusted {
			name = "hook trusts"
		}
		t.Run(name, func(t *testing.T) {
			dir, session := setup(t)
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			opts := resumeOpts(mb, session)
			opts.HarnessName = dir
			var gotName, gotID, gotBackend, gotConfig string
			opts.HookTrust = func(h *harness.Harness, id, backend, configDir string) bool {
				gotName, gotID, gotBackend, gotConfig = h.Name, id, backend, configDir
				return trusted
			}
			if _, err := RunLoop(opts); err != nil {
				t.Fatal(err)
			}
			if len(mb.startOpts) != 1 || mb.startOpts[0].TrustedHooks != trusted {
				t.Fatalf("worker start options: %+v, want TrustedHooks %v", mb.startOpts, trusted)
			}
			if gotName != "authoring" || gotID != "" || gotBackend != "mock" || gotConfig == "" || gotConfig != mb.startOpts[0].ConfigPath {
				t.Errorf("hook saw harness %q id %q backend %q config %q (worker config %q)", gotName, gotID, gotBackend, gotConfig, mb.startOpts[0].ConfigPath)
			}
		})
	}

	t.Run("no hook trusts nothing", func(t *testing.T) {
		dir, session := setup(t)
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := resumeOpts(mb, session)
		opts.HarnessName = dir
		if _, err := RunLoop(opts); err != nil {
			t.Fatal(err)
		}
		if len(mb.startOpts) != 1 || mb.startOpts[0].TrustedHooks {
			t.Fatalf("no hook trusted hooks: %+v", mb.startOpts)
		}
	})
}

// Pre-approval follows trust (ynh #952): the worker is told the harness is
// trusted only when the Trusted hook says so, and --auto-approve is refused
// for a harness that is not.
func TestRunLoop_PreApprovalFollowsTrust(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Setenv("YNH_HOME", t.TempDir())
		dir := filepath.Join(t.TempDir(), "seat")
		writePathHarness(t, dir)
		recordCheck(t, gate.StatusPass)
		return dir, t.TempDir()
	}
	gated := func(opts *RunOptions, trusted bool) {
		opts.Admit = func(*harness.Harness, string) Admission {
			return Admission{MCPApprove: trusted, AutoApprove: trusted}
		}
	}

	for _, trusted := range []bool{true, false} {
		t.Run(fmt.Sprintf("worker sees trusted=%v", trusted), func(t *testing.T) {
			dir, session := setup(t)
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			opts := resumeOpts(mb, session)
			opts.HarnessName = dir
			gated(&opts, trusted)
			if _, err := RunLoop(opts); err != nil {
				t.Fatal(err)
			}
			if len(mb.startOpts) != 1 || mb.startOpts[0].TrustedHarness != trusted {
				t.Fatalf("worker start options: %+v, want TrustedHarness %v", mb.startOpts, trusted)
			}
		})
	}

	t.Run("no Trusted hook trusts nothing", func(t *testing.T) {
		dir, session := setup(t)
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := resumeOpts(mb, session)
		opts.HarnessName = dir
		opts.Admit = func(*harness.Harness, string) Admission { return Admission{} }
		if _, err := RunLoop(opts); err != nil {
			t.Fatal(err)
		}
		if mb.startOpts[0].TrustedHarness {
			t.Fatal("a run with no Trusted hook was trusted")
		}
	})

	for _, level := range []string{AutoApproveEdits, AutoApproveAll} {
		t.Run("auto-approve "+level+" refused for an untrusted harness", func(t *testing.T) {
			dir, session := setup(t)
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			opts := resumeOpts(mb, session)
			opts.Backend = "claude"
			opts.HarnessName = dir
			opts.AutoApprove = level
			gated(&opts, false)
			_, err := RunLoop(opts)
			var ee *ExitError
			if !errors.As(err, &ee) || ee.Code != ExitRefused || ee.ErrorCode != untrustedCode {
				t.Fatalf("err = %v, want an untrusted refusal", err)
			}
			if len(mb.startOpts) != 0 {
				t.Error("a worker started")
			}
		})
	}

	t.Run("auto-approve accepted for a trusted harness", func(t *testing.T) {
		dir, session := setup(t)
		mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
		opts := resumeOpts(mb, session)
		opts.Backend = "claude"
		opts.HarnessName = dir
		opts.AutoApprove = AutoApproveEdits
		gated(&opts, true)
		if _, err := RunLoop(opts); err != nil {
			t.Fatal(err)
		}
		if len(mb.startOpts) != 1 || mb.startOpts[0].AutoApprove != AutoApproveEdits {
			t.Fatalf("worker start options: %+v", mb.startOpts)
		}
	})
}
