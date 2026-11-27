package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

// One full-SHA pin rule for every acceptance route (ynh #1002): trust accept,
// install --trust, run --trust and native delegate entries, checked before
// cloning where it can be.

// ynh trust accept <id> refuses a remote include that floats, recording
// nothing, and accepts one pinned to a full SHA.
func TestTrustAccept_RefusesFloatingInclude(t *testing.T) {
	fakeHome(t)
	floating := installFolder(t, "")
	if out, err := installOut(t, floating); err != nil {
		t.Fatalf("plain install: %v\n%s", err, out)
	}
	err := cmdTrustTo([]string{"accept", "local/fld"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "floats") || !strings.Contains(err.Error(), "40-character commit SHA") {
		t.Fatalf("floating include: err = %v, want the pin refusal", err)
	}
	if store, _ := loadTrustStore(); len(store.Harnesses) != 0 {
		t.Errorf("a refused accept recorded %v", store.Harnesses)
	}

	branch := installFolder(t, "branch")
	if out, err := installOut(t, branch); err != nil {
		t.Fatalf("plain install: %v\n%s", err, out)
	}
	if err := cmdTrustTo([]string{"accept", "local/fld"}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "tag or branch") {
		t.Fatalf("branch include: err = %v, want the pin refusal", err)
	}

	pinned := installFolder(t, "sha")
	if out, err := installOut(t, pinned); err != nil {
		t.Fatalf("plain install: %v\n%s", err, out)
	}
	var out bytes.Buffer
	if err := cmdTrustTo([]string{"accept", "local/fld"}, &out, io.Discard); err != nil {
		t.Fatalf("pinned include: %v", err)
	}
}

// ynh trust accept <dir> takes the same rule.
func TestTrustAcceptFolder_RefusesFloatingInclude(t *testing.T) {
	fakeHome(t)
	floating := installFolder(t, "")
	err := cmdTrustTo([]string{"accept", floating}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "floats") {
		t.Fatalf("floating include: err = %v, want the pin refusal", err)
	}
	if store, _ := loadTrustStore(); len(store.Harnesses) != 0 {
		t.Errorf("a refused accept recorded %v", store.Harnesses)
	}
	pinned := installFolder(t, "sha")
	if err := cmdTrustTo([]string{"accept", pinned}, io.Discard, io.Discard); err != nil {
		t.Fatalf("pinned include: %v", err)
	}
}

// A native delegate entry from a remote source is judged like an include; an
// A2A entry, a local one and the parent's own repository are not.
func TestUnpinnedNativeDelegates(t *testing.T) {
	sha := strings.Repeat("ab", 20)
	p := &harness.Harness{
		Transport: harness.TransportA2A,
		DelegatesTo: []harness.Delegate{
			{GitSource: harness.GitSource{Git: "github.com/x/branch", Ref: "main"}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Git: "github.com/x/none"}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Git: "github.com/x/pinned", Ref: sha}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Git: "github.com/x/a2a", Ref: "main"}},
			{GitSource: harness.GitSource{Git: ".", Ref: "main"}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Git: "/abs/folder"}, Transport: harness.TransportNative},
			{GitSource: harness.GitSource{Local: "../x"}, Transport: harness.TransportNative},
		},
	}
	got := unpinnedNativeDelegates(p)
	if len(got) != 2 || !strings.Contains(got[0], "github.com/x/branch") || !strings.Contains(got[1], "github.com/x/none") {
		t.Fatalf("unpinned = %q, want the branch and no-ref native entries", got)
	}
	p.Transport = harness.TransportNative
	if got := unpinnedNativeDelegates(p); len(got) != 3 {
		t.Errorf("with a native default, the A2A-looking entry counts too: %q", got)
	}
}

// Every route refuses a floating native delegate entry: trust accept of a
// folder, and run --trust.
func TestNativeDelegateEntry_RefusedOnEveryRoute(t *testing.T) {
	fakeHome(t)
	dir := t.TempDir()
	body := `{"name":"nd","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"echo own"}]},` +
		`"delegates_to":[{"git":"github.com/example/reviewer","ref":"main","transport":"native"}]}`
	if err := writePluginJSONFile(dir, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := cmdTrustTo([]string{"accept", dir}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "native delegate github.com/example/reviewer") {
		t.Fatalf("trust accept <dir>: err = %v, want the native delegate named", err)
	}
	p, err := harness.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptFolderRun(dir, p, io.Discard); err == nil || !strings.Contains(err.Error(), "native delegate github.com/example/reviewer") {
		t.Fatalf("run --trust: err = %v, want the native delegate named", err)
	}
	if store, _ := loadTrustStore(); len(store.Harnesses) != 0 {
		t.Errorf("a refused route recorded %v", store.Harnesses)
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
