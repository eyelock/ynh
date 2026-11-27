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
	return makeFolder(t, includeRef, true)
}

// coldFolder is installFolder with the include cache left empty, as on a
// machine that has never fetched the include (ynw-twt3a).
func coldFolder(t *testing.T, includeRef string) (folder string) {
	t.Helper()
	return makeFolder(t, includeRef, false)
}

func makeFolder(t *testing.T, includeRef string, warm bool) (folder string) {
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
	if warm {
		if _, err := resolver.EnsureRepo("file://"+remote, cacheRef); err != nil {
			t.Fatal(err)
		}
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

// The harness of ynh include add is its first positional, wherever the flags
// sit: --trust and the "no longer trusted" note find it either way.
func TestIncludeHarnessRef(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"local/h", "src"}, "local/h"},
		{[]string{"--ref", "abc", "local/h", "src", "--hooks"}, "local/h"},
		{[]string{"--pick", "a,b", "--as", "x", "local/h", "src"}, "local/h"},
		{[]string{"--from-path", "p", "local/h", "src"}, "local/h"},
		{[]string{"--hooks"}, ""},
	} {
		if got := includeHarnessRef(tt.args); got != tt.want {
			t.Errorf("includeHarnessRef(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestIncludeAddTrust_FlagsBeforeTheHarness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installTrustedTestHarness(t, home, "incf", `{"name":"incf","version":"0.1.0","default_vendor":"claude"}`)
	dir := harness.InstalledDirByID(id)
	writeIncludedHarness(t, dir, "extra", hookFields("echo extra"))
	var out bytes.Buffer
	if err := cmdIncludeTo([]string{"add", "--hooks", "--trust", id, filepath.Join(dir, "extra")}, &out); err != nil {
		t.Fatalf("include add with flags first: %v\n%s", err, out.String())
	}
	if got := trustStateOf(id); got != trustAccepted {
		t.Errorf("state = %q, want accepted", got)
	}

	// Without --trust, the note still appears with the flags first.
	id2 := installTrustedTestHarness(t, home, "incg", `{"name":"incg","version":"0.1.0","default_vendor":"claude"}`)
	dir2 := harness.InstalledDirByID(id2)
	writeIncludedHarness(t, dir2, "extra", hookFields("echo extra"))
	out.Reset()
	if err := cmdIncludeTo([]string{"add", "--hooks", id2, filepath.Join(dir2, "extra")}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no longer trusted") {
		t.Errorf("no note with flags first:\n%s", out.String())
	}
}

// A run from a folder found by --harness-file, with a relative path whose
// directory is not a path ref ("hs"), is a folder run for --trust.
func TestRunTrust_RelativeHarnessFileIsAFolderRun(t *testing.T) {
	isolateRun(t)
	root := t.TempDir()
	t.Chdir(root)
	writeSkillHarness(t, filepath.Join(root, "hs"), "hsx")
	err := cmdRun([]string{"--harness-file", "hs/.agents/harness/plugin.json", "--trust", "-v", "cursor", "--install"})
	if err != nil && strings.Contains(err.Error(), "--trust is for a run from a folder") {
		t.Fatalf("a folder run was refused as an installed harness: %v", err)
	}
}

// uncachedFolder is installFolder without warming the include cache: a folder
// with a file:// git include at ref ("sha", "branch" or floating), the include
// not yet fetched. It returns the folder and the include's source URL and ref.
func uncachedFolder(t *testing.T, includeRef string) (folder, url, ref string) {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	gitRepo(t, remote, "remote-sensor")
	folder = filepath.Join(root, "folder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	refJSON := ""
	switch includeRef {
	case "sha":
		ref = gitRevParse(t, remote)
		refJSON = `,"ref":"` + ref + `"`
	case "branch":
		ref = "main"
		refJSON = `,"ref":"main"`
	}
	url = "file://" + remote
	manifest := `{"name":"fld","version":"0.1.0","default_vendor":"claude","includes":[{"git":"` + url + `"` + refJSON + `}]}`
	if err := writePluginJSONFile(folder, []byte(manifest)); err != nil {
		t.Fatal(err)
	}
	return folder, url, ref
}

// ynh #993: --trust fetches a full-SHA include missing from the cache, once,
// accepts it, and the gate then passes. Without --trust the gate stays
// offline: it refuses, fetches nothing, and its Fix line names --trust.
func TestRunTrust_FetchesUncachedFullSHAInclude(t *testing.T) {
	fakeHome(t)
	folder, url, ref := uncachedFolder(t, "sha")
	p, err := harness.LoadDir(folder)
	if err != nil {
		t.Fatal(err)
	}

	gerr := trustGate("", p, io.Discard)
	if !isUntrusted(gerr) {
		t.Fatalf("an uncached include must refuse offline: %v", gerr)
	}
	if strings.Contains(gerr.Error(), "run `ynh update`") {
		t.Errorf("a stray ynh update hint:\n%v", gerr)
	}
	for _, want := range []string{"--trust", "40-character"} {
		if !strings.Contains(gerr.Error(), want) {
			t.Errorf("Fix line lacks %q:\n%v", want, gerr)
		}
	}
	if strings.Contains(gerr.Error(), "Fetch them with 'ynh update'") {
		t.Errorf("a folder run is pointed at ynh update, which takes installed ids only:\n%v", gerr)
	}
	if _, ok := resolver.LookupCache(url, ref); ok {
		t.Fatal("the gate fetched without --trust")
	}

	var out bytes.Buffer
	if err := acceptFolderRun(folder, p, &out); err != nil {
		t.Fatalf("run --trust: %v", err)
	}
	if _, ok := resolver.LookupCache(url, ref); !ok {
		t.Fatal("--trust did not fetch the include")
	}
	if !strings.Contains(out.String(), "(run --trust)") {
		t.Errorf("output: %s", out.String())
	}
	if gerr := trustGate("", p, io.Discard); gerr != nil {
		t.Fatalf("gate after run --trust: %v", gerr)
	}
}

// --trust with an uncached include that is not full-SHA pinned refuses and
// fetches nothing, a branch and a floating include alike.
func TestRunTrust_RefusesUncachedUnpinnedIncludeWithoutFetching(t *testing.T) {
	for _, kind := range []string{"branch", "floating"} {
		t.Run(kind, func(t *testing.T) {
			fakeHome(t)
			folder, url, ref := uncachedFolder(t, kind)
			p, err := harness.LoadDir(folder)
			if err != nil {
				t.Fatal(err)
			}
			err = acceptFolderRun(folder, p, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "full commit SHA") {
				t.Fatalf("err = %v, want the unpinned refusal", err)
			}
			// ynh trust accept reads from the cache, so it cannot help here and
			// the closing line must not offer it as a way out (ynw-jsjts).
			if strings.Contains(err.Error(), "accept it by hand") || !strings.Contains(err.Error(), "run it again with --trust") {
				t.Errorf("closing line offers a route that loops:\n%v", err)
			}
			if _, ok := resolver.LookupCache(url, ref); ok {
				t.Error("an unpinned include was fetched")
			}
			if gerr := trustGate("", p, io.Discard); !isUntrusted(gerr) {
				t.Errorf("a refused --trust must not accept: %v", gerr)
			}
		})
	}
}

// A cached include is not fetched again: removing the source after the first
// --trust leaves a second one working from the cache.
func TestRunTrust_DoesNotRefetchACachedInclude(t *testing.T) {
	fakeHome(t)
	folder, url, ref := uncachedFolder(t, "sha")
	p, err := harness.LoadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptFolderRun(folder, p, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(strings.TrimPrefix(url, "file://")); err != nil {
		t.Fatal(err)
	}
	if err := acceptFolderRun(folder, p, io.Discard); err != nil {
		t.Fatalf("a second --trust reached for the network: %v", err)
	}
	if _, ok := resolver.LookupCache(url, ref); !ok {
		t.Error("cache entry lost")
	}
}

// `ynh run <folder> --trust` and `ynh agent run --harness <folder> --trust`
// both reach the fetch, and neither is refused as untrusted afterwards.
func TestRunTrust_BothRoutesFetchAnUncachedInclude(t *testing.T) {
	t.Run("run", func(t *testing.T) {
		isolateRun(t)
		folder, url, ref := uncachedFolder(t, "sha")
		err := cmdRun([]string{folder, "--trust", "-v", "claude", "--install"})
		if isUntrusted(err) {
			t.Fatalf("run --trust was refused by the gate: %v", err)
		}
		if _, ok := resolver.LookupCache(url, ref); !ok {
			t.Fatal("ynh run --trust did not fetch the include")
		}
	})
	t.Run("agent run", func(t *testing.T) {
		isolateRun(t)
		t.Setenv("YNH_AGENT_SESSION", "")
		vendorShims(t, "no worker is needed to reach the gate")
		folder, url, ref := uncachedFolder(t, "sha")
		r, _, _ := runAgentRefused(t, "--task", "t", "--no-plan", "--harness", folder, "--trust")
		if r.ErrorCode == errCodeUntrusted {
			t.Fatalf("agent run --trust was refused by the gate: %+v", r)
		}
		if _, ok := resolver.LookupCache(url, ref); !ok {
			t.Fatal("ynh agent run --trust did not fetch the include")
		}
	})
}

// ynh check <folder> with an uncached include fails before the gate. Its hint
// must not name ynh update, which takes installed ids only (ynw-lx3cs).
func TestCheckFolder_UncachedIncludeDoesNotNameUpdate(t *testing.T) {
	fakeHome(t)
	folder, _, _ := uncachedFolder(t, "sha")
	var stdout, stderr bytes.Buffer
	err := cmdCheck([]string{folder, "--cwd", t.TempDir()}, &stdout, &stderr)
	if err == nil {
		t.Fatal("check of an uncached include must fail")
	}
	msg := err.Error()
	if strings.Contains(msg, "run `ynh update`") {
		t.Errorf("a folder check is pointed at ynh update:\n%v", msg)
	}
	if !strings.Contains(msg, "ynh run <folder> --trust") {
		t.Errorf("no route named:\n%v", msg)
	}
}
