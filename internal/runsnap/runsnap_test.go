//go:build unix

package runsnap

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

func lockPathFn(root string) func(string) string {
	return func(h string) string { return filepath.Join(root, "locks", h+".lock") }
}

func build(t *testing.T, root, name string, files map[string]string) *Snapshot {
	t.Helper()
	st, err := Stage(root, name)
	if err != nil {
		t.Fatal(err)
	}
	for p, c := range files {
		full := filepath.Join(st, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Seal(root, name, st, lockPathFn(root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Release)
	return s
}

func TestUnchangedRebuildReusesSnapshot(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{".claude/a.md": "one", "b": "two"}
	a := build(t, root, "h", files)
	b := build(t, root, "h", files)
	if a.Hash != b.Hash || a.Dir != b.Dir {
		t.Fatalf("expected reuse: %s vs %s", a.Dir, b.Dir)
	}
	if a.Reused || !b.Reused {
		t.Fatalf("Reused: first=%v second=%v", a.Reused, b.Reused)
	}
	entries, _ := os.ReadDir(filepath.Join(root, SnapshotsDirName, "h"))
	if len(entries) != 1 {
		t.Fatalf("want one snapshot and no staging, got %d entries", len(entries))
	}
	got, err := filepath.EvalSymlinks(filepath.Join(root, "h"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(a.Dir)
	if got != want {
		t.Fatalf("link resolves to %s, want %s", got, want)
	}
}

func TestChangedInputsMakeNewSnapshotAndKeepOld(t *testing.T) {
	root := t.TempDir()
	a := build(t, root, "h", map[string]string{"f": "v1"})
	b := build(t, root, "h", map[string]string{"f": "v2"})
	if a.Hash == b.Hash {
		t.Fatal("different content hashed the same")
	}
	data, err := os.ReadFile(filepath.Join(a.Dir, "f"))
	if err != nil || string(data) != "v1" {
		t.Fatalf("live session's snapshot changed: %q %v", data, err)
	}
	data, _ = os.ReadFile(filepath.Join(root, "h", "f"))
	if string(data) != "v2" {
		t.Fatalf("link not swapped: %q", data)
	}
}

func TestModeAndPathAffectHash(t *testing.T) {
	d1, d2 := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(d1, "x"), []byte("c"), 0o644)
	_ = os.WriteFile(filepath.Join(d2, "x"), []byte("c"), 0o755)
	h1, _ := HashTree(d1)
	h2, _ := HashTree(d2)
	if h1 == h2 {
		t.Fatal("mode ignored")
	}
}

func TestSwapReplacesPreexistingRealDirOnce(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "h")
	_ = os.MkdirAll(old, 0o755)
	_ = os.WriteFile(filepath.Join(old, "legacy"), []byte("x"), 0o644)
	s := build(t, root, "h", map[string]string{"f": "v1"})
	fi, err := os.Lstat(filepath.Join(root, "h"))
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("not a link: %v %v", fi, err)
	}
	if _, err := os.Stat(filepath.Join(s.Link, "legacy")); err == nil {
		t.Fatal("legacy file survived")
	}
}

func TestSwapIsAtomicUnderReaders(t *testing.T) {
	root := t.TempDir()
	build(t, root, "h", map[string]string{"f": "v0"})
	// The writer bumps swaps before and after each build, so an odd value
	// means one is under way and a changed value means one began or ended.
	var swaps atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				atRead := swaps.Load()
				if _, err := os.ReadFile(filepath.Join(root, "h", "f")); err != nil {
					obs := missingPathObservations(root, &swaps, atRead, err)
					t.Errorf("reader saw a missing path: %v\n%s", err, missingPathDiagnostics(root, obs))
					return
				}
			}
		}()
	}
	for i := range 60 {
		// Each snapshot stays held until the test ends: this test is about the
		// link swap, and a reader resolving the link is not a session, so it
		// holds no lock and an unheld old snapshot may be pruned under it.
		swaps.Add(1)
		build(t, root, "h", map[string]string{"f": "v" + string(rune('a'+i%26)) + string(rune('A'+i/26))})
		swaps.Add(1)
	}
	close(stop)
	wg.Wait()
}

// missingPathObservations is what a reader that saw h/f missing can learn at
// once, before anything slow runs: the writer keeps swapping the whole time,
// so a check made tens of milliseconds later says nothing about the moment of
// the miss. swapsAtRead is the swap counter read just before the failed read;
// it is read again after the checks. Equal counters mean no swap began or
// ended in between, so a clean re-read then points at the kernel's lookup, and
// a missing link or target points at swapLink.
func missingPathObservations(root string, swaps *atomic.Int64, swapsAtRead int64, readErr error) string {
	var b strings.Builder
	link := filepath.Join(root, "h")
	fmt.Fprintf(&b, "failed read of h/f: %v\n", readErr)
	_, err := os.Lstat(link)
	fmt.Fprintf(&b, "lstat h: %v\n", err)
	_, err = os.Stat(filepath.Join(link, "f"))
	fmt.Fprintf(&b, "stat h/f: %v\n", err)
	target, rlErr := os.Readlink(link)
	fmt.Fprintf(&b, "readlink h: %q %v\n", target, rlErr)
	if rlErr == nil {
		_, err = os.Stat(filepath.Join(root, target))
		fmt.Fprintf(&b, "stat target: %v\n", err)
		_, err = os.Stat(filepath.Join(root, target, "f"))
		fmt.Fprintf(&b, "stat target/f: %v\n", err)
	}
	_, reread := os.ReadFile(filepath.Join(link, "f"))
	fmt.Fprintf(&b, "read h/f again: %v\n", reread)
	after := swaps.Load()
	fmt.Fprintf(&b, "swap counter (odd means a build is under way): %d at the read, %d after these checks\n", swapsAtRead, after)
	// The counter moves when a build starts and when it ends, and a swap
	// happens inside one. An odd value at the read means a build (so
	// possibly a swap) was already under way, which the verdicts cannot rule out.
	switch {
	case reread == nil && after == swapsAtRead && swapsAtRead%2 == 0:
		b.WriteString("verdict: no swap between the failed read and a clean re-read: the kernel's lookup, not swapLink\n")
	case reread == nil:
		b.WriteString("verdict: the re-read is clean but a build was under way or began or ended in between: not conclusive\n")
	case after == swapsAtRead && swapsAtRead%2 == 0:
		b.WriteString("verdict: still missing with no swap in between: the link or its target is really gone, a swapLink or lock bug\n")
	default:
		b.WriteString("verdict: still missing and a build was under way or began or ended in between: look at the link and target lines\n")
	}
	return b.String()
}

// missingPathDiagnostics adds the slow, environmental facts, after the
// observations above were taken: the kernel and the temp dir's filesystem
// (the swap rests on rename over a symlink being atomic there).
func missingPathDiagnostics(root, observations string) string {
	var b strings.Builder
	b.WriteString(observations)
	run := func(name string, args ...string) {
		out, err := exec.Command(name, args...).CombinedOutput()
		fmt.Fprintf(&b, "$ %s %s\n%s", name, strings.Join(args, " "), out)
		if err != nil {
			fmt.Fprintf(&b, "(%v)\n", err)
		}
	}
	run("uname", "-a")
	run("df", "-P", root)
	if out, err := exec.Command("stat", "-f", "-c", "filesystem type: %T", root).CombinedOutput(); err == nil {
		b.Write(out)
	}
	entries, _ := os.ReadDir(filepath.Join(root, SnapshotsDirName, "h"))
	fmt.Fprintf(&b, "snapshots: %d\n", len(entries))
	return b.String()
}

func TestSharedLockHeld(t *testing.T) {
	root := t.TempDir()
	a := build(t, root, "h", map[string]string{"f": "v1"})
	f, err := os.OpenFile(lockPathFn(root)(a.Hash), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("exclusive lock granted while snapshot held")
	}
	// A second shared holder is fine.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		t.Fatalf("shared lock refused: %v", err)
	}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestLiveSessionsSnapshotIsNeverDeleted(t *testing.T) {
	root := t.TempDir()
	a := build(t, root, "h", map[string]string{"f": "v1"}) // live: lock held
	build(t, root, "h", map[string]string{"f": "v2"}).Release()
	build(t, root, "h", map[string]string{"f": "v3"})
	if !exists(a.Dir) {
		t.Fatal("a live session's snapshot was deleted")
	}
	if !exists(lockPathFn(root)(a.Hash)) {
		t.Fatal("a live session's lock file was deleted")
	}
	data, _ := os.ReadFile(filepath.Join(a.Dir, "f"))
	if string(data) != "v1" {
		t.Fatalf("live snapshot changed: %q", data)
	}
}

func TestFinishedSessionsSnapshotIsDeletedByNextRun(t *testing.T) {
	root := t.TempDir()
	a := build(t, root, "h", map[string]string{"f": "v1"})
	b := build(t, root, "h", map[string]string{"f": "v2"})
	if !exists(a.Dir) {
		t.Fatal("snapshot deleted while its session is live")
	}
	a.Release()
	// b is current and held; a is finished and not current.
	c := build(t, root, "h", map[string]string{"f": "v2"})
	if !c.Reused || c.Dir != b.Dir {
		t.Fatal("expected reuse of the current snapshot")
	}
	if exists(a.Dir) {
		t.Fatal("a finished session's non-current snapshot was kept")
	}
	if exists(lockPathFn(root)(a.Hash)) {
		t.Fatal("its lock file was kept")
	}
	if !exists(b.Dir) {
		t.Fatal("current snapshot deleted")
	}
}

func TestCurrentSnapshotIsNeverDeleted(t *testing.T) {
	root := t.TempDir()
	a := build(t, root, "h", map[string]string{"f": "v1"})
	a.Release() // no session holds it
	b := build(t, root, "h", map[string]string{"f": "v1"})
	if !b.Reused || !exists(b.Dir) {
		t.Fatal("the current snapshot was deleted or not reused")
	}
	b.Release()
	Prune(root, "h", b.Hash, lockPathFn(root))
	if !exists(b.Dir) {
		t.Fatal("Prune deleted the current snapshot")
	}
}

func TestPruneLeavesOtherHarnessesAndStaging(t *testing.T) {
	root := t.TempDir()
	o := build(t, root, "other", map[string]string{"f": "x"})
	o.Release()
	a := build(t, root, "h", map[string]string{"f": "v1"})
	a.Release()
	b := build(t, root, "h", map[string]string{"f": "v2"})
	staging := filepath.Join(root, SnapshotsDirName, "h", stagingPrefix+"x")
	_ = os.MkdirAll(staging, 0o700)
	Prune(root, "h", b.Hash, lockPathFn(root))
	if !exists(o.Dir) || !exists(staging) {
		t.Fatal("Prune touched another harness's snapshot or a staging folder")
	}
}

// Sessions come and go while runs seal: a held snapshot always survives.
func TestPruneUnderConcurrentSessions(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	var mu sync.Mutex // stands in for the assembly lock
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				mu.Lock()
				st, err := Stage(root, "h")
				if err != nil {
					t.Error(err)
					mu.Unlock()
					return
				}
				_ = os.WriteFile(filepath.Join(st, "f"), []byte{byte('a' + (i+j)%4)}, 0o644)
				s, err := Seal(root, "h", st, lockPathFn(root))
				mu.Unlock()
				if err != nil {
					t.Error(err)
					return
				}
				if !exists(filepath.Join(s.Dir, "f")) {
					t.Error("held snapshot missing")
				}
				s.Release()
			}
		}(i)
	}
	wg.Wait()
}

func TestStageClearsOnlyStaleStaging(t *testing.T) {
	root := t.TempDir()
	a := build(t, root, "h", map[string]string{"f": "v1"})
	stale := filepath.Join(root, SnapshotsDirName, "h", stagingPrefix+"dead")
	_ = os.MkdirAll(stale, 0o700)
	if _, err := Stage(root, "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Fatal("stale staging kept")
	}
	if _, err := os.Stat(a.Dir); err != nil {
		t.Fatal("snapshot removed")
	}
}
