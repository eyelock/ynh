package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

// One full-SHA pin rule for every acceptance route (ynh #1002): trust accept,
// install --trust, run --trust and native delegate entries, checked before
// cloning where it can be.

// ynh trust accept <id> accepts a remote include that floats at the commit it
// was read at and records that commit, with a line saying so; it no longer
// refuses, so the way out that the run refusal and trust show name leads
// somewhere (ynw-3qlzu). A full-SHA include records no floating entry.
func TestTrustAccept_AcceptsFloatingIncludeAtItsCommit(t *testing.T) {
	for _, tc := range []struct{ name, ref, why string }{
		{"no ref", "", "floats"},
		{"branch", "branch", "tag or branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeHome(t)
			folder := installFolder(t, tc.ref)
			if out, err := installOut(t, folder); err != nil {
				t.Fatalf("plain install: %v\n%s", err, out)
			}
			p, err := harness.LoadQualified("local/fld")
			if err != nil {
				t.Fatal(err)
			}
			if st := trustStatusFor("local/fld", p, trustStore{Harnesses: map[string]trustRecord{}}, nil); st.State != trustUnreviewed {
				t.Fatalf("before accept: state %q, want unreviewed", st.State)
			}
			var out bytes.Buffer
			if err := cmdTrustTo([]string{"accept", "local/fld"}, &out, io.Discard); err != nil {
				t.Fatalf("floating include: %v", err)
			}
			store, _ := loadTrustStore()
			rec, ok := store.Harnesses["local/fld"]
			if !ok || len(rec.Floating) != 1 || !isFullSHA(rec.Floating[0].Commit) || !strings.Contains(rec.Floating[0].Why, tc.why) {
				t.Fatalf("record = %+v, want one floating include with a full commit", rec)
			}
			for _, want := range []string{"Floating, accepted at what is installed now", "accepted at commit " + rec.Floating[0].Commit} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if st := trustStatusFor("local/fld", p, store, nil); st.State != trustAccepted {
				t.Errorf("after accept: state %q, want accepted", st.State)
			}
		})
	}

	fakeHome(t)
	pinned := installFolder(t, "sha")
	if out, err := installOut(t, pinned); err != nil {
		t.Fatalf("plain install: %v\n%s", err, out)
	}
	var out bytes.Buffer
	if err := cmdTrustTo([]string{"accept", "local/fld"}, &out, io.Discard); err != nil {
		t.Fatalf("pinned include: %v", err)
	}
	if store, _ := loadTrustStore(); len(store.Harnesses["local/fld"].Floating) != 0 || strings.Contains(out.String(), "Floating") {
		t.Errorf("a pinned include was reported floating: %+v\n%s", store.Harnesses["local/fld"], out.String())
	}
}

// ynh trust accept <dir> takes the same rule: the include's record carries the
// commit, and nothing is refused.
func TestTrustAcceptFolder_AcceptsFloatingIncludeAtItsCommit(t *testing.T) {
	fakeHome(t)
	floating := installFolder(t, "")
	var out bytes.Buffer
	if err := cmdTrustTo([]string{"accept", floating}, &out, io.Discard); err != nil {
		t.Fatalf("floating include: %v", err)
	}
	store, _ := loadTrustStore()
	if len(store.Harnesses) != 1 {
		t.Fatalf("records = %v, want one include record", store.Harnesses)
	}
	for k, rec := range store.Harnesses {
		if !strings.HasPrefix(k, includeRecordPrefix) || !isFullSHA(rec.SHA) || len(rec.Floating) != 1 {
			t.Errorf("record %s = %+v, want the include's commit and floating entry", k, rec)
		}
	}
	if !strings.Contains(out.String(), "accepted at commit") {
		t.Errorf("output does not say which commit:\n%s", out.String())
	}
}

// A native delegate entry from a remote source is judged like an include; an
// A2A entry, a local one and the parent's own repository are not.
func TestUnpinnedNativeDelegates(t *testing.T) {
	sha := strings.Repeat("ab", 20)
	// The parent lives in its own git repository, so "." is its own repo.
	parent := t.TempDir()
	if out, err := exec.Command("git", "-C", parent, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	p := &harness.Harness{
		Dir:       parent,
		Transport: harness.TransportA2A,
		DelegatesTo: []harness.Delegate{
			{GitSource: harness.GitSource{Git: "github.com/x/branch", Ref: "main"}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Git: "github.com/x/none"}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Git: "github.com/x/pinned", Ref: sha}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Git: "github.com/x/a2a", Ref: "main"}},
			{GitSource: harness.GitSource{Git: ".", Ref: "main"}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Local: "../x"}, Transport: harness.TransportNative},
			// A git path to another repository is a clone of it, judged like
			// an include of the same source (resolver.IsOwnRepoSource).
			{GitSource: harness.GitSource{Git: "../other", Ref: "main"}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Git: "/abs/folder"}, Transport: harness.TransportNative},
		},
	}
	got := unpinnedNativeDelegates(p)
	if len(got) != 4 || !strings.Contains(got[0], "github.com/x/branch") || !strings.Contains(got[1], "github.com/x/none") ||
		!strings.Contains(got[2], "../other") || !strings.Contains(got[3], "/abs/folder") {
		t.Fatalf("unpinned = %q, want the branch, no-ref, relative-path and absolute-path native entries", got)
	}
	p.Transport = harness.TransportNative
	if got := unpinnedNativeDelegates(p); len(got) != 5 {
		t.Errorf("with a native default, the A2A-looking entry counts too: %q", got)
	}
}

// A floating native delegate entry: trust accept of a folder accepts it and
// says which commit (a local git folder's HEAD) or that none could be told,
// while run --trust, which accepts unreviewed, still refuses it.
func TestNativeDelegateEntry_AcceptedByTrustAcceptRefusedByRunTrust(t *testing.T) {
	fakeHome(t)
	deleg := t.TempDir()
	gitRepo(t, deleg, "del-sensor")
	// trust accept reads a delegate from the cache, as it reads an include.
	if _, err := resolver.EnsureRepo(deleg, ""); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, git string
		wantLine  string
	}{
		{"local git folder", deleg, "accepted at commit " + gitRevParse(t, deleg)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			body := `{"name":"nd","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"echo own"}]},` +
				`"delegates_to":[{"git":"` + tc.git + `","transport":"native"}]}`
			if err := writePluginJSONFile(dir, []byte(body)); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := cmdTrustTo([]string{"accept", dir}, &out, io.Discard); err != nil {
				t.Fatalf("trust accept <dir>: %v, want it accepted", err)
			}
			if !strings.Contains(out.String(), "native delegate "+tc.git) || !strings.Contains(out.String(), tc.wantLine) {
				t.Errorf("output lacks the entry and %q:\n%s", tc.wantLine, out.String())
			}
			p, err := harness.LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if err := acceptFolderRun(dir, p, io.Discard); err == nil || !strings.Contains(err.Error(), "native delegate "+tc.git) {
				t.Fatalf("run --trust: err = %v, want the native delegate named", err)
			}
		})
	}
}

// install --ref <branch> --trust refuses before it clones anything: the
// source here does not exist, so a clone would fail with "resolving".
func TestInstallTrust_RefusesBeforeCloning(t *testing.T) {
	fakeHome(t)
	for name, args := range map[string][]string{
		"a branch": {"file:///does/not/exist.git", "--ref", "develop", "--trust"},
		"no ref":   {"file:///does/not/exist.git", "--trust"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := installOut(t, args...)
			if err == nil || !strings.Contains(err.Error(), "--trust refuses") || strings.Contains(err.Error(), "resolving") {
				t.Fatalf("err = %v\n%s, want the pin refusal before any clone", err, out)
			}
		})
	}
}

// A full-SHA include whose own include floats: run --trust refuses, naming the
// nested include, and records nothing. The nested ref is only known once the
// parent is read, so the parent is already in the cache; nothing is accepted.
func TestRunTrust_NestedFloatingIncludeIsRefused(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()

	inner := filepath.Join(root, "inner")
	gitRepo(t, inner, "inner-sensor")

	mid := filepath.Join(root, "mid")
	if err := os.MkdirAll(mid, 0o755); err != nil {
		t.Fatal(err)
	}
	midBody := `{"name":"mid","version":"1.0.0","includes":[{"git":"file://` + inner + `","ref":"main"}]}`
	if err := writePluginJSONFile(mid, []byte(midBody)); err != nil {
		t.Fatal(err)
	}
	gitIn(t, mid, "init", "-q", "-b", "main")
	gitIn(t, mid, "add", ".")
	gitIn(t, mid, "commit", "-qm", "mid")
	midSHA := gitRevParse(t, mid)

	folder := filepath.Join(root, "folder")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"fld","version":"0.1.0","default_vendor":"claude","includes":[{"git":"file://` + mid + `","ref":"` + midSHA + `"}]}`
	if err := writePluginJSONFile(folder, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.EnsureRepo("file://"+mid, midSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.EnsureRepo("file://"+inner, "main"); err != nil {
		t.Fatal(err)
	}

	p, err := harness.LoadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	err = acceptFolderRun(folder, p, io.Discard)
	if err == nil || !strings.Contains(err.Error(), inner) || !strings.Contains(err.Error(), "tag or branch") {
		t.Fatalf("err = %v, want the nested include named as floating", err)
	}
	if store, _ := loadTrustStore(); len(store.Harnesses) != 0 {
		t.Errorf("a refused --trust recorded %v", store.Harnesses)
	}
}

// image --from --trust judges a floating native delegate entry like every
// other acceptance route, and bakes nothing.
func TestImageFromTrust_RefusesFloatingNativeDelegate(t *testing.T) {
	fakeHome(t)
	dir := t.TempDir()
	body := `{"name":"imgnd","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"echo own"}]},` +
		`"delegates_to":[{"git":"github.com/example/reviewer","ref":"main","transport":"native"}]}`
	if err := writePluginJSONFile(dir, []byte(body)); err != nil {
		t.Fatal(err)
	}
	p, err := harness.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := imageFromTrustRecord(p, &config.Config{}, imageArgs{name: "imgnd", from: dir, trust: true}, true, "")
	if err == nil || !strings.Contains(err.Error(), "native delegate github.com/example/reviewer") {
		t.Fatalf("err = %v, rec = %v, want the native delegate named", err, rec)
	}
	if rec != nil {
		t.Errorf("a refused build returned a record: %v", rec)
	}
}

// The bead's scenario (ynw-3qlzu): a harness with a native delegate entry that
// names another git repository by absolute path, no ref, whose harness ops
// declares an MCP server. The gate refuses it, trust show says it is not yet
// reviewed, and trust accept, which they both name, now accepts it and records
// the delegate's commit, after which the gate passes.
func TestFloatingNativeDelegate_RefusalShowAndAcceptAgree(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	deleg := filepath.Join(root, "ops")
	if err := os.MkdirAll(deleg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePluginJSONFile(deleg, []byte(`{"name":"ops","version":"0.1.0","default_vendor":"claude","mcp_servers":{"db":{"command":"echo","args":["x"]}}}`)); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "ops"}} {
		if out, err := exec.Command("git", append([]string{"-C", deleg}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	team := filepath.Join(root, "team")
	if err := os.MkdirAll(team, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePluginJSONFile(team, []byte(`{"name":"team","version":"0.1.0","default_vendor":"claude","delegates_to":[{"git":"`+deleg+`","transport":"native"}]}`)); err != nil {
		t.Fatal(err)
	}
	if out, err := installOut(t, deleg); err != nil {
		t.Fatalf("install delegate: %v\n%s", err, out)
	}
	if err := cmdTrustTo([]string{"accept", "local/ops"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("accept delegate: %v", err)
	}
	if out, err := installOut(t, team); err != nil {
		t.Fatalf("install team: %v\n%s", err, out)
	}
	p, err := harness.LoadQualified("local/team")
	if err != nil {
		t.Fatal(err)
	}
	gerr := trustGate("local/team", p, io.Discard)
	if !isUntrusted(gerr) || !strings.Contains(gerr.Error(), "ynh trust accept local/team") {
		t.Fatalf("gate: %v, want a refusal naming 'ynh trust accept local/team'", gerr)
	}
	var out bytes.Buffer
	if err := cmdTrustTo([]string{"accept", "local/team"}, &out, io.Discard); err != nil {
		t.Fatalf("the accept the refusal names refused: %v", err)
	}
	if !strings.Contains(out.String(), "native delegate "+deleg) || !strings.Contains(out.String(), "accepted at commit "+gitRevParse(t, deleg)) {
		t.Errorf("accept does not say which commit:\n%s", out.String())
	}
	store, _ := loadTrustStore()
	if rec := store.Harnesses["local/team"]; len(rec.Floating) != 1 || rec.Floating[0].Commit != gitRevParse(t, deleg) {
		t.Errorf("record = %+v, want the delegate's commit", rec)
	}
	if gerr := trustGate("local/team", p, io.Discard); gerr != nil {
		t.Errorf("after accept the gate still refuses: %v", gerr)
	}
}
