package vendor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallSymlinks_CreatesLinks(t *testing.T) {
	stagingDir := t.TempDir()
	projectDir := t.TempDir()

	// Create staging artifacts.
	skillDir := filepath.Join(stagingDir, ".test", "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	agentFile := filepath.Join(stagingDir, ".test", "agents")
	if err := os.MkdirAll(agentFile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentFile, "reviewer.md"), []byte("review"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifactDirs := map[string]string{"skills": "skills", "agents": "agents"}
	entries, err := installSymlinks(stagingDir, projectDir, ".test", artifactDirs)
	if err != nil {
		t.Fatalf("installSymlinks failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}

	// Verify symlinks exist and point to correct targets.
	for _, entry := range entries {
		info, err := os.Lstat(entry.Link)
		if err != nil {
			t.Fatalf("symlink not created at %s: %v", entry.Link, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is not a symlink", entry.Link)
		}
		actual, _ := os.Readlink(entry.Link)
		if actual != entry.Target {
			t.Errorf("symlink %s points to %s, want %s", entry.Link, actual, entry.Target)
		}
	}
}

func TestInstallSymlinks_SkipsExistingFile(t *testing.T) {
	stagingDir := t.TempDir()
	projectDir := t.TempDir()

	// Create staging artifact.
	skillDir := filepath.Join(stagingDir, ".test", "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a real file at the link location (user's own config).
	userFile := filepath.Join(projectDir, ".test", "skills", "hello")
	if err := os.MkdirAll(filepath.Dir(userFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte("user's file"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifactDirs := map[string]string{"skills": "skills"}
	entries, err := installSymlinks(stagingDir, projectDir, ".test", artifactDirs)
	if err != nil {
		t.Fatalf("installSymlinks failed: %v", err)
	}

	// Should skip the existing file.
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0 (should skip existing file)", len(entries))
	}

	// User's file should be untouched.
	data, _ := os.ReadFile(userFile)
	if string(data) != "user's file" {
		t.Errorf("user's file was modified: %q", string(data))
	}
}

func TestInstallSymlinks_IdempotentReinstall(t *testing.T) {
	stagingDir := t.TempDir()
	projectDir := t.TempDir()

	skillDir := filepath.Join(stagingDir, ".test", "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifactDirs := map[string]string{"skills": "skills"}

	// First install.
	entries1, err := installSymlinks(stagingDir, projectDir, ".test", artifactDirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries1) != 1 {
		t.Fatalf("first install entries = %d, want 1", len(entries1))
	}

	// Second install (should replace existing symlink).
	entries2, err := installSymlinks(stagingDir, projectDir, ".test", artifactDirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries2) != 1 {
		t.Fatalf("second install entries = %d, want 1", len(entries2))
	}

	// Symlink should still be valid.
	actual, _ := os.Readlink(entries2[0].Link)
	if actual != entries2[0].Target {
		t.Errorf("symlink points to %s, want %s", actual, entries2[0].Target)
	}
}

func TestInstallSymlinks_CreatesParentDirs(t *testing.T) {
	stagingDir := t.TempDir()
	projectDir := t.TempDir()

	// Project has no .test/ directory at all.
	skillDir := filepath.Join(stagingDir, ".test", "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifactDirs := map[string]string{"skills": "skills"}
	entries, err := installSymlinks(stagingDir, projectDir, ".test", artifactDirs)
	if err != nil {
		t.Fatalf("installSymlinks failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}

	// Parent directories should have been created.
	if _, err := os.Stat(filepath.Join(projectDir, ".test", "skills")); err != nil {
		t.Errorf("parent dir not created: %v", err)
	}
}

func TestCleanSymlinks_RemovesLinks(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	entries := []SymlinkEntry{{Target: target, Link: link}}
	if err := cleanSymlinks(entries); err != nil {
		t.Fatalf("cleanSymlinks failed: %v", err)
	}

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Error("symlink should have been removed")
	}
}

func TestCleanSymlinks_SkipsNonSymlinks(t *testing.T) {
	dir := t.TempDir()

	// Create a real file where the symlink should be.
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(link, []byte("real file"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := []SymlinkEntry{{Target: "/some/target", Link: link}}
	if err := cleanSymlinks(entries); err != nil {
		t.Fatalf("cleanSymlinks failed: %v", err)
	}

	// Real file should still exist.
	if _, err := os.Stat(link); err != nil {
		t.Error("real file should not have been removed")
	}
}

func TestCleanSymlinks_SkipsMissing(t *testing.T) {
	entries := []SymlinkEntry{{Target: "/no/target", Link: "/no/link"}}
	if err := cleanSymlinks(entries); err != nil {
		t.Fatalf("cleanSymlinks should not error on missing: %v", err)
	}
}

func TestCleanSymlinks_SkipsWrongTarget(t *testing.T) {
	dir := t.TempDir()

	target1 := filepath.Join(dir, "target1")
	if err := os.WriteFile(target1, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	target2 := filepath.Join(dir, "target2")
	if err := os.WriteFile(target2, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target2, link); err != nil {
		t.Fatal(err)
	}

	// Entry says it should point to target1, but it points to target2.
	entries := []SymlinkEntry{{Target: target1, Link: link}}
	if err := cleanSymlinks(entries); err != nil {
		t.Fatalf("cleanSymlinks failed: %v", err)
	}

	// Symlink should not have been removed (wrong target).
	if _, err := os.Lstat(link); err != nil {
		t.Error("symlink with wrong target should not have been removed")
	}
}

func TestNeedsSymlinks(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"claude", false},
		{"cursor", true},
		{"codex", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := Get(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if got := adapter.NeedsSymlinks(); got != tt.want {
				t.Errorf("NeedsSymlinks() = %v, want %v", got, tt.want)
			}
		})
	}
}
