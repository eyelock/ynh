package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestToolVersion_ReadsFirstLine(t *testing.T) {
	got := toolVersion(`printf 'golangci-lint has version 1.62.0\nbuilt with go1.25\n'`, t.TempDir())
	if got != "golangci-lint has version 1.62.0" {
		t.Errorf("got %q, want the first line only — the rest is banner", got)
	}
}

// Provenance is worth having; it is not worth failing a gate for. A missing
// tool, a non-zero exit or a hang must all leave the version absent and the
// sensor untouched.
func TestToolVersion_FailureIsNeverFatal(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ name, cmd string }{
		{"command does not exist", "definitely-not-a-real-binary-xyz --version"},
		{"non-zero exit, no output", "exit 3"},
		{"empty declaration", ""},
		{"whitespace only", "   "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toolVersion(c.cmd, dir); got != "" {
				t.Errorf("got %q, want empty", got)
			}
		})
	}
}

// Some tools print their version to stdout and still exit non-zero. stdout is
// worth reading either way.
func TestToolVersion_NonZeroExitWithStdoutStillCounts(t *testing.T) {
	if got := toolVersion(`printf 'mytool 2.1\n'; exit 1`, t.TempDir()); got != "mytool 2.1" {
		t.Errorf("got %q, want the version despite the non-zero exit", got)
	}
}

// java -version prints to stderr and exits 0. A clean exit is what makes
// stderr trustworthy here; a failing one makes it a shell error message.
func TestToolVersion_CleanExitOnStderrCounts(t *testing.T) {
	if got := toolVersion(`printf 'openjdk 21.0.1\n' >&2`, t.TempDir()); got != "openjdk 21.0.1" {
		t.Errorf("got %q, want the stderr version from a clean exit", got)
	}
}

// A `--version` that opens a pager or waits on stdin would otherwise hang the
// gate, and a gate that hangs is worse than one with no version recorded.
func TestToolVersion_HangIsBoundedAndYieldsNothing(t *testing.T) {
	done := make(chan string, 1)
	go func() { done <- toolVersion("sleep 30", t.TempDir()) }()
	select {
	case got := <-done:
		if got != "" {
			t.Errorf("a timed-out probe must record nothing, got %q", got)
		}
	case <-timeoutAfter():
		t.Fatal("toolVersion did not return — the probe is unbounded and can hang the gate")
	}
}

// A banner longer than the cap must be truncated rather than embedded whole in
// every sensor result of every run.
func TestToolVersion_Truncates(t *testing.T) {
	got := toolVersion(`printf '%0.sx' $(seq 1 500); printf '\n'`, t.TempDir())
	if len(got) > maxToolVersion {
		t.Errorf("length %d exceeds cap %d", len(got), maxToolVersion)
	}
	if len(got) == 0 {
		t.Error("a long version should be truncated, not dropped")
	}
}

// The gate runs every turn of the inner loop. Re-probing a constant each time
// would add real wall-clock to every iteration.
func TestToolVersion_IsCached(t *testing.T) {
	dir := t.TempDir()
	marker := dir + "/probe-count"
	cmd := `printf 'x\n' >> ` + marker + `; printf 'v1.0\n'`

	first := toolVersion(cmd, dir)
	second := toolVersion(cmd, dir)
	if first != "v1.0" || second != "v1.0" {
		t.Fatalf("unexpected results: %q %q", first, second)
	}
	data := readFileOrEmpty(t, marker)
	if n := strings.Count(data, "x"); n != 1 {
		t.Errorf("probe ran %d times, want 1 — the result is cached per process", n)
	}
}

// timeoutAfter gives the probe generous headroom over its own 5s bound, so a
// slow machine does not make this test flaky while it still catches an
// unbounded probe.
func timeoutAfter() <-chan time.Time { return time.After(toolVersionTimeout + 10*time.Second) }

func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// The CI-only failure this guards against.
//
// CommandContext kills the shell when the context expires, but Run blocks
// until the output pipes close, and a shell that *forks* rather than execs
// leaves its child holding them. `sh -c "sleep 30"` returned promptly on macOS
// and ran the full thirty seconds on Linux, so the bound was real only on the
// platform it was written on. A backgrounded child reproduces that everywhere.
func TestToolVersion_BoundHoldsWhenAChildKeepsThePipesOpen(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		toolVersion("sleep 30 & sleep 30", t.TempDir())
	}()
	select {
	case <-done:
	case <-time.After(toolVersionTimeout + 5*time.Second):
		t.Fatal("the probe outlived its bound while a child held the output pipes — " +
			"a gate that hangs is worse than one with no version recorded")
	}
}
