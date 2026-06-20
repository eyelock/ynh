package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpoint_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := &Checkpoint{
		SessionID:         "sess-abc",
		Backend:           "claude",
		ResumeToken:       "uuid-123",
		Phase:             PhaseAct,
		PlanFinalized:     true,
		ApprovedPlan:      "do the thing",
		LastCompletedTurn: 4,
		PendingMessage:    "<sensor-results>…</sensor-results>",
		Budget: CheckpointBudget{
			Turns:          4,
			Tokens:         12345,
			WallConsumedMS: 83000,
			PlanIterations: 2,
		},
		Task: "fix the bug",
	}

	if err := writeCheckpoint(dir, want); err != nil {
		t.Fatalf("writeCheckpoint: %v", err)
	}

	got, err := readCheckpoint(dir)
	if err != nil {
		t.Fatalf("readCheckpoint: %v", err)
	}

	if got.Version != checkpointVersion {
		t.Errorf("Version = %d, want %d", got.Version, checkpointVersion)
	}
	if got.UpdatedAt == "" {
		t.Error("UpdatedAt should be stamped on write")
	}
	// Compare the meaningful fields (Version/UpdatedAt are stamped by write,
	// which mutates the passed pointer too — normalise both sides).
	got.Version, got.UpdatedAt = 0, ""
	wantCopy := *want
	wantCopy.Version, wantCopy.UpdatedAt = 0, ""
	if *got != wantCopy {
		t.Errorf("roundtrip mismatch:\n got  %+v\n want %+v", *got, wantCopy)
	}
}

func TestWriteCheckpoint_AtomicNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	cp := &Checkpoint{SessionID: "s", Backend: "mock", Phase: PhaseAct}

	// Write twice to exercise the overwrite-via-rename path.
	for i := 0; i < 2; i++ {
		if err := writeCheckpoint(dir, cp); err != nil {
			t.Fatalf("writeCheckpoint #%d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if strings.HasPrefix(e.Name(), ".checkpoint-") && strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file after atomic write: %s", e.Name())
		}
	}
	if len(names) != 1 || names[0] != checkpointFile {
		t.Errorf("dir contents = %v, want exactly [%s]", names, checkpointFile)
	}
}

func TestReadCheckpoint_Missing(t *testing.T) {
	_, err := readCheckpoint(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing checkpoint")
	}
	if !strings.Contains(err.Error(), "no checkpoint") {
		t.Errorf("error should mention missing checkpoint, got: %v", err)
	}
}

func TestReadCheckpoint_Corrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(checkpointPath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readCheckpoint(dir)
	if err == nil {
		t.Fatal("expected error for corrupt checkpoint")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Errorf("error should mention corruption, got: %v", err)
	}
}

func TestReadCheckpoint_VersionMismatch(t *testing.T) {
	dir := t.TempDir()
	raw, _ := json.Marshal(map[string]any{"version": checkpointVersion + 99, "session_id": "s"})
	if err := os.WriteFile(checkpointPath(dir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readCheckpoint(dir)
	if err == nil || !strings.Contains(err.Error(), "unsupported checkpoint version") {
		t.Errorf("expected version-mismatch error, got: %v", err)
	}
}

func TestReadCheckpoint_MissingSessionID(t *testing.T) {
	dir := t.TempDir()
	raw, _ := json.Marshal(map[string]any{"version": checkpointVersion})
	if err := os.WriteFile(checkpointPath(dir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readCheckpoint(dir)
	if err == nil || !strings.Contains(err.Error(), "session_id") {
		t.Errorf("expected missing-session_id error, got: %v", err)
	}
}

func TestSessionDirFromEmit(t *testing.T) {
	cases := []struct {
		emit, want string
	}{
		{"", ""},
		{"-", ""},
		{"/tmp/session/trajectory.jsonl", "/tmp/session"},
		{"trajectory.jsonl", "."},
	}
	for _, c := range cases {
		if got := sessionDirFromEmit(c.emit); got != c.want {
			t.Errorf("sessionDirFromEmit(%q) = %q, want %q", c.emit, got, c.want)
		}
	}
	// The derived directory must match where a checkpoint would land.
	if got := filepath.Dir("/a/b/trajectory.jsonl"); got != sessionDirFromEmit("/a/b/trajectory.jsonl") {
		t.Errorf("sessionDirFromEmit disagrees with filepath.Dir")
	}
}
