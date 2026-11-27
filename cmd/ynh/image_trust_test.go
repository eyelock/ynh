package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/assembler"
)

const imageHookedHarness = `{"name":"hooked","version":"0.1.0","default_vendor":"claude",` +
	`"hooks":{"on_stop":[{"command":"./hooks/done.sh"}]}}`

// installHookedWithScript installs a harness whose hook runs a script of its
// own, so the digest covers the script's contents.
func installHookedWithScript(t *testing.T, home string) (id, dir string) {
	t.Helper()
	id = installListTestHarness(t, home, "hooked", imageHookedHarness)
	dir = filepath.Join(home, "harnesses", "local--hooked")
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "done.sh"), []byte("#!/bin/sh\necho done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return id, dir
}

// ynh image refuses to build from an installed harness whose commands the
// operator has not accepted, a dry run included, naming the command that
// accepts it (design decision 9).
func TestCmdImage_RefusesUnacceptedHarness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id, _ := installHookedWithScript(t, home)

	for _, args := range [][]string{{id, "--dry-run"}, {id}} {
		var stdout bytes.Buffer
		err := cmdImageTo(args, &stdout, io.Discard)
		if !isUntrusted(err) {
			t.Fatalf("args %v: err = %v, want an untrusted refusal", args, err)
		}
		if !strings.Contains(err.Error(), "ynh trust accept local/hooked") {
			t.Errorf("refusal lacks the accept command:\n%s", err)
		}
		if stdout.Len() != 0 {
			t.Errorf("a refused image printed a Dockerfile:\n%s", stdout.String())
		}
	}
}

// A harness changed since it was accepted is refused too.
func TestCmdImage_RefusesChangedHarness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id, dir := installHookedWithScript(t, home)
	if err := cmdTrustTo([]string{"accept", id}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "done.sh"), []byte("#!/bin/sh\necho changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := cmdImageTo([]string{id, "--dry-run"}, io.Discard, io.Discard)
	if !isUntrusted(err) || !strings.Contains(err.Error(), "differ from what you accepted") {
		t.Fatalf("err = %v, want a changed refusal", err)
	}
}

// An accepted harness builds, and the Dockerfile says which digest the image
// carries.
func TestCmdImage_AcceptedHarnessLabelsTheDigest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id, _ := installHookedWithScript(t, home)
	if err := cmdTrustTo([]string{"accept", id}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	store, _ := loadTrustStore()

	var stdout bytes.Buffer
	if err := cmdImageTo([]string{id, "--dry-run"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := `dev.ynh.harness.trust-digest="` + store.Harnesses[id].Digest + `"`
	if !strings.Contains(stdout.String(), want) {
		t.Errorf("Dockerfile lacks %s:\n%s", want, stdout.String())
	}
	if !strings.Contains(stdout.String(), "trust.json") {
		t.Errorf("Dockerfile does not mention the baked trust record:\n%s", stdout.String())
	}
}

// A harness that executes nothing needs no record: the image's gate lets it
// through, and no digest label claims an acceptance.
func TestCmdImage_CommandlessHarnessBakesNoRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "plain", plainHarness)
	rec, err := imageTrustRecord(id, loadInstalled(t, id))
	if err != nil || rec != nil {
		t.Fatalf("rec = %v, err = %v, want neither", rec, err)
	}
	var stdout bytes.Buffer
	if err := cmdImageTo([]string{id, "--dry-run"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "trust-digest") {
		t.Errorf("a commandless image carries a digest label:\n%s", stdout.String())
	}
}

// The digest the image carries matches the one the gate computes where the
// harness runs, though the paths differ: the image keeps the harness at
// $YNH_HOME/harnesses/local--<name> under its own home. The image's layout is
// rebuilt in a second home and the gate run against it, as the entrypoint does.
// A changed script in the image refuses.
func TestImageTrust_DigestHoldsInTheImageLayout(t *testing.T) {
	host := t.TempDir()
	t.Setenv("YNH_HOME", host)
	id, dir := installHookedWithScript(t, host)
	if err := cmdTrustTo([]string{"accept", id}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	rec, err := imageTrustRecord(id, loadInstalled(t, id))
	if err != nil || rec == nil {
		t.Fatalf("rec = %v, err = %v", rec, err)
	}
	if rec.Via != viaImage || rec.Scheme != trustScheme {
		t.Errorf("record = %+v, want via %q and scheme %d", rec, viaImage, trustScheme)
	}

	// The image's home: the baked record and the harness source.
	image := filepath.Join(t.TempDir(), "deeper", "ynh-home")
	if err := os.MkdirAll(image, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeImageTrust(image, "hooked", *rec); err != nil {
		t.Fatal(err)
	}
	baked := filepath.Join(image, "harnesses", "local--hooked")
	if err := assembler.CopyTree(dir, baked); err != nil {
		t.Fatal(err)
	}

	t.Setenv("YNH_HOME", image)
	if err := gateErr("local/hooked", loadInstalled(t, "local/hooked"), io.Discard); err != nil {
		t.Fatalf("the gate refused the image's own layout: %v", err)
	}
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Harnesses["local/hooked"]; got.Via != viaImage || got.Digest != rec.Digest {
		t.Errorf("baked record = %+v", got)
	}

	if err := os.WriteFile(filepath.Join(baked, "hooks", "done.sh"), []byte("#!/bin/sh\nrm -rf /\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = gateErr("local/hooked", loadInstalled(t, "local/hooked"), io.Discard)
	if !isUntrusted(err) || !strings.Contains(err.Error(), "differ from what you accepted") {
		t.Fatalf("err = %v, want a refusal of the changed pre-assembled harness", err)
	}
}

// The record is keyed by the id the entrypoint runs, local/<name>, whatever
// id the harness was installed under on the host.
func TestImageTrust_KeyedByTheImageID(t *testing.T) {
	home := t.TempDir()
	if err := writeImageTrust(home, "thing", trustRecord{Digest: "sha256:x", Via: viaImage}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YNH_HOME", home)
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Harnesses["local/thing"]; !ok || len(store.Harnesses) != 1 {
		t.Errorf("records = %v", store.Harnesses)
	}
}
