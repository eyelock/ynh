package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/symlink"
	"github.com/eyelock/ynh/internal/vendor"
)

// fakeHome points ynh at an empty home and returns it. Every check that reads
// state reads it from here, so a test can break one thing at a time.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	return home
}

func findingsOf(c doctorCheck) string {
	var b strings.Builder
	for _, f := range c.Findings {
		b.WriteString(f.Severity + " " + f.Subject + " " + f.Message + " | " + f.Remedy + "\n")
	}
	return b.String()
}

func TestDoctor_LauncherNotOnPath(t *testing.T) {
	home := fakeHome(t)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")

	c := checkLauncher()
	if c.Status != sevWarn {
		t.Fatalf("expected warn, got %q:\n%s", c.Status, findingsOf(c))
	}
	if !strings.Contains(findingsOf(c), "not on PATH") {
		t.Errorf("finding should say what is wrong:\n%s", findingsOf(c))
	}
	// The remedy must be runnable, not advice.
	if !strings.Contains(findingsOf(c), "export PATH=") {
		t.Errorf("finding should carry the command that fixes it:\n%s", findingsOf(c))
	}
}

func TestDoctor_LauncherOnPathIsSilent(t *testing.T) {
	home := fakeHome(t)
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin")

	if c := checkLauncher(); c.Status != sevOK {
		t.Errorf("a bin dir on PATH must be silent, got %q:\n%s", c.Status, findingsOf(c))
	}
}

func TestDoctor_Quarantine(t *testing.T) {
	home := fakeHome(t)

	if c := checkQuarantine(); c.Status != sevOK {
		t.Fatalf("empty quarantine must be silent, got %q:\n%s", c.Status, findingsOf(c))
	}

	dir := filepath.Join(home, migration.QuarantineDir, "broken", "busted")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	c := checkQuarantine()
	if c.Status != sevWarn {
		t.Fatalf("a quarantined harness must be reported, got %q:\n%s", c.Status, findingsOf(c))
	}
	if !strings.Contains(findingsOf(c), "busted") {
		t.Errorf("finding should name the harness:\n%s", findingsOf(c))
	}
	// Guard against the fixture being in the wrong place: an earlier manual
	// check "passed" only because it planted the directory somewhere the code
	// never looks.
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("fixture is vacuous: %v", err)
	}
}

func TestDoctor_SymlinksDangling(t *testing.T) {
	fakeHome(t)
	project := t.TempDir()

	log := &symlink.Log{Installations: []symlink.Installation{{
		Harness: "demo", Vendor: "claude", Project: project,
		Symlinks: []vendor.SymlinkEntry{{
			Target: filepath.Join(project, "target"),
			Link:   filepath.Join(project, "never-created"),
		}},
	}}}
	if err := log.Save(); err != nil {
		t.Fatal(err)
	}

	c := checkSymlinks()
	if c.Status != sevWarn {
		t.Fatalf("a missing link must be reported, got %q:\n%s", c.Status, findingsOf(c))
	}
	if !strings.Contains(findingsOf(c), "never-created") {
		t.Errorf("finding should name the missing link:\n%s", findingsOf(c))
	}
}

func TestDoctor_SymlinksProjectGone(t *testing.T) {
	fakeHome(t)
	gone := filepath.Join(t.TempDir(), "removed")

	log := &symlink.Log{Installations: []symlink.Installation{{
		Harness: "demo", Vendor: "claude", Project: gone,
	}}}
	if err := log.Save(); err != nil {
		t.Fatal(err)
	}
	c := checkSymlinks()
	if !strings.Contains(findingsOf(c), "project directory is gone") {
		t.Errorf("expected a gone-project finding:\n%s", findingsOf(c))
	}
	if !strings.Contains(findingsOf(c), "ynh prune") {
		t.Errorf("finding should name the command that cleans it:\n%s", findingsOf(c))
	}
}

func TestDoctor_NoHarnessesIsInfoNotOK(t *testing.T) {
	fakeHome(t)
	c := checkHarnesses()
	if c.Status != sevInfo {
		t.Fatalf("an empty install must be info, not a silent ok, got %q", c.Status)
	}
}

// A check that ran and found nothing must serialise findings as [], never
// null: a consumer iterating the array should not have to special-case it.
func TestDoctor_FindingsNeverNull(t *testing.T) {
	fakeHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")

	var buf bytes.Buffer
	if err := cmdDoctorTo([]string{"--format", "json"}, &buf, &buf); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(buf.Bytes(), []byte("null")) {
		t.Errorf("doctor JSON must not contain null:\n%s", buf.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc["checks"].([]any) {
		if _, ok := c.(map[string]any)["findings"].([]any); !ok {
			t.Errorf("findings must be an array: %v", c)
		}
	}
}

func TestDoctor_SummaryCountsMatchFindings(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, migration.QuarantineDir, "broken", "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	r := runDoctor()
	wantErr, wantWarn := 0, 0
	for _, c := range r.Checks {
		for _, f := range c.Findings {
			switch f.Severity {
			case sevError:
				wantErr++
			case sevWarn:
				wantWarn++
			}
		}
	}
	if r.Summary.Errors != wantErr || r.Summary.Warnings != wantWarn {
		t.Errorf("summary %+v disagrees with the findings it summarises (errors=%d warnings=%d)",
			r.Summary, wantErr, wantWarn)
	}
	if r.Summary.Warnings < 2 {
		t.Errorf("fixture is vacuous: expected at least the launcher and quarantine warnings, got %+v", r.Summary)
	}
}

func TestDoctor_RejectsBadFlags(t *testing.T) {
	fakeHome(t)
	for _, args := range [][]string{{"--nope"}, {"extra"}, {"--format"}, {"--format", "yaml"}} {
		var out, errBuf bytes.Buffer
		if err := cmdDoctorTo(args, &out, &errBuf); err == nil {
			t.Errorf("args %v should be rejected", args)
		}
	}
}

// worst() decides a check's status. If it under-reports, a broken setup renders
// as ok, which is the whole failure this command exists to prevent.
func TestDoctor_WorstSeverityWins(t *testing.T) {
	cases := []struct {
		sev  []string
		want string
	}{
		{nil, sevOK},
		{[]string{sevInfo}, sevInfo},
		{[]string{sevInfo, sevWarn}, sevWarn},
		{[]string{sevWarn, sevError}, sevError},
		{[]string{sevError, sevInfo}, sevError},
	}
	for _, tc := range cases {
		c := doctorCheck{}
		for _, s := range tc.sev {
			c.Findings = append(c.Findings, doctorFinding{Severity: s, Message: "x"})
		}
		if got := c.worst(); got != tc.want {
			t.Errorf("severities %v: want %q, got %q", tc.sev, tc.want, got)
		}
	}
}

// The published schema and the real output must agree. Validated with findings
// present, not just on an empty happy path: a schema that only ever sees
// `findings: []` has not been tested against the shape that matters.
func TestDoctor_JSONSchemaRoundTrip(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("PATH", "/usr/bin:/bin")
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, migration.QuarantineDir, "broken", "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := cmdDoctorTo([]string{"--format", "json"}, &out, io.Discard); err != nil {
		t.Fatalf("cmdDoctorTo: %v", err)
	}
	var v any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	schema, err := clischema.Get("doctor")
	if err != nil {
		t.Fatalf("Get doctor schema: %v", err)
	}
	if err := schema.Validate(v); err != nil {
		t.Errorf("doctor JSON does not validate: %v\noutput: %s", err, out.String())
	}

	// Guard the guard: if the fixture produced no findings, the round trip
	// proved nothing about the finding shape.
	doc := v.(map[string]any)
	total := 0
	for _, c := range doc["checks"].([]any) {
		total += len(c.(map[string]any)["findings"].([]any))
	}
	if total == 0 {
		t.Fatal("fixture is vacuous: schema was validated against zero findings")
	}
}

// installHarnessWithVendor writes an installed harness that defaults to the
// given vendor, so checkVendors has something that actually wants one.
func installHarnessWithVendor(t *testing.T, name, vendorName string) {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"version":"0.1.0","default_vendor":%q}`, name, vendorName)
	for _, dir := range []string{harness.InstalledDir(name), harness.InstalledDirByID("local/" + name)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The headline check from #279: default_vendor names a CLI that is not there.
func TestDoctor_VendorMissingForInstalledHarness(t *testing.T) {
	fakeHome(t)
	// An empty PATH guarantees no vendor CLI resolves, so the assertion does
	// not depend on what happens to be installed on the machine running this.
	t.Setenv("PATH", t.TempDir())
	installHarnessWithVendor(t, "demo", "codex")

	c := checkVendors()
	if c.Status != sevError {
		t.Fatalf("a missing vendor CLI must be an error, got %q:\n%s", c.Status, findingsOf(c))
	}
	got := findingsOf(c)
	if !strings.Contains(got, "codex") {
		t.Errorf("finding should name the vendor:\n%s", got)
	}
	if !strings.Contains(got, "local/demo") {
		t.Errorf("finding should name the harness that wants it, so the user knows what breaks:\n%s", got)
	}
}

// Only vendors something actually targets are reported. A machine without
// `copilot` is not broken if nothing asks for copilot.
func TestDoctor_VendorSilentWhenNothingWantsIt(t *testing.T) {
	fakeHome(t)
	t.Setenv("PATH", t.TempDir())

	if c := checkVendors(); c.Status != sevOK {
		t.Errorf("no installed harness means no vendor requirement, got %q:\n%s",
			c.Status, findingsOf(c))
	}
}

// A harness listed as installed whose manifest no longer parses. This is the
// post-migration failure that today surfaces only at run time.
func TestDoctor_HarnessListedButBroken(t *testing.T) {
	fakeHome(t)
	installHarnessWithVendor(t, "broken", "claude")

	// Corrupt the manifest LoadByID reads, leaving the install listed.
	bad := filepath.Join(harness.InstalledDirByID("local/broken"), ".harness.json")
	if err := os.WriteFile(bad, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := checkHarnesses()
	if c.Status != sevError {
		t.Fatalf("an unloadable harness must be an error, got %q:\n%s", c.Status, findingsOf(c))
	}
	if !strings.Contains(findingsOf(c), "broken") {
		t.Errorf("finding should name the harness:\n%s", findingsOf(c))
	}
}

func TestDoctor_HealthyHarnessIsSilent(t *testing.T) {
	fakeHome(t)
	installHarnessWithVendor(t, "fine", "claude")

	if c := checkHarnesses(); c.Status != sevOK {
		t.Errorf("a loadable harness must be silent, got %q:\n%s", c.Status, findingsOf(c))
	}
}
