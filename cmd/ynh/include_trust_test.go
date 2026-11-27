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
