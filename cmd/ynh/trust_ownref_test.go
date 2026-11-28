package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

const installedSHA = "0123456789abcdef0123456789abcdef01234567"

func installRemote(t *testing.T, sourceType, ref, sha string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "hooked", hookedHarness)
	if err := plugin.SaveInstalledJSON(harness.InstalledDirByID(id), &plugin.InstalledJSON{
		SourceType: sourceType, Source: "github.com/acme/hooked", Ref: ref, SHA: sha, InstalledAt: "2026-11-27T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// ynh trust accept of an installed remote harness records the commit that
// was installed, whatever its ref, so a floating install can be accepted and
// a later move reads as changed (ynh #1023). An install that recorded no
// full commit, and so has nothing to record, is refused with the reinstall.
func TestTrustAccept_RecordsTheInstalledCommit(t *testing.T) {
	cases := []struct {
		name, sourceType, ref, sha string
		wantSHA                    string
		wantErr                    string
	}{
		{"branch", "git", "main", installedSHA, installedSHA, ""},
		{"tag", "registry", "v1.2.0", installedSHA, installedSHA, ""},
		{"no ref", "git", "", installedSHA, installedSHA, ""},
		{"full ref", "git", installedSHA, installedSHA, installedSHA, ""},
		{"local", "local", "", "", "", ""},
		{"no commit recorded", "git", "main", "", "", "no full commit SHA"},
		{"short commit recorded", "git", "main", "0123456", "", "no full commit SHA"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := installRemote(t, tc.sourceType, tc.ref, tc.sha)
			var out, errb bytes.Buffer
			err := cmdTrustAccept([]string{id}, &out, &errb)
			store, serr := loadTrustStore()
			if serr != nil {
				t.Fatal(serr)
			}
			rec, recorded := store.Harnesses[id]
			if tc.wantErr == "" {
				if err != nil || !recorded {
					t.Fatalf("err = %v (%s), recorded = %v; want accepted", err, errb.String(), recorded)
				}
				if rec.SHA != tc.wantSHA {
					t.Errorf("recorded SHA %q, want %q", rec.SHA, tc.wantSHA)
				}
				if tc.wantSHA != "" && notPinned(tc.ref) != "" && !strings.Contains(out.String(), "accepted at commit "+tc.wantSHA) {
					t.Errorf("a floating ref's acceptance does not say which commit:\n%s", out.String())
				}
				return
			}
			msg := errb.String()
			if err != nil {
				msg += err.Error()
			}
			for _, want := range []string{tc.wantErr, "ynh install github.com/acme/hooked --ref <40-character SHA>", "run 'ynh trust accept " + id + "' again"} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal lacks %q:\n%s", want, msg)
				}
			}
			if strings.Contains(msg, "accept it by hand") {
				t.Errorf("refusal sends the operator back to the command that refused:\n%s", msg)
			}
			if recorded {
				t.Error("a refused accept recorded the harness")
			}
		})
	}
}

// Every route that refuses a floating own ref names 'ynh trust accept' as the
// way out, and that now works: update --trust is refused for a branch install,
// the gate refuses it unreviewed, and trust accept then admits it at the
// installed commit.
func TestFloatingInstall_FixLinesLeadSomewhereThatWorks(t *testing.T) {
	id := installRemote(t, "git", "main", installedSHA)

	err := cmdUpdate([]string{id, "--trust"})
	if err == nil || !strings.Contains(err.Error(), "Pin it to a 40-character commit SHA and try again") {
		t.Fatalf("update --trust: %v, want a refusal naming trust accept", err)
	}

	p, lerr := harness.LoadQualified(id)
	if lerr != nil {
		t.Fatal(lerr)
	}
	gerr := trustGate(id, p, io.Discard)
	if !isUntrusted(gerr) || !strings.Contains(gerr.Error(), "ynh trust accept "+id) {
		t.Fatalf("gate: %v, want a refusal naming 'ynh trust accept %s'", gerr, id)
	}

	if err := cmdTrustAccept([]string{id}, io.Discard, io.Discard); err != nil {
		t.Fatalf("the accept the refusals name refused: %v", err)
	}
	if gerr := trustGate(id, p, io.Discard); gerr != nil {
		t.Fatalf("after accept the gate still refuses: %v", gerr)
	}
}

// trust accept takes both floating things at once: the harness's own ref at
// the commit installed (ynh #1023) and a floating remote include at the commit
// it is at now (ynh #1002, #1016). The unrecorded-commit check comes first, so
// an install with no commit is refused whatever its includes.
func TestTrustAccept_FloatingOwnRefAndFloatingInclude(t *testing.T) {
	for _, tc := range []struct {
		name, sha string
		refused   bool
	}{
		{"commit recorded", installedSHA, false},
		{"no commit recorded", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := installRemote(t, "git", "main", tc.sha)
			remote := filepath.Join(t.TempDir(), "remote")
			gitRepo(t, remote, "remote-sensor")
			manifest := `{"name":"hooked","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"echo done"}]},` +
				`"includes":[{"git":"file://` + remote + `","ref":"main"}]}`
			if err := writePluginJSONFile(harness.InstalledDirByID(id), []byte(manifest)); err != nil {
				t.Fatal(err)
			}
			if _, err := resolver.EnsureRepo("file://"+remote, "main"); err != nil {
				t.Fatal(err)
			}
			var out, errb bytes.Buffer
			err := cmdTrustAccept([]string{id}, &out, &errb)
			store, _ := loadTrustStore()
			rec, recorded := store.Harnesses[id]
			if tc.refused {
				if err == nil && errb.Len() == 0 {
					t.Fatal("an install with no recorded commit was accepted")
				}
				if recorded {
					t.Error("a refused accept recorded the harness")
				}
				return
			}
			if err != nil || !recorded {
				t.Fatalf("err = %v (%s), recorded = %v", err, errb.String(), recorded)
			}
			if rec.SHA != installedSHA || len(rec.Floating) != 1 || !isFullSHA(rec.Floating[0].Commit) {
				t.Errorf("record = %+v, want the installed commit and one floating include", rec)
			}
			for _, want := range []string{"It is accepted at commit " + installedSHA, "Floating, accepted at what is installed now"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}
