package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

func TestFullSHA(t *testing.T) {
	sha := strings.Repeat("a1", 20)
	for s, want := range map[string]bool{
		sha: true, strings.ToUpper(sha): true,
		sha[:39]: false, sha + "0": false, "": false, "v1.2.3": false, "main": false, "abc1234": false,
		strings.Repeat("g", 40): false,
	} {
		if got := isFullSHA(s); got != want {
			t.Errorf("isFullSHA(%q) = %v, want %v", s, got, want)
		}
	}
	for ref, want := range map[string]string{
		"":        "floats",
		"v1.0":    "tag or branch",
		"abc123":  "tag or branch",
		"abc1234": "short SHA",
		sha:       "",
	} {
		got := notPinned(ref)
		if (want == "") != (got == "") || !strings.Contains(got, want) {
			t.Errorf("notPinned(%q) = %q, want it to mention %q", ref, got, want)
		}
	}
}

// remoteSource is a git repository holding a harness "hh" with one hook, and
// its head commit.
func remoteSource(t *testing.T, includes string) (src, sha string) {
	t.Helper()
	src = t.TempDir()
	body := `{"name":"hh","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"echo done"}]}` + includes + `}`
	if err := writePluginJSONFile(src, []byte(body)); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, "init", "-q")
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-qm", "hh")
	return src, gitRevParse(t, src)
}

func installOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	var err error
	captureStdout(t, &out, func() { err = cmdInstall(args) })
	return out.String(), err
}

// install --ref <full-sha> --trust installs and records the acceptance, saying
// how and what, and the harness then passes the gate.
func TestInstallTrust_AtAFullSHA(t *testing.T) {
	fakeHome(t)
	src, sha := remoteSource(t, "")
	out, err := installOut(t, "file://"+src, "--ref", sha, "--trust")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	for _, want := range []string{"Accepted local/hh (install --trust)", "hook on_stop: echo done"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	p, lerr := harness.LoadByID("local/hh")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if gerr := trustGate("local/hh", p, io.Discard); gerr != nil {
		t.Fatalf("gate: %v", gerr)
	}
	store, _ := loadTrustStore()
	if got := store.Harnesses["local/hh"].Via; got != viaInstallTrus {
		t.Errorf("via = %q", got)
	}
}

// A tag, a branch, a short SHA or no ref makes --trust refuse with the reason,
// and nothing is installed.
func TestInstallTrust_RefusesWhatIsNotPinned(t *testing.T) {
	fakeHome(t)
	src, _ := remoteSource(t, "")
	gitIn(t, src, "tag", "v1")
	for name, args := range map[string][]string{
		"a tag":  {"file://" + src, "--ref", "v1", "--trust"},
		"no ref": {"file://" + src, "--trust"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := installOut(t, args...)
			if err == nil || !strings.Contains(err.Error(), "--trust refuses") || !strings.Contains(err.Error(), "40-character commit SHA") {
				t.Fatalf("err = %v\n%s", err, out)
			}
			if list, _ := harness.ListAll(); len(list) != 0 {
				t.Errorf("a refused --trust installed %v", list)
			}
		})
	}
	// The same install without --trust works and stays refused by the gate.
	if out, err := installOut(t, "file://"+src, "--ref", "v1"); err != nil {
		t.Fatalf("plain install: %v\n%s", err, out)
	}
	p, _ := harness.LoadByID("local/hh")
	if gerr := trustGate("local/hh", p, io.Discard); !isUntrusted(gerr) {
		t.Errorf("gate = %v, want an untrusted refusal until accepted", gerr)
	}
}

// installFolder makes a local folder harness with a remote include, at ref.
func installFolder(t *testing.T, includeRef string) (folder string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	gitRepo(t, remote, "remote-sensor")
	folder = filepath.Join(root, "folder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	ref := ""
	switch includeRef {
	case "sha":
		ref = `,"ref":"` + gitRevParse(t, remote) + `"`
	case "branch":
		ref = `,"ref":"main"`
	}
	manifest := `{"name":"fld","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"echo own"}]},"includes":[{"git":"file://` + remote + `"` + ref + `}]}`
	if err := writePluginJSONFile(folder, []byte(manifest)); err != nil {
		t.Fatal(err)
	}
	cacheRef := ""
	switch includeRef {
	case "sha":
		cacheRef = gitRevParse(t, remote)
	case "branch":
		cacheRef = "main"
	}
	if _, err := resolver.EnsureRepo("file://"+remote, cacheRef); err != nil {
		t.Fatal(err)
	}
	return folder
}

// A local install takes --trust when its remote includes that bring commands
// are at full SHAs (4c); a floating one makes it refuse.
func TestInstallTrust_LocalFolder(t *testing.T) {
	fakeHome(t)
	floating := installFolder(t, "")
	if out, err := installOut(t, floating, "--trust"); err == nil || !strings.Contains(err.Error(), "floats") {
		t.Fatalf("floating include: err = %v\n%s", err, out)
	}
	if list, _ := harness.ListAll(); len(list) != 0 {
		t.Errorf("a refused --trust installed %v", list)
	}
	branch := installFolder(t, "branch")
	if _, err := installOut(t, branch, "--trust"); err == nil || !strings.Contains(err.Error(), "tag or branch") {
		t.Fatalf("branch include: err = %v", err)
	}

	pinned := installFolder(t, "sha")
	out, err := installOut(t, pinned, "--trust")
	if err != nil {
		t.Fatalf("pinned: %v\n%s", err, out)
	}
	p, _ := harness.LoadByID("local/fld")
	if gerr := trustGate("local/fld", p, io.Discard); gerr != nil {
		t.Fatalf("gate: %v", gerr)
	}
}

// run --trust accepts only a folder's full-SHA includes, records how, and
// never an installed harness.
func TestRunTrust_FolderRemoteIncludes(t *testing.T) {
	fakeHome(t)
	floating := installFolder(t, "")
	p, err := harness.LoadDir(floating)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := acceptFolderRun(floating, p, &out); err == nil || !strings.Contains(err.Error(), "floats") {
		t.Fatalf("floating include: %v", err)
	}
	if gerr := trustGate("", p, io.Discard); !isUntrusted(gerr) {
		t.Fatalf("a refused --trust must not accept: %v", gerr)
	}

	pinned := installFolder(t, "sha")
	pp, err := harness.LoadDir(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptFolderRun(pinned, pp, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "(run --trust)") {
		t.Errorf("output: %s", out.String())
	}
	if gerr := trustGate("", pp, io.Discard); gerr != nil {
		t.Fatalf("gate after run --trust: %v", gerr)
	}
	store, _ := loadTrustStore()
	vias := 0
	for k, rec := range store.Harnesses {
		if strings.HasPrefix(k, includeRecordPrefix) && rec.Via == viaRunTrust {
			vias++
		}
	}
	if vias != 1 {
		t.Errorf("records via run --trust = %d", vias)
	}

	// An installed harness never takes --trust at run time.
	installed := installListTestHarness(t, os.Getenv("YNH_HOME"), "inst", plainHarness)
	if err := cmdRun([]string{installed, "--trust"}); err == nil || !strings.Contains(err.Error(), "ynh trust accept") {
		t.Errorf("installed + --trust: %v", err)
	}
}

// After an update or include change that moves an accepted harness's commands,
// the command says it is now refused and why.
func TestUpdateSaysTheHarnessIsNoLongerTrusted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installTrustedTestHarness(t, home, "mv", hookedHarness)
	before := trustStateOf(id)
	if before != trustAccepted {
		t.Fatalf("state = %q", before)
	}
	var quiet bytes.Buffer
	noteUntrustedAfterChange(id, before, &quiet)
	if quiet.Len() != 0 {
		t.Errorf("a note with nothing changed: %q", quiet.String())
	}

	installListTestHarness(t, home, "mv", strings.Replace(hookedHarness, "echo done", "echo moved", 1))
	var out bytes.Buffer
	noteUntrustedAfterChange(id, before, &out)
	for _, want := range []string{"is no longer trusted", "ynh run, ynh agent run and ynh check refuse it", "ynh trust accept " + id} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("note lacks %q:\n%s", want, out.String())
		}
	}
	// A harness that was never accepted gets no note: it was refused already.
	out.Reset()
	noteUntrustedAfterChange(id, trustUnreviewed, &out)
	if out.Len() != 0 {
		t.Errorf("note for an unreviewed harness: %q", out.String())
	}
}

// ynh include add on an accepted installed harness says so when it changes
// what the harness executes.
func TestIncludeAddSaysTheHarnessIsNoLongerTrusted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installTrustedTestHarness(t, home, "inc", `{"name":"inc","version":"0.1.0","default_vendor":"claude"}`)
	dir := harness.InstalledDirByID(id)
	writeIncludedHarness(t, dir, "extra", hookFields("echo extra"))
	var out bytes.Buffer
	if err := cmdIncludeTo([]string{"add", id, filepath.Join(dir, "extra"), "--hooks"}, &out); err != nil {
		t.Fatalf("include add: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "is no longer trusted") {
		t.Errorf("no note:\n%s", out.String())
	}
}

// trust show and ls print how an acceptance was recorded.
func TestTrustShowAndLsPrintVia(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "vv", hookedHarness)
	if err := cmdTrustTo([]string{"accept", id}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	var show, ls, js bytes.Buffer
	if err := cmdTrustTo([]string{"show", id}, &show, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(show.String(), "via:      accept") {
		t.Errorf("show:\n%s", show.String())
	}
	if err := cmdTrustTo([]string{"ls"}, &ls, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ls.String(), "VIA") || !strings.Contains(ls.String(), "accept") {
		t.Errorf("ls:\n%s", ls.String())
	}
	if err := cmdTrustTo([]string{"show", id, "--format", "json"}, &js, io.Discard); err != nil {
		t.Fatal(err)
	}
	var st struct {
		AcceptedVia string `json:"accepted_via"`
	}
	if err := json.Unmarshal(js.Bytes(), &st); err != nil || st.AcceptedVia != "accept" {
		t.Errorf("json: %v %s", err, js.String())
	}
}

// ynh include add --trust accepts the harness again in one step after the
// change, recording 'include --trust', and a path argument refuses it.
func TestIncludeAddTrust_AcceptsInOneStep(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installTrustedTestHarness(t, home, "inct", `{"name":"inct","version":"0.1.0","default_vendor":"claude"}`)
	dir := harness.InstalledDirByID(id)
	writeIncludedHarness(t, dir, "extra", hookFields("echo extra"))
	var out bytes.Buffer
	if err := cmdIncludeTo([]string{"add", id, filepath.Join(dir, "extra"), "--hooks", "--trust"}, &out); err != nil {
		t.Fatalf("include add --trust: %v\n%s", err, out.String())
	}
	if strings.Contains(out.String(), "no longer trusted") || !strings.Contains(out.String(), "Accepted "+id+" (include --trust)") {
		t.Errorf("output:\n%s", out.String())
	}
	if got := trustStateOf(id); got != trustAccepted {
		t.Errorf("state = %q, want accepted", got)
	}
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.Harnesses[id].Via != viaIncludeTrus {
		t.Errorf("via = %q", store.Harnesses[id].Via)
	}
	if err := cmdIncludeTo([]string{"add", "./folder", "x", "--trust"}, io.Discard); err == nil || !strings.Contains(err.Error(), "installed harness id") {
		t.Errorf("path with --trust: %v", err)
	}
	if err := cmdIncludeTo([]string{"remove", id, "x", "--trust"}, io.Discard); err == nil {
		t.Error("include remove --trust was accepted")
	}
}

// ynh update --trust records 'update --trust'; a floating remote include
// makes it refuse before anything is fetched.
func TestUpdateTrust(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "upt", hookedHarness)
	if err := cmdUpdate([]string{id, "--trust"}); err != nil {
		t.Fatalf("update --trust: %v", err)
	}
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.Harnesses[id].Via != viaUpdateTrust || trustStateOf(id) != trustAccepted {
		t.Errorf("record = %+v, state %q", store.Harnesses[id], trustStateOf(id))
	}
	if err := cmdUpdate([]string{"./folder", "--trust"}); err == nil || !strings.Contains(err.Error(), "installed harness id") {
		t.Errorf("path with --trust: %v", err)
	}
}

func TestUpdateTrust_RefusesFloatingInclude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	remote := filepath.Join(t.TempDir(), "remote")
	gitRepo(t, remote, "remote-sensor")
	if _, err := resolver.EnsureRepo("file://"+remote, ""); err != nil {
		t.Fatal(err)
	}
	id := installListTestHarness(t, home, "upf", `{"name":"upf","version":"0.1.0","default_vendor":"claude","includes":[{"git":"file://`+remote+`"}]}`)
	err := cmdUpdate([]string{id, "--trust"})
	if err == nil || !strings.Contains(err.Error(), "full commit SHA") {
		t.Fatalf("err = %v", err)
	}
	if got := trustStateOf(id); got == trustAccepted {
		t.Error("a refused --trust accepted the harness")
	}
}
