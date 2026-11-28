package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
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

// A harness with nothing to accept is refused without --trust, since the
// image runs one-shot and needs an acceptance baked in (ynh #1058, N2), and
// builds with it.
func TestImageFromTrust_CommandlessNeedsTrust(t *testing.T) {
	fakeHome(t)
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(plainHarness)); err != nil {
		t.Fatal(err)
	}
	err := cmdImageTo([]string{"plain", "--from", dir, "--dry-run"}, io.Discard, io.Discard)
	if err == nil || !isUntrusted(err) || !strings.Contains(err.Error(), "--trust") {
		t.Errorf("an unaccepted commandless image must be refused, naming --trust: %v", err)
	}
	if err := cmdImageTo([]string{"plain", "--from", dir, "--trust", "--dry-run"}, io.Discard, io.Discard); err != nil {
		t.Errorf("--trust builds: %v", err)
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
	if err := trustGate("local/hh", inImage, io.Discard); err != nil {
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

// The image keeps the --from source and commit (ynh #1040): inside it, the
// harness reads as installed from that URL at that SHA, not (local), the baked
// record carries both, and the record still reads accepted against them.
func TestImageFromTrust_ImageKeepsSourceAndSHA(t *testing.T) {
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
	url := "file://" + src
	ia := imageArgs{name: "hh", from: url, ref: sha, trust: true, sha: sha}
	rec, err := imageFromTrustRecord(p, cfg, ia, false, sha)
	if err != nil || rec == nil {
		t.Fatalf("rec = %v, err = %v", rec, err)
	}

	image := t.TempDir()
	dst := filepath.Join(image, "harnesses", "local--hh")
	if err := assembler.CopyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	prov := &plugin.InstalledJSON{SourceType: "git", Source: url, Ref: sha, SHA: sha}
	if err := bakeFromProvenance(dst, prov, rec); err != nil {
		t.Fatal(err)
	}
	if rec.Source != url || rec.SHA != sha {
		t.Errorf("record source = %q, sha = %q, want %q, %q", rec.Source, rec.SHA, url, sha)
	}
	if err := writeImageTrust(image, "hh", *rec); err != nil {
		t.Fatal(err)
	}

	t.Setenv("YNH_HOME", image)
	inImage := loadInstalled(t, "local/hh")
	if inImage.InstalledFrom == nil || inImage.InstalledFrom.Source != url || inImage.InstalledFrom.SHA != sha {
		t.Fatalf("inside the image the harness reads as %+v, want source %q at %q", inImage.InstalledFrom, url, sha)
	}
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	st := trustStatusFor("local/hh", inImage, store, nil)
	if st.State != trustAccepted || st.Moved != "" {
		t.Errorf("state = %q, moved = %q, want accepted", st.State, st.Moved)
	}
	if st.Source != url || st.SHA != sha {
		t.Errorf("trust show reports source %q at %q, want %q at %q", st.Source, st.SHA, url, sha)
	}
	if err := trustGate("local/hh", inImage, io.Discard); err != nil {
		t.Errorf("the gate refused the image's own harness: %v", err)
	}
}

// A local --from folder has no source worth keeping, so nothing is baked.
func TestBakeFromProvenance_LocalBakesNothing(t *testing.T) {
	dst := t.TempDir()
	rec := &trustRecord{}
	if err := bakeFromProvenance(dst, nil, rec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plugin.InstalledPath(dst)); !os.IsNotExist(err) {
		t.Errorf("installed.json written for a local source: %v", err)
	}
	if rec.Source != "" || rec.SHA != "" {
		t.Errorf("record = %+v, want no source or sha", rec)
	}
}

// bakedContext runs cmdImageTo with args and returns what it baked into the
// build context: the harness's installed.json (nil when none was written) and
// the image's trust record.
func bakedContext(t *testing.T, args ...string) (installed *plugin.InstalledJSON, rec trustRecord) {
	t.Helper()
	imageContextHook = func(dir string) {
		if _, err := os.Stat(plugin.InstalledPath(filepath.Join(dir, "harness"))); err == nil {
			ins, err := plugin.LoadInstalledJSON(filepath.Join(dir, "harness"))
			if err != nil {
				t.Errorf("reading the baked installed.json: %v", err)
			}
			installed = ins
		}
		data, err := os.ReadFile(filepath.Join(dir, "home", trustFile))
		if err != nil {
			t.Errorf("reading the baked trust record: %v", err)
			return
		}
		var store trustStore
		if err := json.Unmarshal(data, &store); err != nil {
			t.Errorf("parsing the baked trust record: %v", err)
		}
		rec = store.Harnesses["local/hh"]
	}
	t.Cleanup(func() { imageContextHook = nil })
	if err := cmdImageTo(args, io.Discard, io.Discard); err != nil {
		t.Fatalf("image %v: %v", args, err)
	}
	return installed, rec
}

// The provenance cmdImageTo fills in for a remote --from at a full SHA is what
// the image carries: installed.json and the trust record agree on the source,
// the ref and the commit the clone resolved to, and the record is the one the
// gate would accept inside the image (ynh #1040).
func TestCmdImageTo_BakesProvenanceForRemoteFrom(t *testing.T) {
	fakeHome(t)
	src, sha := remoteSource(t, "")
	url := "file://" + src
	installed, rec := bakedContext(t, "hh", "--from", url, "--ref", sha, "--trust", "--dry-run")
	if installed == nil {
		t.Fatal("no installed.json baked for a remote --from")
	}
	if installed.Source != url || installed.Ref != sha || installed.SHA != sha || installed.Path != "" {
		t.Errorf("installed.json = %+v, want source %q at ref and sha %q, no path", installed, url, sha)
	}
	if installed.SourceType == "" || installed.InstalledAt == "" {
		t.Errorf("installed.json lacks its source type or time: %+v", installed)
	}
	if rec.Source != url || rec.Ref != sha || rec.SHA != sha || rec.Via != viaImage {
		t.Errorf("trust record = %+v, want source %q at %q via image", rec, url, sha)
	}
}

// The same for a harness in a subfolder of the source (--path), whose
// provenance records the path.
func TestCmdImageTo_BakesProvenanceForRemoteFromPath(t *testing.T) {
	fakeHome(t)
	src := t.TempDir()
	sub := filepath.Join(src, "harnesses", "hh")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePluginJSONFile(sub, []byte(`{"name":"hh","version":"0.1.0","default_vendor":"claude","hooks":{"on_stop":[{"command":"echo done"}]}}`)); err != nil {
		t.Fatal(err)
	}
	gitIn(t, src, "init", "-q")
	gitIn(t, src, "add", ".")
	gitIn(t, src, "commit", "-qm", "hh")
	sha := gitRevParse(t, src)
	url := "file://" + src
	installed, rec := bakedContext(t, "hh", "--from", url, "--path", "harnesses/hh", "--ref", sha, "--trust", "--dry-run")
	if installed == nil {
		t.Fatal("no installed.json baked for a remote --from --path")
	}
	if installed.Source != url || installed.SHA != sha || installed.Path != "harnesses/hh" {
		t.Errorf("installed.json = %+v, want source %q at %q, path harnesses/hh", installed, url, sha)
	}
	if rec.Source != url || rec.SHA != sha {
		t.Errorf("trust record = %+v, want source %q at %q", rec, url, sha)
	}
}

// A local --from folder still bakes nothing: no installed.json, and a record
// with no source or commit.
func TestCmdImageTo_LocalFromBakesNoProvenance(t *testing.T) {
	fakeHome(t)
	src, _ := remoteSource(t, "")
	installed, rec := bakedContext(t, "hh", "--from", src, "--trust", "--dry-run")
	if installed != nil {
		t.Errorf("installed.json baked for a local --from: %+v", installed)
	}
	if rec.Source != "" || rec.Ref != "" || rec.SHA != "" {
		t.Errorf("trust record carries provenance for a local --from: %+v", rec)
	}
}

// Inside an image, ls --format json names the harness by the id the image runs
// it under (local/<name>), whatever host-style source it was built from, while
// still reporting that source and commit (ynh #1040).
func TestImageFromSource_ListJSONIdIsTheImagesOwn(t *testing.T) {
	fakeHome(t)
	src, sha := remoteSource(t, "")
	image := t.TempDir()
	dst := filepath.Join(image, "harnesses", "local--hh")
	if err := assembler.CopyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	const source = "https://github.com/eyelock/demo"
	if err := bakeFromProvenance(dst, &plugin.InstalledJSON{SourceType: "git", Source: source, Ref: sha, SHA: sha}, nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YNH_HOME", image)

	var out bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var env listEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	entries := env.Harnesses
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	e := entries[0]
	if e.ID != "local/hh" || e.Namespace != "" {
		t.Errorf("id %q, namespace %q, want local/hh and none", e.ID, e.Namespace)
	}
	if e.InstalledFrom == nil || e.InstalledFrom.Source != source || e.InstalledFrom.SHA != sha {
		t.Errorf("installed_from = %+v, want the --from source and commit", e.InstalledFrom)
	}

	var text bytes.Buffer
	if err := cmdListTo(nil, &text, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "local/hh") {
		t.Errorf("text list does not name local/hh:\n%s", text.String())
	}

	// The id the JSON gives is one the image can load.
	var info bytes.Buffer
	if err := cmdInfoTo([]string{e.ID, "--format", "json"}, &info, io.Discard); err != nil {
		t.Fatalf("info %s: %v", e.ID, err)
	}
	if !strings.Contains(info.String(), `"id": "local/hh"`) && !strings.Contains(info.String(), `"id":"local/hh"`) {
		t.Errorf("info names another id:\n%s", info.String())
	}
}
