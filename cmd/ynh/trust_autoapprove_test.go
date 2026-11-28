package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/permission"
	"github.com/eyelock/ynh/internal/plugin"
)

// ynh #1058 P2: auto_approve enters the digest only when declared (D8).

func TestDigestIgnoresUndeclaredAutoApprove(t *testing.T) {
	sensors := map[string]plugin.Sensor{"lint": cmdSensor("golangci-lint run", "")}
	p := harnessWithSensors(t, sensors)
	// Pinned: the digest an earlier ynh recorded for this harness. It must
	// not move, or every existing acceptance would read as changed.
	const want = "sha256:8d0656778d8daf1d"
	if got := trustDigest(p); got != want {
		t.Errorf("digest of a harness declaring no level = %s, want the pre-feature %s", got, want)
	}
}

func TestDigestMovesWithDeclaredAutoApprove(t *testing.T) {
	base := harnessWithSensors(t, map[string]plugin.Sensor{"lint": cmdSensor("golangci-lint run", "")})
	seen := map[string]string{"(none)": trustDigest(base)}
	for _, level := range []string{"none", "edits", "all"} {
		p := harnessWithSensors(t, map[string]plugin.Sensor{"lint": cmdSensor("golangci-lint run", "")})
		p.AutoApprove = level
		d := trustDigest(p)
		for other, od := range seen {
			if d == od {
				t.Errorf("level %q has the same digest as %s", level, other)
			}
		}
		seen[level] = d
	}
}

func TestDigestMovesWithDeclaredEntryAutoApprove(t *testing.T) {
	mk := func(level string) *harness.Harness {
		return &harness.Harness{Name: "h", DelegatesTo: []harness.Delegate{{
			GitSource: harness.GitSource{Git: "github.com/o/reviewer"}, AutoApprove: level,
		}}}
	}
	// Computing the set reads delegates; a delegate that is not installed is a
	// problem and yields no digest, so compare the entry keys it digests.
	a, b, c := entryKeyOf(mk("")), entryKeyOf(mk("none")), entryKeyOf(mk("edits"))
	if a == b || b == c || a == c {
		t.Errorf("entry keys must differ by declared level: %q %q %q", a, b, c)
	}
	if strings.Contains(a, "auto_approve") {
		t.Errorf("an entry declaring no level must keep its key: %q", a)
	}
}

func entryKeyOf(h *harness.Harness) string { return entryKey(h.DelegatesTo[0]) }

func TestTrustShowListsTheLevel(t *testing.T) {
	p := harnessWithSensors(t, nil)
	st := trustStatusFor("id", p, trustStore{Harnesses: map[string]trustRecord{}}, nil)
	st.AutoApprove, st.AutoApproveFrom = string(p.AutoApproveOf()), string(p.AutoApproveOrigin())
	out := showText(t, st)
	if !strings.Contains(out, "opens at all when run unattended") {
		t.Errorf("a no-commands harness must list its level:\n%s", out)
	}

	p.AutoApprove = "edits"
	st.AutoApprove, st.AutoApproveFrom = string(p.AutoApproveOf()), string(p.AutoApproveOrigin())
	st.DelegateLevels = []string{"github.com/o/reviewer: none"}
	out = showText(t, st)
	for _, want := range []string{"opens at edits when run unattended", "declared by the harness", "delegate github.com/o/reviewer: none"} {
		if !strings.Contains(out, want) {
			t.Errorf("trust show should contain %q:\n%s", want, out)
		}
	}
}

func TestOneShotRunOfUnacceptedNoCommandsHarnessIsRefused(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	p := harnessWithSensors(t, nil)
	const id = "github.com/someone/skills-only"

	// Interactive: still needs nothing.
	if err := trustGate(id, p, nil); err != nil {
		t.Fatalf("an interactive run of a no-commands harness needs no acceptance: %v", err)
	}
	// One-shot: refused until accepted.
	err := oneShotAcceptanceGate(id, p)
	if err == nil || !isUntrusted(err) {
		t.Fatalf("a one-shot run must be refused, got %v", err)
	}
	if !strings.Contains(err.Error(), "ynh trust accept "+id) || !strings.Contains(err.Error(), "opens at all") {
		t.Errorf("refusal should name the level and the fix: %v", err)
	}

	store, _ := loadTrustStore()
	store.Harnesses[id] = trustRecord{Digest: executablesDigest(harnessExecutables(p))}
	if err := saveTrustStore(store); err != nil {
		t.Fatal(err)
	}
	if err := oneShotAcceptanceGate(id, p); err != nil {
		t.Errorf("accepted, the one-shot run is allowed: %v", err)
	}

	// Declaring a level voids the acceptance.
	p.AutoApprove = "none"
	if err := trustGate(id, p, nil); err == nil {
		t.Error("declaring a level must void an earlier acceptance")
	}
}

func TestOneShotGateLeavesAFolderRunAlone(t *testing.T) {
	if err := oneShotAcceptanceGate("", harnessWithSensors(t, nil)); err != nil {
		t.Errorf("folder runs are the folder route's: %v", err)
	}
}

// acceptForTest records an acceptance of the installed harness id, as
// `ynh trust accept` does.
func acceptForTest(t *testing.T, id string) {
	t.Helper()
	p, err := harness.LoadQualified(id)
	if err != nil {
		t.Fatal(err)
	}
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	rec := trustRecord{Digest: executablesDigest(harnessExecutables(p)), Scheme: trustScheme}
	if pr := p.InstalledFrom; pr != nil {
		rec.Source, rec.Ref, rec.SHA = pr.Source, pr.Ref, pr.SHA
	}
	store.Harnesses[id] = rec
	if err := saveTrustStore(store); err != nil {
		t.Fatal(err)
	}
}

func TestTrustAcceptSaysTheLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "lvl", `{"name":"lvl","version":"0.1.0","auto_approve":"edits"}`)

	var out, errb bytes.Buffer
	if err := cmdTrustAccept([]string{"local/lvl"}, &out, &errb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "opens at edits when run unattended (harness)") {
		t.Errorf("accept text must say the level and its source:\n%s", out.String())
	}

	var js bytes.Buffer
	if err := cmdTrustAccept([]string{"local/lvl", "--format", "json"}, &js, &errb); err != nil {
		t.Fatal(err)
	}
	var st struct {
		AutoApprove     string `json:"auto_approve"`
		AutoApproveFrom string `json:"auto_approve_from"`
	}
	if err := json.Unmarshal(js.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.AutoApprove != "edits" || st.AutoApproveFrom != "harness" {
		t.Errorf("accept JSON level = %+v", st)
	}
}

func TestTrustAcceptListsEntryLevels(t *testing.T) {
	set := trustSet{DelegateLevels: []string{"github.com/o/reviewer: none"}}
	var b bytes.Buffer
	printAcceptedLevel(&b, &harness.Harness{}, set)
	for _, want := range []string{"opens at all when run unattended (default)", "delegate github.com/o/reviewer: none"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("want %q in:\n%s", want, b.String())
		}
	}
}

// ynw-44fxh: a harness opening at all says what running as root does to it, in
// trust show and trust accept, in text and JSON; any other level says nothing.
func TestTrustSaysAllAsRootNeedsASandboxVariable(t *testing.T) {
	note := permission.RootAllNote
	for _, want := range []string{"IS_SANDBOX=1", "CLAUDE_CODE_BUBBLEWRAP", "Claude Code run", "Other vendors are unaffected"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the note must name %q: %s", want, note)
		}
	}

	all := harnessWithSensors(t, nil) // declares nothing: opens at all
	edits := harnessWithSensors(t, nil)
	edits.AutoApprove = "edits"

	var st trustStatus
	fillAutoApprove(&st, all)
	if st.AutoApproveAsRoot != note {
		t.Errorf("JSON field for all = %q", st.AutoApproveAsRoot)
	}
	if out := showText(t, st); !strings.Contains(out, note) {
		t.Errorf("trust show text lacks the note:\n%s", out)
	}
	var b bytes.Buffer
	printAcceptedLevel(&b, all, trustSet{})
	if !strings.Contains(b.String(), note) {
		t.Errorf("trust accept text lacks the note:\n%s", b.String())
	}
	js, err := json.Marshal(st)
	if err != nil || !strings.Contains(string(js), `"auto_approve_as_root"`) {
		t.Errorf("JSON lacks auto_approve_as_root: %s (%v)", js, err)
	}

	var e trustStatus
	fillAutoApprove(&e, edits)
	b.Reset()
	printAcceptedLevel(&b, edits, trustSet{})
	if e.AutoApproveAsRoot != "" || strings.Contains(b.String(), "IS_SANDBOX") || strings.Contains(showText(t, e), "IS_SANDBOX") {
		t.Errorf("a level other than all must not carry the note: %+v", e)
	}
}

// ynw-xngvy: the vendor is chosen per run, so the note is shown for every
// harness at all whatever its default vendor, and it is worded for Claude Code
// alone.
func TestRootNoteIsShownAtAllWhateverTheDefaultVendor(t *testing.T) {
	if permission.RootNote(permission.All) == "" {
		t.Fatal("level all must carry the note")
	}
	if permission.RootNote(permission.Edits) != "" || permission.RootNote(permission.None) != "" {
		t.Error("a level other than all carries no note")
	}
	for _, v := range []string{"", "claude", "codex", "cursor", "opencode", "copilot"} {
		h := harnessWithSensors(t, nil)
		h.DefaultVendor = v
		var st trustStatus
		fillAutoApprove(&st, h)
		var b bytes.Buffer
		printAcceptedLevel(&b, h, trustSet{})
		if st.AutoApproveAsRoot != permission.RootAllNote || !strings.Contains(b.String(), "IS_SANDBOX") || !strings.Contains(showText(t, st), "IS_SANDBOX") {
			t.Errorf("default_vendor %q: the note must show in JSON and text: %+v", v, st)
		}
	}
	for _, want := range []string{"Claude Code", "Other vendors are unaffected"} {
		if !strings.Contains(permission.RootAllNote, want) {
			t.Errorf("the note must say %q", want)
		}
	}
}
