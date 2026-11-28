package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// manifestBytes is the harness's manifest as it is on disk now.
func manifestBytes(t *testing.T, id string) []byte {
	t.Helper()
	b, err := os.ReadFile(plugin.PluginPath(harness.InstalledDirByID(id)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// An acceptedHarness is installed, with a local include that brings a hook,
// and accepted by hand.
func acceptedHarness(t *testing.T, name string) (id string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id = installTrustedTestHarness(t, home, name, `{"name":"`+name+`","version":"0.1.0","default_vendor":"claude"}`)
	writeIncludedHarness(t, harness.InstalledDirByID(id), "extra", hookFields("echo extra"))
	return id
}

// include add --trust of a remote include that is not pinned is refused and
// the harness and its record are left exactly as they were (ynh #1041).
func TestIncludeAddTrust_RefusalChangesNothing(t *testing.T) {
	id := acceptedHarness(t, "iar")
	remote := filepath.Join(t.TempDir(), "remote")
	gitRepo(t, remote, "remote-sensor")
	beforeManifest := manifestBytes(t, id)
	beforeStore, _ := loadTrustStore()

	var out bytes.Buffer
	err := cmdIncludeTo([]string{"add", id, "file://" + remote, "--trust"}, &out)
	if err == nil || !strings.Contains(err.Error(), "full commit SHA") || !strings.Contains(err.Error(), "was not made") {
		t.Fatalf("err = %v", err)
	}
	// Both ways out work on a harness left unchanged: pin and run it again, or
	// make the change without --trust and then run ynh trust accept, which
	// accepts a floating include at the commit installed (ynh #1016).
	for _, want := range []string{"Pin each of them", "run it again with --trust", "make the change without --trust"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "accept it by hand") {
		t.Errorf("the refusal still offers accepting by hand:\n%v", err)
	}
	if !bytes.Equal(manifestBytes(t, id), beforeManifest) {
		t.Error("a refused include add --trust changed the manifest")
	}
	afterStore, _ := loadTrustStore()
	if afterStore.Harnesses[id] != beforeStore.Harnesses[id] {
		t.Errorf("the trust record changed: %+v -> %+v", beforeStore.Harnesses[id], afterStore.Harnesses[id])
	}
	if got := trustStateOf(id); got != trustAccepted {
		t.Errorf("state = %q, want accepted as before", got)
	}
}

// include update --trust that moves a pinned remote include onto a branch is
// refused before it is left written.
func TestIncludeUpdateTrust_RefusalChangesNothing(t *testing.T) {
	id := acceptedHarness(t, "iur")
	remote := filepath.Join(t.TempDir(), "remote")
	gitRepo(t, remote, "remote-sensor")
	sha := gitRevParse(t, remote)
	var out bytes.Buffer
	if err := cmdIncludeTo([]string{"add", id, "file://" + remote, "--ref", sha, "--trust"}, &out); err != nil {
		t.Fatalf("add at a full SHA: %v\n%s", err, out.String())
	}
	if got := trustStateOf(id); got != trustAccepted {
		t.Fatalf("state = %q", got)
	}
	beforeManifest := manifestBytes(t, id)

	err := cmdIncludeTo([]string{"update", id, "file://" + remote, "--ref", "main", "--trust"}, &out)
	if err == nil || !strings.Contains(err.Error(), "full commit SHA") {
		t.Fatalf("err = %v", err)
	}
	if !bytes.Equal(manifestBytes(t, id), beforeManifest) {
		t.Error("a refused include update --trust changed the manifest")
	}
	if got := trustStateOf(id); got != trustAccepted {
		t.Errorf("state = %q, want accepted as before", got)
	}
}

// include remove has one behaviour: it changes what the harness executes, so
// an accepted harness is 'changed' afterwards. Without --trust it says so and
// names the way back; with --trust it records the new digest as accepted.
func TestIncludeRemove_OneBehaviour(t *testing.T) {
	t.Run("without --trust the harness is changed and says so", func(t *testing.T) {
		id := acceptedHarness(t, "irm")
		dir := harness.InstalledDirByID(id)
		var out bytes.Buffer
		if err := cmdIncludeTo([]string{"add", id, filepath.Join(dir, "extra"), "--hooks", "--trust"}, &out); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if err := cmdIncludeTo([]string{"remove", id, filepath.Join(dir, "extra")}, &out); err != nil {
			t.Fatalf("remove: %v", err)
		}
		if got := trustStateOf(id); got != trustChanged {
			t.Errorf("state = %q, want changed", got)
		}
		for _, want := range []string{"no longer trusted", "ynh trust accept " + id} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("output lacks %q:\n%s", want, out.String())
			}
		}
	})
	t.Run("with --trust the new digest is accepted", func(t *testing.T) {
		id := acceptedHarness(t, "irt")
		dir := harness.InstalledDirByID(id)
		var out bytes.Buffer
		if err := cmdIncludeTo([]string{"add", id, filepath.Join(dir, "extra"), "--hooks", "--trust"}, &out); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if err := cmdIncludeTo([]string{"remove", id, filepath.Join(dir, "extra"), "--trust"}, &out); err != nil {
			t.Fatalf("remove --trust: %v", err)
		}
		if got := trustStateOf(id); got == trustChanged || got == trustUnreviewed {
			t.Errorf("state = %q, want accepted or no-commands", got)
		}
		if !strings.Contains(out.String(), "(include --trust)") || strings.Contains(out.String(), "no longer trusted") {
			t.Errorf("output:\n%s", out.String())
		}
	})
	t.Run("--trust still refuses what stays unpinned, and changes nothing", func(t *testing.T) {
		id := acceptedHarness(t, "irf")
		dir := harness.InstalledDirByID(id)
		remote := filepath.Join(t.TempDir(), "remote")
		gitRepo(t, remote, "remote-sensor")
		// A floating remote include written by hand, as an older ynh left it.
		if err := cmdIncludeTo([]string{"add", id, "file://" + remote}, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if err := cmdIncludeTo([]string{"add", id, filepath.Join(dir, "extra"), "--hooks"}, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		before := manifestBytes(t, id)
		err := cmdIncludeTo([]string{"remove", id, filepath.Join(dir, "extra"), "--trust"}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "full commit SHA") {
			t.Fatalf("err = %v", err)
		}
		if !bytes.Equal(manifestBytes(t, id), before) {
			t.Error("a refused include remove --trust changed the manifest")
		}
	})
}

// A refusal that comes from the include check, not the pin rule, also writes
// nothing and prints nothing as done: A is pinned, but A's own include of B at
// 'main' is not in the cache, so what A brings cannot be read in full.
func TestIncludeAddTrust_NestedUncachedRefusesBeforeWriting(t *testing.T) {
	id := acceptedHarness(t, "ian")
	b := filepath.Join(t.TempDir(), "b")
	gitRepo(t, b, "b-sensor")
	a := filepath.Join(t.TempDir(), "a")
	body := `{"name":"outer","version":"1.0.0","includes":[{"git":"file://` + b + `","ref":"main"}]}`
	if err := writePluginJSONFile(a, []byte(body)); err != nil {
		t.Fatal(err)
	}
	gitIn(t, a, "init", "-q", "-b", "main")
	gitIn(t, a, "add", "-A")
	gitIn(t, a, "commit", "-qm", "init")
	sha := gitRevParse(t, a)
	beforeManifest := manifestBytes(t, id)
	beforeStore, _ := loadTrustStore()

	var out bytes.Buffer
	err := cmdIncludeTo([]string{"add", id, "file://" + a, "--ref", sha, "--trust"}, &out)
	if err == nil || !strings.Contains(err.Error(), "could not be read in full") {
		t.Fatalf("err = %v", err)
	}
	// The harness was left unchanged, so 'ynh update' has nothing to fetch:
	// the Fix line names the route that works (ynh #1041).
	for _, want := range []string{"was not made", "Make the change without --trust", "ynh update " + id, "ynh trust accept " + id} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, err)
		}
	}
	if strings.Contains(err.Error(), "Fetch them with 'ynh update', then run it again") {
		t.Errorf("the refusal still sends an unchanged harness to ynh update first:\n%v", err)
	}
	if !bytes.Equal(manifestBytes(t, id), beforeManifest) {
		t.Error("a refused include add --trust left the include written")
	}
	if out.Len() != 0 {
		t.Errorf("a refused call printed output:\n%s", out.String())
	}
	if afterStore, _ := loadTrustStore(); afterStore.Harnesses[id] != beforeStore.Harnesses[id] {
		t.Error("the trust record changed")
	}
	if got := trustStateOf(id); got != trustAccepted {
		t.Errorf("state = %q, want accepted as before", got)
	}
}

// A refused call, whichever of add, update or remove, prints no success line.
func TestIncludeTrust_RefusalPrintsNothingAsDone(t *testing.T) {
	id := acceptedHarness(t, "ipn")
	remote := filepath.Join(t.TempDir(), "remote")
	gitRepo(t, remote, "remote-sensor")
	var out bytes.Buffer
	if err := cmdIncludeTo([]string{"add", id, "file://" + remote, "--trust"}, &out); err == nil {
		t.Fatal("an unpinned include add --trust was accepted")
	}
	for _, bad := range []string{"Added include", "Accepted"} {
		if strings.Contains(out.String(), bad) {
			t.Errorf("a refused call printed %q:\n%s", bad, out.String())
		}
	}
}
