package freshness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// write creates a file with a specific modification time, so ordering is
// asserted rather than raced. Real filesystems have coarse timestamp
// granularity and a test that writes two files back to back and expects one to
// be newer is a flake waiting to happen.
func write(t *testing.T, dir, name, content string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"add", "-A"},
		{"commit", "-m", "fixture"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestAbsentWhenNoArtifactMatched(t *testing.T) {
	dir := t.TempDir()
	got := Evaluate(dir, nil, nil)
	if got.State != StateAbsent {
		t.Fatalf("state = %q, want %q", got.State, StateAbsent)
	}
	if got.Reason == "" {
		t.Error("absent result carried no reason; the operator is told nothing")
	}
}

func TestFreshWhenArtifactNewerThanInputs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/main.go", "package main", 2*time.Hour)
	artifact := write(t, dir, "out/result.json", "{}", 1*time.Hour)

	got := Evaluate(dir, []string{artifact}, []string{"src/*"})
	if got.State != StateFresh {
		t.Fatalf("state = %q (%s), want %q", got.State, got.Reason, StateFresh)
	}
	if got.Basis != BasisMTime {
		t.Errorf("basis = %q, want %q", got.Basis, BasisMTime)
	}
}

func TestStaleWhenInputNewerThanArtifact(t *testing.T) {
	dir := t.TempDir()
	artifact := write(t, dir, "out/result.json", "{}", 2*time.Hour)
	write(t, dir, "src/main.go", "package main", 1*time.Hour)

	got := Evaluate(dir, []string{artifact}, []string{"src/*"})
	if got.State != StateStale {
		t.Fatalf("state = %q (%s), want %q", got.State, got.Reason, StateStale)
	}
	if got.Reason == "" {
		t.Error("stale result carried no reason")
	}
}

// The whole point of `observes`: an edit somewhere the artifact does not depend
// on must not invalidate it. Without this, every docs commit re-runs the suite
// and the gate gets switched off.
func TestUnrelatedChangeDoesNotStale(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/main.go", "package main", 3*time.Hour)
	artifact := write(t, dir, "out/result.json", "{}", 2*time.Hour)
	write(t, dir, "README.md", "# docs", 1*time.Hour)

	got := Evaluate(dir, []string{artifact}, []string{"src/*"})
	if got.State != StateFresh {
		t.Fatalf("state = %q (%s), want fresh: README is not an observed input",
			got.State, got.Reason)
	}
}

// With no `observes`, the whole tracked tree counts — so the same unrelated
// edit *does* stale it. That is the deliberate cost of not declaring inputs.
func TestWholeTreeDefaultIsStrict(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/main.go", "package main", 3*time.Hour)
	artifact := write(t, dir, "out/result.json", "{}", 2*time.Hour)
	write(t, dir, "README.md", "# docs", 1*time.Hour)
	gitInit(t, dir)
	// git add/commit rewrites nothing on disk, but re-stamp to be certain the
	// ordering under test is the one we set.
	write(t, dir, "README.md", "# docs", 1*time.Hour)

	got := Evaluate(dir, []string{artifact}, nil)
	if got.State != StateStale {
		t.Fatalf("state = %q (%s), want stale: undeclared inputs mean the whole tree",
			got.State, got.Reason)
	}
}

// An artifact must not count as an input to its own sensor, or writing the
// newer of two declared files would stale the sensor against itself.
//
// One artifact cannot show this: comparing a file's mtime to its own is always
// equal and never "after". It takes two.
func TestArtifactsExcludedFromTheirOwnInputs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/main.go", "package main", 4*time.Hour)
	oldArtifact := write(t, dir, "out/a.json", "{}", 3*time.Hour)
	newArtifact := write(t, dir, "out/b.json", "{}", 1*time.Minute)
	gitInit(t, dir)
	write(t, dir, "src/main.go", "package main", 4*time.Hour)
	write(t, dir, "out/a.json", "{}", 3*time.Hour)
	write(t, dir, "out/b.json", "{}", 1*time.Minute)

	got := Evaluate(dir, []string{oldArtifact, newArtifact}, nil)
	if got.State != StateFresh {
		t.Fatalf("state = %q (%s), want fresh: b.json is an artifact, not an input to itself",
			got.State, got.Reason)
	}
}

// .ynh/ holds the gate's own state, and ynh's own docs say the baseline is
// meant to be committed — so it is tracked, and git alone will not exclude it.
// Counting it would mean recording a baseline invalidates every artifact in
// the repository.
func TestTrackedYnhDirExcludedFromInputs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/main.go", "package main", 3*time.Hour)
	artifact := write(t, dir, "result.json", "{}", 2*time.Hour)
	// Committed, exactly as a real repository carries its ratchet.
	write(t, dir, ".ynh/baseline/h/lint.json", "{}", 1*time.Minute)
	gitInit(t, dir)
	write(t, dir, "src/main.go", "package main", 3*time.Hour)
	write(t, dir, "result.json", "{}", 2*time.Hour)
	write(t, dir, ".ynh/baseline/h/lint.json", "{}", 1*time.Minute)

	if !isTracked(t, dir, ".ynh/baseline/h/lint.json") {
		t.Fatal("fixture is wrong: .ynh must be tracked for this test to exercise the exclusion")
	}

	got := Evaluate(dir, []string{artifact}, nil)
	if got.State != StateFresh {
		t.Fatalf("state = %q (%s), want fresh: .ynh is the gate's own state",
			got.State, got.Reason)
	}
}

// isTracked guards the fixture above. A test that silently stops exercising
// the branch it names is worse than no test.
func isTracked(t *testing.T, dir, rel string) bool {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "ls-files", "--", rel).Output()
	if err != nil {
		t.Fatal(err)
	}
	return len(out) > 0
}

// Untracked build output must not invalidate artifacts, or `make build` breaks
// every files sensor in the repository.
func TestUntrackedOutputExcludedFromInputs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/main.go", "package main", 3*time.Hour)
	artifact := write(t, dir, "result.json", "{}", 2*time.Hour)
	gitInit(t, dir)
	write(t, dir, "src/main.go", "package main", 3*time.Hour)
	write(t, dir, "result.json", "{}", 2*time.Hour)
	// Never committed, and newer than the artifact.
	write(t, dir, "bin/ynh", "binary", 1*time.Minute)

	got := Evaluate(dir, []string{artifact}, nil)
	if got.State != StateFresh {
		t.Fatalf("state = %q (%s), want fresh: untracked output is not an input",
			got.State, got.Reason)
	}
}

// No observes and no git means ynh genuinely cannot see. Unknown, not fresh:
// a gate that cannot see is not a gate that passed.
func TestUnknownWithoutObservesOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	artifact := write(t, dir, "result.json", "{}", 1*time.Hour)

	got := Evaluate(dir, []string{artifact}, nil)
	if got.State != StateUnknown {
		t.Fatalf("state = %q (%s), want %q", got.State, got.Reason, StateUnknown)
	}
}

// An `observes` glob matching nothing leaves nothing to compare against.
// Reporting fresh there would be the exact false green this package prevents.
func TestUnknownWhenObservesMatchesNothing(t *testing.T) {
	dir := t.TempDir()
	artifact := write(t, dir, "result.json", "{}", 1*time.Hour)

	got := Evaluate(dir, []string{artifact}, []string{"nonexistent/*"})
	if got.State != StateUnknown {
		t.Fatalf("state = %q (%s), want %q", got.State, got.Reason, StateUnknown)
	}
}

// A sensor declaring several artifacts is only as current as its least current
// one, so the oldest is the end that decides.
func TestOldestArtifactDecides(t *testing.T) {
	dir := t.TempDir()
	oldArtifact := write(t, dir, "out/a.json", "{}", 3*time.Hour)
	newArtifact := write(t, dir, "out/b.json", "{}", 1*time.Minute)
	write(t, dir, "src/main.go", "package main", 2*time.Hour)

	got := Evaluate(dir, []string{oldArtifact, newArtifact}, []string{"src/*"})
	if got.State != StateStale {
		t.Fatalf("state = %q (%s), want stale: a.json predates the input",
			got.State, got.Reason)
	}
}

// `observes` patterns must mean what they look like they mean.
//
// Go's filepath.Match treats `**` as an ordinary `*` — it matches inside one
// path element and never descends. A harness declaring the obvious
// `services/**` would get the directories one level down, whose mtimes say
// nothing, leaving no usable inputs and a verdict of `unknown`. This was found
// against a real harness before it shipped.
func TestObservesPatterns(t *testing.T) {
	setup := func(t *testing.T) (dir, artifact string) {
		t.Helper()
		dir = t.TempDir()
		write(t, dir, "services/gateway/auth.go", "package gw", 3*time.Hour)
		write(t, dir, "services/gateway/internal/deep.go", "package internal", 3*time.Hour)
		write(t, dir, "services/registry/reg.go", "package reg", 3*time.Hour)
		write(t, dir, "docs/readme.md", "# docs", 3*time.Hour)
		artifact = write(t, dir, "out/result.json", "{}", 2*time.Hour)
		return dir, artifact
	}

	// The pattern that would silently observe nothing before this fix.
	t.Run("** descends", func(t *testing.T) {
		dir, artifact := setup(t)
		write(t, dir, "services/gateway/internal/deep.go", "changed", 1*time.Minute)
		got := Evaluate(dir, []string{artifact}, []string{"services/**"})
		if got.State != StateStale {
			t.Fatalf("state = %q (%s), want stale: services/** must reach nested files",
				got.State, got.Reason)
		}
	})

	// A bare directory is the same subtree, so the three spellings agree.
	for _, pat := range []string{"services", "services/*", "services/**"} {
		t.Run("directory spelling "+pat, func(t *testing.T) {
			dir, artifact := setup(t)
			write(t, dir, "services/registry/reg.go", "changed", 1*time.Minute)
			got := Evaluate(dir, []string{artifact}, []string{pat})
			if got.State != StateStale {
				t.Fatalf("%s: state = %q (%s), want stale", pat, got.State, got.Reason)
			}
		})
	}

	// A suffix after ** filters, and still descends.
	t.Run("**/*.go filters and descends", func(t *testing.T) {
		dir, artifact := setup(t)
		write(t, dir, "services/gateway/internal/deep.go", "changed", 1*time.Minute)
		got := Evaluate(dir, []string{artifact}, []string{"services/**/*.go"})
		if got.State != StateStale {
			t.Fatalf("state = %q (%s), want stale", got.State, got.Reason)
		}
	})

	// And the filter genuinely excludes: a changed .md under a **/*.go
	// pattern is not an observed input.
	t.Run("**/*.go excludes other extensions", func(t *testing.T) {
		dir, artifact := setup(t)
		write(t, dir, "services/gateway/notes.md", "changed", 1*time.Minute)
		got := Evaluate(dir, []string{artifact}, []string{"services/**/*.go"})
		if got.State != StateFresh {
			t.Fatalf("state = %q (%s), want fresh: a .md is not matched by **/*.go",
				got.State, got.Reason)
		}
	})

	// Scoping still holds — an edit outside the observed subtree is not an input.
	t.Run("outside the subtree is not observed", func(t *testing.T) {
		dir, artifact := setup(t)
		write(t, dir, "docs/readme.md", "changed", 1*time.Minute)
		got := Evaluate(dir, []string{artifact}, []string{"services/**"})
		if got.State != StateFresh {
			t.Fatalf("state = %q (%s), want fresh: docs/ is not under services/",
				got.State, got.Reason)
		}
	})
}
