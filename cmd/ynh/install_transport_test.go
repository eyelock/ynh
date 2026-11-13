package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
)

var delegateURL = map[string]string{}

// transportSource makes a git repository holding the harness "hh" (vendor
// cursor, one delegate) with the given transport on the delegate.
func transportSource(t *testing.T, transport string) string {
	t.Helper()
	del := t.TempDir()
	if err := writePluginJSONFile(del, []byte(`{"name":"del","version":"0.1.0","default_vendor":"cursor"}`)); err != nil {
		t.Fatal(err)
	}
	gitIn(t, del, "init", "-q")
	gitIn(t, del, "add", ".")
	gitIn(t, del, "commit", "-qm", "del")
	src := t.TempDir()
	delegateURL[src] = "file://" + del
	setTransport(t, src, transport)
	gitIn(t, src, "init", "-q")
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-qm", "hh")
	return src
}

func setTransport(t *testing.T, src, transport string) {
	t.Helper()
	extra := ""
	if transport != "" {
		extra = `,"transport":"` + transport + `"`
	}
	body := `{"name":"hh","version":"0.1.0","default_vendor":"cursor","delegates_to":[{"git":"` + delegateURL[src] + `"` + extra + `}]}`
	if err := writePluginJSONFile(src, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

// A refused transport leaves nothing behind on a fresh install.
func TestInstall_NativeOnCursorRefusedFresh(t *testing.T) {
	fakeHome(t)
	src := transportSource(t, "native")

	err := cmdInstall([]string{"file://" + src})
	if err == nil || !strings.Contains(err.Error(), "Fix: set transport: a2a on this delegate, or run on a vendor with subagents") {
		t.Fatalf("want the refusal with its fix, got %v", err)
	}
	if _, serr := os.Stat(harness.InstalledDirByID("local/hh")); !os.IsNotExist(serr) {
		t.Errorf("a refused fresh install must leave no install folder (stat: %v)", serr)
	}
	if list, _ := harness.ListAll(); len(list) != 0 {
		t.Errorf("nothing should be installed, got %v", list)
	}
}

// A refused reinstall leaves the working install exactly as it was.
func TestInstall_RefusedReinstallLeavesWorkingInstallIntact(t *testing.T) {
	fakeHome(t)
	src := transportSource(t, "")
	if err := cmdInstall([]string{"file://" + src}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	dir := harness.InstalledDirByID("local/hh")
	manifest := filepath.Join(dir, ".agents", "harness", "plugin.json")
	installed := filepath.Join(dir, ".agents", "harness", "installed.json")
	beforeManifest, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	beforeInstalled, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}

	setTransport(t, src, "native")
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-qm", "native")
	err = cmdInstall([]string{"file://" + src})
	if err == nil || !strings.Contains(err.Error(), "Fix: set transport: a2a") {
		t.Fatalf("want the refusal with its fix, got %v", err)
	}

	afterManifest, rerr := os.ReadFile(manifest)
	if rerr != nil || string(afterManifest) != string(beforeManifest) {
		t.Errorf("the installed manifest changed (err %v):\n%s", rerr, afterManifest)
	}
	afterInstalled, rerr := os.ReadFile(installed)
	if rerr != nil || string(afterInstalled) != string(beforeInstalled) {
		t.Errorf("installed.json changed or went (err %v)", rerr)
	}
	if _, lerr := harness.LoadByID("local/hh"); lerr != nil {
		t.Errorf("the working install no longer loads: %v", lerr)
	}
}
