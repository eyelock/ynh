package main

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
)

// ynh image --from <source> --trust (ynh #986): the build bakes a record with
// via: image, under the same full-SHA rule as `ynh install --trust`; without
// --trust a harness with commands is refused at build time.

func TestImageFromTrust_RemoteFullSHA(t *testing.T) {
	fakeHome(t)
	src, sha := remoteSource(t, "")
	var out bytes.Buffer
	if err := cmdImageTo([]string{"hh", "--from", "file://" + src, "--ref", sha, "--trust", "--dry-run"}, &out, io.Discard); err != nil {
		t.Fatalf("image --from --trust: %v", err)
	}
	if !strings.Contains(out.String(), "trust-digest") || !strings.Contains(out.String(), `dev.ynh.harness.sha="`+sha+`"`) {
		t.Errorf("Dockerfile lacks the digest label or the pinned sha:\n%s", out.String())
	}
}

// Accepting through the image route prints the same verb as every other route,
// on stderr so a --dry-run Dockerfile on stdout stays clean.
func TestImageFromTrust_PrintsAccepted(t *testing.T) {
	fakeHome(t)
	src, sha := remoteSource(t, "")
	var out, errb bytes.Buffer
	if err := cmdImageTo([]string{"hh", "--from", "file://" + src, "--ref", sha, "--trust", "--dry-run"}, &out, &errb); err != nil {
		t.Fatalf("image --from --trust: %v", err)
	}
	if !strings.HasPrefix(errb.String(), "Accepted local/hh (image): ") || !strings.Contains(errb.String(), "command(s), sha256:") {
		t.Errorf("stderr lacks the Accepted line:\n%s", errb.String())
	}
	if strings.Contains(out.String(), "Accepted ") {
		t.Errorf("stdout (the Dockerfile) carries the Accepted line:\n%s", out.String())
	}
}

func TestImageFromTrust_RefusesFloatingSource(t *testing.T) {
	fakeHome(t)
	src, _ := remoteSource(t, "")
	gitIn(t, src, "tag", "v1")
	for name, args := range map[string][]string{
		"tag":       {"hh", "--from", "file://" + src, "--ref", "v1", "--trust", "--dry-run"},
		"branch":    {"hh", "--from", "file://" + src, "--ref", "main", "--trust", "--dry-run"},
		"short sha": {"hh", "--from", "file://" + src, "--ref", gitRevParse(t, src)[:8], "--trust", "--dry-run"},
		"no ref":    {"hh", "--from", "file://" + src, "--trust", "--dry-run"},
	} {
		t.Run(name, func(t *testing.T) {
			err := cmdImageTo(args, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "--trust refuses to accept this image") || !strings.Contains(err.Error(), "the source's own ref") {
				t.Fatalf("err = %v, want a refusal naming the source's ref", err)
			}
		})
	}
}

func TestImageFromTrust_RefusesWithoutTrust(t *testing.T) {
	fakeHome(t)
	src, sha := remoteSource(t, "")
	for name, args := range map[string][]string{
		"remote": {"hh", "--from", "file://" + src, "--ref", sha, "--dry-run"},
		"local":  {"hh", "--from", src, "--dry-run"},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			err := cmdImageTo(args, &out, io.Discard)
			if !isUntrusted(err) {
				t.Fatalf("err = %v, want an untrusted refusal", err)
			}
			for _, want := range []string{"refusing to build an image of hh", "hook on_stop: echo done", "Fix:", "--trust"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal lacks %q:\n%s", want, err)
				}
			}
			if out.Len() != 0 {
				t.Errorf("a refused build printed a Dockerfile:\n%s", out.String())
			}
		})
	}
}

func TestImageFromTrust_LocalFolder(t *testing.T) {
	fakeHome(t)
	src, _ := remoteSource(t, "")
	var out bytes.Buffer
	if err := cmdImageTo([]string{"hh", "--from", src, "--trust", "--dry-run"}, &out, io.Discard); err != nil {
		t.Fatalf("local folder --trust: %v", err)
	}
	if !strings.Contains(out.String(), "trust-digest") {
		t.Errorf("Dockerfile lacks the digest label:\n%s", out.String())
	}
}

// A remote include that brings commands must be pinned too, naming what floats.
func TestImageFromTrust_RefusesFloatingInclude(t *testing.T) {
	fakeHome(t)
	floating := installFolder(t, "")
	err := cmdImageTo([]string{"fld", "--from", floating, "--trust", "--dry-run"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "floats") || !strings.Contains(err.Error(), "--trust refuses") {
		t.Fatalf("err = %v, want a refusal naming the floating include", err)
	}
	pinned := installFolder(t, "sha")
	if err := cmdImageTo([]string{"fld", "--from", pinned, "--trust", "--dry-run"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("pinned include: %v", err)
	}
}

// A cold include cache must not stop --trust: the includes are fetched before
// the gate reads them (ynw-twt3a). A floating include is judged once fetched.
func TestImageFromTrust_ColdIncludeCache(t *testing.T) {
	fakeHome(t)
	pinned := coldFolder(t, "sha")
	var out bytes.Buffer
	if err := cmdImageTo([]string{"fld", "--from", pinned, "--trust", "--dry-run"}, &out, io.Discard); err != nil {
		t.Fatalf("cold pinned include: %v", err)
	}
	if !strings.Contains(out.String(), "trust-digest") {
		t.Errorf("Dockerfile lacks the digest label:\n%s", out.String())
	}
	for _, ref := range []string{"", "branch"} {
		floating := coldFolder(t, ref)
		err := cmdImageTo([]string{"fld", "--from", floating, "--trust", "--dry-run"}, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "--trust refuses") || strings.Contains(err.Error(), "not in the cache") {
			t.Fatalf("cold floating include (%q): err = %v, want the unpinned refusal", ref, err)
		}
	}
}

// Without --trust an uncached include cannot be read, and the refusal must not
// point at 'ynh update local/<name>' (not installed): its Fix names --trust.
func TestImageFromTrust_ColdIncludeWithoutTrustNamesTrust(t *testing.T) {
	fakeHome(t)
	cold := coldFolder(t, "sha")
	err := cmdImageTo([]string{"fld", "--from", cold, "--dry-run"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("want a refusal")
	}
	msg := err.Error()
	if strings.Contains(msg, "ynh update") || !strings.Contains(msg, "Fix:") || !strings.Contains(msg, "--trust") || !strings.Contains(msg, "40-character") {
		t.Errorf("refusal lacks a --from Fix line naming --trust at a full SHA:\n%s", msg)
	}
}

// A harness with nothing to accept builds as before, with or without --trust.
func TestImageFromTrust_CommandlessBuildsAsBefore(t *testing.T) {
	fakeHome(t)
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(plainHarness)); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"plain", "--from", dir, "--dry-run"}, {"plain", "--from", dir, "--trust", "--dry-run"}} {
		var out bytes.Buffer
		if err := cmdImageTo(args, &out, io.Discard); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(out.String(), "trust-digest") {
			t.Errorf("%v: a commandless image carries a digest label", args)
		}
	}
}

func TestImageFromTrust_Flags(t *testing.T) {
	if _, err := parseImageArgs([]string{"x", "--trust"}); err == nil {
		t.Error("--trust without --from should fail")
	}
	if _, err := parseImageArgs([]string{"x", "--ref", "v1"}); err == nil {
		t.Error("--ref without --from should fail")
	}
	if err := cmdImageTo([]string{"x", "--from", t.TempDir(), "--ref", "v1"}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "--ref") {
		t.Errorf("--ref on a local source: %v", err)
	}
}

// The record an `image --from <git> --trust` build bakes must read accepted
// inside the image. The image's harness/ folder is a clone with no
// installed.json, so the record carries no source or commit to compare
// against it (ynw-8py06).
func TestImageFromTrust_BakedRecordReadsAcceptedInTheImage(t *testing.T) {
	fakeHome(t)
	src, sha := remoteSource(t, "")
	p, err := harness.LoadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	ia := imageArgs{name: "hh", from: "file://" + src, ref: sha, trust: true, sha: sha}
	rec, err := imageFromTrustRecord(p, cfg, ia, false, sha)
	if err != nil || rec == nil {
		t.Fatalf("rec = %v, err = %v", rec, err)
	}

	// The image's home: the baked record, and the clone with no installed.json.
	image := t.TempDir()
	if err := writeImageTrust(image, "hh", *rec); err != nil {
		t.Fatal(err)
	}
	if err := assembler.CopyTree(src, filepath.Join(image, "harnesses", "local--hh")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YNH_HOME", image)
	inImage := loadInstalled(t, "local/hh")
	if inImage.InstalledFrom != nil {
		t.Fatalf("the image's clone has provenance %+v; the test assumes none", inImage.InstalledFrom)
	}
	if err := gateErr("local/hh", inImage, io.Discard); err != nil {
		t.Fatalf("the gate refused the image's own harness: %v", err)
	}
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	if st := trustStatusFor("local/hh", inImage, store, nil); st.State != trustAccepted || st.Moved != "" {
		t.Errorf("state = %q, moved = %q, want accepted", st.State, st.Moved)
	}
}
