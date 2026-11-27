package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

const hookedHarness = `{"name":"hooked","version":"0.1.0","default_vendor":"claude",` +
	`"hooks":{"on_stop":[{"command":"echo done"}]}}`

const plainHarness = `{"name":"plain","version":"0.1.0","default_vendor":"claude"}`

func loadInstalled(t *testing.T, id string) *harness.Harness {
	t.Helper()
	p, err := harness.LoadByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// An installed harness that executes something and was never accepted is
// refused, naming what it executes and the command that accepts it.
func TestTrustGate_UnacceptedInstalledHarnessIsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "hooked", hookedHarness)

	err := trustGate(id, loadInstalled(t, id), io.Discard)
	if !isUntrusted(err) {
		t.Fatalf("err = %v, want an untrusted refusal", err)
	}
	for _, want := range []string{"local/hooked", "echo done", "ynh trust show local/hooked", "ynh trust accept local/hooked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal lacks %q:\n%s", want, err)
		}
	}
}

// Accepted harnesses pass, and one that executes nothing needs no acceptance.
func TestTrustGate_AcceptedAndCommandlessPass(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	hooked := installTrustedTestHarness(t, home, "hooked", hookedHarness)
	plain := installListTestHarness(t, home, "plain", plainHarness)

	for _, id := range []string{hooked, plain} {
		if err := trustGate(id, loadInstalled(t, id), io.Discard); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}

// A harness whose commands moved since acceptance is refused again, and a
// scheme-2 record says why it reads as changed.
func TestTrustGate_ChangedHarnessIsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installTrustedTestHarness(t, home, "hooked", hookedHarness)
	installListTestHarness(t, home, "hooked", strings.Replace(hookedHarness, "echo done", "echo changed", 1))

	err := trustGate(id, loadInstalled(t, id), io.Discard)
	if !isUntrusted(err) || !strings.Contains(err.Error(), "differ from what you accepted") {
		t.Fatalf("err = %v, want a changed refusal", err)
	}

	store, lerr := loadTrustStore()
	if lerr != nil {
		t.Fatal(lerr)
	}
	rec := store.Harnesses[id]
	rec.Scheme = 2
	store.Harnesses[id] = rec
	if serr := saveTrustStore(store); serr != nil {
		t.Fatal(serr)
	}
	err = trustGate(id, loadInstalled(t, id), io.Discard)
	if !isUntrusted(err) || !strings.Contains(err.Error(), scriptDigestNote) {
		t.Fatalf("err = %v, want scheme 3's note", err)
	}
}

// Includes that cannot be read refuse, naming ynh update (decision 15).
func TestTrustGate_UnreadableIncludeIsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "inc",
		`{"name":"inc","version":"0.1.0","includes":[{"git":"https://example.invalid/none.git"}]}`)

	err := trustGate(id, loadInstalled(t, id), io.Discard)
	if !isUntrusted(err) || !strings.Contains(err.Error(), "ynh update") {
		t.Fatalf("err = %v, want a refusal naming ynh update", err)
	}
}

// `ynh run` refuses before launching anything, with the gate's message.
func TestTrustGate_RunRefusesBeforeLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("PATH", t.TempDir()) // no vendor CLI: reaching a launch would fail differently
	id := installListTestHarness(t, home, "hooked", hookedHarness)

	err := cmdRun([]string{id, "-v", "claude", "--", "hello"})
	if !isUntrusted(err) {
		t.Fatalf("err = %v, want an untrusted refusal", err)
	}
}

// `ynh check` refuses with exit 1 and the structured code untrusted, not the
// exit 2 of a check that could not run.
func TestTrustGate_CheckRefusesWithStructuredCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "hooked", hookedHarness)

	var stdout, stderr bytes.Buffer
	err := cmdCheck([]string{id, "--format", "json", "--cwd", t.TempDir()}, &stdout, &stderr)
	if err == nil || errors.Is(err, errCheckExec) || errors.Is(err, errCheckBlocked) {
		t.Fatalf("err = %v, want a plain refusal (exit 1)", err)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if jerr := json.Unmarshal(stderr.Bytes(), &env); jerr != nil {
		t.Fatalf("stderr is not an error envelope: %v\n%s", jerr, stderr.String())
	}
	if env.Error.Code != errCodeUntrusted || !strings.Contains(env.Error.Message, "ynh trust accept "+id) {
		t.Errorf("envelope = %+v", env.Error)
	}
	// The envelope validates against the published schema.
	var v any
	if jerr := json.Unmarshal(stderr.Bytes(), &v); jerr != nil {
		t.Fatal(jerr)
	}
	s, serr := clischema.Get("error")
	if serr != nil {
		t.Fatal(serr)
	}
	if verr := s.Validate(v); verr != nil {
		t.Errorf("error envelope does not validate: %v", verr)
	}
}

// ---- folder runs ----

// remoteIncludeFolder makes a folder harness that includes a remote git
// repository declaring one sensor, cached so it can be read.
func remoteIncludeFolder(t *testing.T) (folder, src string) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	root := t.TempDir()
	src = filepath.Join(root, "remote")
	gitRepo(t, src, "remote-sensor")
	folder = filepath.Join(root, "folder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pinned at the commit: acceptance is refused for a floating include.
	sha := gitRevParse(t, src)
	manifest := `{"name":"folderh","version":"0.1.0","includes":[{"git":"file://` + src + `","ref":"` + sha + `"}]}`
	if err := writePluginJSONFile(folder, []byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.EnsureRepo("file://"+src, sha); err != nil {
		t.Fatal(err)
	}
	return folder, src
}

// A folder run says it is not trust-checked, and its own commands run.
func TestTrustGate_FolderRunLaunchesWithItsNotice(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(hookedHarness)); err != nil {
		t.Fatal(err)
	}
	p, err := harness.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if gerr := trustGate("", p, &stderr); gerr != nil {
		t.Fatalf("a folder's own commands are not gated: %v", gerr)
	}
	if !strings.Contains(stderr.String(), "not trust-checked") {
		t.Errorf("no notice: %q", stderr.String())
	}
}

// A remote include's commands are refused until accepted for the folder, and
// asked about again when the include brings something else.
func TestTrustGate_FolderRunRefusesAnUnacceptedRemoteInclude(t *testing.T) {
	folder, src := remoteIncludeFolder(t)
	p, err := harness.LoadDir(folder)
	if err != nil {
		t.Fatal(err)
	}

	gerr := trustGate("", p, io.Discard)
	if !isUntrusted(gerr) || !strings.Contains(gerr.Error(), "/remote//") || !strings.Contains(gerr.Error(), "remote-sensor") ||
		!strings.Contains(gerr.Error(), "ynh trust accept "+folder) {
		t.Fatalf("err = %v, want a refusal naming the include, its sensor and the accept command", gerr)
	}

	var out bytes.Buffer
	if aerr := cmdTrustTo([]string{"accept", folder}, &out, io.Discard); aerr != nil {
		t.Fatalf("accept: %v", aerr)
	}
	if !strings.Contains(out.String(), "Recorded 1 remote include") {
		t.Errorf("accept output: %q", out.String())
	}
	if gerr = trustGate("", p, io.Discard); gerr != nil {
		t.Fatalf("after accepting: %v", gerr)
	}

	// The include now declares a different sensor command: refused again.
	changed := `{"name":"inc","version":"1.0.0","sensors":{"remote-sensor":{"category":"maintainability","tolerance":"blocking",` +
		`"source":{"command":"echo different"},"output":{"format":"text","channel":"stdout+exit"}}}}`
	if werr := writePluginJSONFile(src, []byte(changed)); werr != nil {
		t.Fatal(werr)
	}
	runGitInDir(t, src, "add", "-A")
	runGitInDir(t, src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "change")
	// A pinned include cannot change under its ref, so the folder moves to
	// the new commit, which has no record.
	newSHA := gitRevParse(t, src)
	moved := `{"name":"folderh","version":"0.1.0","includes":[{"git":"file://` + src + `","ref":"` + newSHA + `"}]}`
	if werr := writePluginJSONFile(folder, []byte(moved)); werr != nil {
		t.Fatal(werr)
	}
	if _, uerr := resolver.EnsureRepo("file://"+src, newSHA); uerr != nil {
		t.Fatal(uerr)
	}
	p, err = harness.LoadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	if gerr = trustGate("", p, io.Discard); !isUntrusted(gerr) {
		t.Fatalf("err = %v, want the changed include refused", gerr)
	}
}

// A local include and the harness's own repository are the harness's own
// files: a folder run takes them without a record.
func TestTrustGate_LocalIncludeIsNotRemote(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	root := t.TempDir()
	writeIncludedHarness(t, root, "up", hookFields("./hooks/up.sh"))
	writeScript(t, filepath.Join(root, "up"), "hooks/up.sh", "#!/bin/sh\nexit 0\n")
	p := &harness.Harness{Name: "consumer", Dir: root, Includes: []harness.Include{{GitSource: harness.GitSource{Local: "up"}, Hooks: hooksOn(true)}}}
	if err := trustGate("", p, io.Discard); err != nil {
		t.Fatalf("a local include is the folder's own: %v", err)
	}
}

// An include a folder run cannot read refuses, naming ynh update.
func TestTrustGate_FolderRunWithUnreadableIncludeIsRefused(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	p := &harness.Harness{Name: "consumer", Dir: t.TempDir(), Includes: []harness.Include{
		{GitSource: harness.GitSource{Git: "https://example.invalid/none.git"}},
	}}
	err := trustGate("", p, io.Discard)
	if !isUntrusted(err) || !strings.Contains(err.Error(), "ynh update") {
		t.Fatalf("err = %v", err)
	}
}

// ---- ynh agent run ----

type agentRefusal struct {
	ExitCode  int    `json:"exit_code"`
	Converged bool   `json:"converged"`
	Reason    string `json:"reason"`
	ErrorCode string `json:"error_code"`
}

func runAgentRefused(t *testing.T, args ...string) (agentRefusal, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cmdAgentRun(append([]string{"--format", "json", "--worktree", t.TempDir()}, args...), &stdout, &stderr, strings.NewReader(""))
	var r agentRefusal
	if jerr := json.Unmarshal(stdout.Bytes(), &r); jerr != nil {
		t.Fatalf("no result: %v\n%s", jerr, stdout.String())
	}
	return r, stdout.String(), err
}

// ynh agent run refuses an unaccepted install before any worker starts: exit
// 1, error_code untrusted in the result, and the result validates.
func TestTrustGate_AgentRunRefusesAnUnacceptedInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("YNH_AGENT_SESSION", "")
	starts := vendorShims(t, "a refused run must not start a worker")
	id := installListTestHarness(t, home, "hooked", hookedHarness)

	r, _, err := runAgentRefused(t, "--task", "t", "--no-plan", "--harness", id)
	if processExitCode(err) != 1 || r.ExitCode != 1 || r.Converged {
		t.Errorf("exit %d result %+v, want a refusal with 1", processExitCode(err), r)
	}
	if r.ErrorCode != errCodeUntrusted || !strings.Contains(r.Reason, "ynh trust accept "+id) {
		t.Errorf("error_code %q reason %q", r.ErrorCode, r.Reason)
	}
	if _, serr := os.Stat(starts); serr == nil {
		t.Error("a worker started")
	}
}

// --resume re-checks the gate for the harness its checkpoint names, and
// refuses a checkpoint that names none.
func TestTrustGate_AgentRunResumeIsChecked(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("YNH_AGENT_SESSION", "")
	starts := vendorShims(t, "a refused run must not start a worker")
	id := installListTestHarness(t, home, "hooked", hookedHarness)

	for name, cp := range map[string]map[string]any{
		"checkpoint names an unaccepted harness": {"backend": "claude", "task": "t", "phase": "act", "plan_finalized": true, "pending_message": "go", "harness_name": id},
		"checkpoint names no harness":            {"backend": "claude", "task": "t", "phase": "act", "plan_finalized": true, "pending_message": "go"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeAgentCheckpoint(t, dir, cp)
			r, _, err := runAgentRefused(t, "--resume", dir)
			if processExitCode(err) != 1 || r.ExitCode != 1 || r.ErrorCode != errCodeUntrusted {
				t.Errorf("exit %d result %+v, want exit 1 and error_code untrusted", processExitCode(err), r)
			}
			if _, serr := os.Stat(starts); serr == nil {
				t.Error("a worker started")
			}
		})
	}
}

// An accepted harness runs: the gate lets it through to the worker.
func TestTrustGate_AgentRunAcceptedInstallPassesTheGate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("YNH_AGENT_SESSION", "")
	vendorShims(t, "Not logged in")
	id := installTrustedTestHarness(t, home, "hooked", hookedHarness)

	r, _, _ := runAgentRefused(t, "--task", "t", "--no-plan", "--harness", id)
	if r.ErrorCode == errCodeUntrusted {
		t.Fatalf("an accepted harness was refused: %+v", r)
	}
}

// ---- scripts the digest could not read ----

// A script the digest skips or refuses blocks the run, accepted or not
// (the human's decision on ynw-pz9kv): a link to a device, out of the harness,
// dangling or looping, a link to a folder beside the script, a link out in
// the script's folder, and a script that is missing. The refusal names each
// with its reason and a Fix, and carries the gate's code.
func TestTrustGate_SkippedScriptBlocksTheRun(t *testing.T) {
	outside := t.TempDir()
	writeScript(t, outside, "evil.sh", "#!/bin/sh\n")

	cases := []struct {
		name  string
		setup func(t *testing.T, dir string)
		cmd   string
		want  []string
	}{
		{"a link to a device", func(t *testing.T, dir string) { mustSymlink(t, "/dev/zero", dir, "hooks/g.sh") },
			"./hooks/g.sh", []string{"g.sh", "leads outside the harness"}},
		{"a link out of the harness", func(t *testing.T, dir string) { mustSymlink(t, filepath.Join(outside, "evil.sh"), dir, "hooks/g.sh") },
			"./hooks/g.sh", []string{"g.sh", "leads outside the harness"}},
		{"a dangling link", func(t *testing.T, dir string) { mustSymlink(t, filepath.Join(dir, "nowhere"), dir, "hooks/g.sh") },
			"./hooks/g.sh", []string{"g.sh", "does not resolve"}},
		{"a looping link", func(t *testing.T, dir string) {
			mustSymlink(t, filepath.Join(dir, "hooks", "b.sh"), dir, "hooks/g.sh")
			mustSymlink(t, filepath.Join(dir, "hooks", "g.sh"), dir, "hooks/b.sh")
		}, "./hooks/g.sh", []string{"g.sh", "does not resolve"}},
		{"a link out in the script's folder", func(t *testing.T, dir string) {
			writeScript(t, dir, "hooks/g.sh", "#!/bin/sh\n")
			mustSymlink(t, filepath.Join(outside, "evil.sh"), dir, "hooks/helper.sh")
		}, "./hooks/g.sh", []string{"helper.sh", "leads outside the harness"}},
		{"a link to a folder in the script's folder", func(t *testing.T, dir string) {
			writeScript(t, dir, "hooks/g.sh", "#!/bin/sh\n")
			if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
				t.Fatal(err)
			}
			mustSymlink(t, filepath.Join(dir, "lib"), dir, "hooks/lib")
		}, "./hooks/g.sh", []string{"lib", "a link to a folder"}},
		{"a missing script", func(t *testing.T, dir string) {}, "./hooks/g.sh", []string{"g.sh", "missing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("YNH_HOME", home)
			manifest := `{"name":"scr","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"` + tc.cmd + `"}]}}`
			id := installTrustedTestHarness(t, home, "scr", manifest)
			tc.setup(t, harness.InstalledDirByID(id))
			// Accepted again with the script as it now is: accepting changes nothing.
			if err := cmdTrustTo([]string{"accept", id}, io.Discard, io.Discard); err != nil {
				t.Fatal(err)
			}

			err := trustGate(id, loadInstalled(t, id), io.Discard)
			if !isUntrusted(err) {
				t.Fatalf("err = %v, want an untrusted refusal", err)
			}
			for _, want := range append(tc.want, "hook on_stop", "Fix:") {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal lacks %q:\n%s", want, err)
				}
			}
		})
	}
}

// Project code is not reviewable and never was, so it does not block; a
// regular covered script does not either.
func TestTrustGate_ProjectAndRegularScriptsDoNotBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	manifest := `{"name":"ok","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"./hooks/g.sh"},{"command":"bash scripts/wake.sh"}]}}`
	id := installListTestHarness(t, home, "ok", manifest)
	writeScript(t, harness.InstalledDirByID(id), "hooks/g.sh", "#!/bin/sh\n")
	if err := cmdTrustTo([]string{"accept", id}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := trustGate(id, loadInstalled(t, id), io.Discard); err != nil {
		t.Fatalf("err = %v", err)
	}
}

// A folder run's remote include is refused for a script it cannot read.
func TestTrustGate_FolderRunRefusesAnUnreadableScriptOfARemoteInclude(t *testing.T) {
	folder, src := remoteIncludeFolder(t)
	hooky := `{"name":"inc","version":"1.0.0","hooks":{"on_stop":[{"command":"./hooks/x.sh"}]}}`
	if err := writePluginJSONFile(src, []byte(hooky)); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, "/dev/zero", src, "hooks/x.sh")
	runGitInDir(t, src, "add", "-A")
	runGitInDir(t, src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "hook")
	if _, err := resolver.EnsureRepo("file://"+src, ""); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"folderh","version":"0.1.0","includes":[{"git":"file://` + src + `","hooks":true}]}`
	if err := writePluginJSONFile(folder, []byte(manifest)); err != nil {
		t.Fatal(err)
	}
	p, err := harness.LoadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	gerr := trustGate("", p, io.Discard)
	if !isUntrusted(gerr) || !strings.Contains(gerr.Error(), "x.sh") || !strings.Contains(gerr.Error(), "Fix:") {
		t.Fatalf("err = %v", gerr)
	}
}

// trust show says a skipped script blocks a run.
func TestTrustShowSaysASkippedScriptBlocksARun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "scr", `{"name":"scr","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"./hooks/g.sh"}]}}`)
	writeScript(t, harness.InstalledDirByID(id), "hooks/g.sh", "#!/bin/sh\n")
	mustSymlink(t, "/dev/zero", harness.InstalledDirByID(id), "hooks/z.sh")
	st := trustStatusFor(id, loadInstalled(t, id), trustStore{Harnesses: map[string]trustRecord{}}, nil)
	if out := showText(t, st); !strings.Contains(out, "skipped z.sh") || !strings.Contains(out, "this blocks a run") {
		t.Errorf("show:\n%s", out)
	}

	missing := installListTestHarness(t, home, "gone", `{"name":"gone","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"./hooks/g.sh"}]}}`)
	st = trustStatusFor(missing, loadInstalled(t, missing), trustStore{Harnesses: map[string]trustRecord{}}, nil)
	if out := showText(t, st); !strings.Contains(out, "is missing; this blocks a run") {
		t.Errorf("show of a missing script:\n%s", out)
	}
}

func mustSymlink(t *testing.T, target, dir, rel string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// A record for a remote include covers that include's commands alone, so two
// folders that share it share one acceptance: accepting the second does not
// undo the first (ynw-5l7hc).
func TestTrustGate_FoldersSharingARemoteIncludeShareItsRecord(t *testing.T) {
	a, src := remoteIncludeFolder(t)
	b := filepath.Join(filepath.Dir(a), "folderb")
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	// B includes the same remote and has a local include with a hook of its own.
	writeIncludedHarness(t, b, "localinc", hookFields("echo local"))
	manifest := `{"name":"folderb","version":"0.1.0","includes":[{"git":"file://` + src + `","ref":"` + gitRevParse(t, src) + `"},{"local":"localinc","hooks":true}]}`
	if err := writePluginJSONFile(b, []byte(manifest)); err != nil {
		t.Fatal(err)
	}
	pa, err := harness.LoadDir(a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := harness.LoadDir(b)
	if err != nil {
		t.Fatal(err)
	}

	if aerr := cmdTrustTo([]string{"accept", a}, io.Discard, io.Discard); aerr != nil {
		t.Fatal(aerr)
	}
	if gerr := trustGate("", pa, io.Discard); gerr != nil {
		t.Fatalf("A after accepting A: %v", gerr)
	}
	// B shares A's acceptance of the remote include: its local hook needs none.
	if gerr := trustGate("", pb, io.Discard); gerr != nil {
		t.Fatalf("B reads A's record of the shared include, and its local include is its own: %v", gerr)
	}
	if aerr := cmdTrustTo([]string{"accept", b}, io.Discard, io.Discard); aerr != nil {
		t.Fatal(aerr)
	}
	if gerr := trustGate("", pa, io.Discard); gerr != nil {
		t.Fatalf("accepting B undid A's acceptance: %v", gerr)
	}
}

// A folder whose remote include brings no commands is not refused because a
// local include has a hook (ynw-5l7hc).
func TestTrustGate_LocalIncludeCommandsAreNotTheRemoteIncludesRecord(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePluginJSONFile(remote, []byte(`{"name":"inc","version":"1.0.0"}`)); err != nil {
		t.Fatal(err)
	}
	runGitInDir(t, remote, "init", "-q", "-b", "main")
	runGitInDir(t, remote, "add", "-A")
	runGitInDir(t, remote, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	if _, err := resolver.EnsureRepo("file://"+remote, ""); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "folder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	writeIncludedHarness(t, folder, "localinc", hookFields("echo local"))
	manifest := `{"name":"folderh","version":"0.1.0","includes":[{"git":"file://` + remote + `"},{"local":"localinc","hooks":true}]}`
	if err := writePluginJSONFile(folder, []byte(manifest)); err != nil {
		t.Fatal(err)
	}
	p, err := harness.LoadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	if gerr := trustGate("", p, io.Discard); gerr != nil {
		t.Fatalf("a remote include with no commands, and a local hook: %v", gerr)
	}
}

// A harness that only requests workspace folders is refused until accepted,
// and the refusal lists the folders (ynh #978).
func TestTrustGate_WorkspaceOnlyRefusalListsFolders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "folders", `{"name":"folders","version":"0.1.0","default_vendor":"claude",`+
		`"workspace":["../sibling","~/notes"]}`)

	err := trustGate(id, loadInstalled(t, id), io.Discard)
	if !isUntrusted(err) {
		t.Fatalf("err = %v, want an untrusted refusal", err)
	}
	msg := err.Error()
	for _, want := range []string{"what it asks for", "It requests workspace folders:", "../sibling", "~/notes", "ynh trust accept " + id} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "what it executes") || strings.Contains(msg, "It executes:") {
		t.Errorf("refusal claims it executes something:\n%s", msg)
	}
}
