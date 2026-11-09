//go:build unix

package runsnap

import (
	"os"
	"path/filepath"
	"sync"
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
				if _, err := os.ReadFile(filepath.Join(root, "h", "f")); err != nil {
					t.Errorf("reader saw a missing path: %v", err)
					return
				}
			}
		}()
	}
	for i := range 60 {
		s := build(t, root, "h", map[string]string{"f": "v" + string(rune('a'+i%26)) + string(rune('A'+i/26))})
		s.Release()
	}
	close(stop)
	wg.Wait()
}

func TestSharedLockHeldAndNeverDeleted(t *testing.T) {
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
	a.Release()
	build(t, root, "h", map[string]string{"f": "v2"})
	if _, err := os.Stat(a.Dir); err != nil {
		t.Fatal("old snapshot deleted")
	}
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
