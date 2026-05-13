package vendor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirHasContent_NonExistent(t *testing.T) {
	if dirHasContent(filepath.Join(t.TempDir(), "no-such")) {
		t.Error("non-existent dir reported as has content")
	}
}

func TestDirHasContent_Empty(t *testing.T) {
	dir := t.TempDir()
	if dirHasContent(dir) {
		t.Error("empty dir reported as has content")
	}
}

func TestDirHasContent_WithFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !dirHasContent(dir) {
		t.Error("dir with file reported as empty")
	}
}

func TestDirHasContent_WithSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !dirHasContent(dir) {
		t.Error("dir with subdir reported as empty")
	}
}

func TestFileExists_Missing(t *testing.T) {
	if fileExists(filepath.Join(t.TempDir(), "no-such")) {
		t.Error("missing file reported as existing")
	}
}

func TestFileExists_RegularFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(p) {
		t.Error("file reported missing")
	}
}

func TestFileExists_Directory(t *testing.T) {
	if fileExists(t.TempDir()) {
		t.Error("directory should not be reported as a regular file")
	}
}
