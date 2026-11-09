package vendor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const snapRoot = "/home/x/.ynh/run/.snapshots/local--seat"

func writeCodexRollout(t *testing.T, home, name string, mod time.Time, line map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "11", "09")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(line)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, append(b, []byte("\n{\"type\":\"event_msg\"}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func meta(id, cwd string) map[string]any {
	return map[string]any{"type": "session_meta", "payload": map[string]any{"id": id, "cwd": cwd, "base_instructions": "x"}}
}

func TestCodexResolveLastSession_NewestAcrossSnapshots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	now := time.Now()
	writeCodexRollout(t, home, "rollout-a.jsonl", now.Add(-3*time.Hour), meta("old", snapRoot+"/aaaa"))
	writeCodexRollout(t, home, "rollout-b.jsonl", now.Add(-1*time.Hour), meta("newest-here", snapRoot+"/bbbb"))
	// Newer, but another harness's snapshot (a sibling folder with the same prefix) and an unrelated project.
	writeCodexRollout(t, home, "rollout-c.jsonl", now.Add(-30*time.Minute), meta("sibling", snapRoot+"-other/cccc"))
	writeCodexRollout(t, home, "rollout-d.jsonl", now.Add(-10*time.Minute), meta("project", "/work/project"))

	got, err := (&Codex{}).ResolveLastSession(snapRoot, time.Time{})
	if err != nil || got != "newest-here" {
		t.Fatalf("got %q, %v; want newest-here", got, err)
	}
}

func TestCodexResolveLastSession_NoneAndUnreadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	if _, err := (&Codex{}).ResolveLastSession(snapRoot, time.Time{}); !errors.Is(err, ErrNoResumableSession) {
		t.Errorf("no store: err = %v, want ErrNoResumableSession", err)
	}
	writeCodexRollout(t, home, "rollout-a.jsonl", time.Now(), meta("x", "/work/project"))
	if _, err := (&Codex{}).ResolveLastSession(snapRoot, time.Time{}); !errors.Is(err, ErrNoResumableSession) {
		t.Errorf("none under root: err = %v, want ErrNoResumableSession", err)
	}
	// The format changed: files exist, none has a session_meta first line.
	home2 := t.TempDir()
	t.Setenv("HOME", home2)
	writeCodexRollout(t, home2, "rollout-a.jsonl", time.Now(), map[string]any{"kind": "other"})
	_, err := (&Codex{}).ResolveLastSession(snapRoot, time.Time{})
	if err == nil || errors.Is(err, ErrNoResumableSession) {
		t.Errorf("unreadable format must be a distinct error, got %v", err)
	}
}

func writeCursorChat(t *testing.T, home, group, chat string, m map[string]any) {
	t.Helper()
	dir := filepath.Join(home, ".cursor", "chats", group, chat)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCursorResolveLastSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := (&Cursor{}).ResolveLastSession(snapRoot, time.Time{}); !errors.Is(err, ErrNoResumableSession) {
		t.Errorf("no store: err = %v", err)
	}
	writeCursorChat(t, home, "g1", "chat-old", map[string]any{"cwd": snapRoot + "/aaaa", "updatedAtMs": 1000})
	writeCursorChat(t, home, "g2", "chat-new", map[string]any{"cwd": snapRoot + "/bbbb", "updatedAtMs": 5000})
	writeCursorChat(t, home, "g2", "chat-other", map[string]any{"cwd": "/work/project", "updatedAtMs": 9000})
	writeCursorChat(t, home, "g1", "chat-sibling", map[string]any{"cwd": snapRoot + "-other/x", "updatedAtMs": 9000})

	got, err := (&Cursor{}).ResolveLastSession(snapRoot, time.Time{})
	if err != nil || got != "chat-new" {
		t.Fatalf("got %q, %v; want chat-new", got, err)
	}
	if _, err := (&Cursor{}).ResolveLastSession("/nowhere/else", time.Time{}); !errors.Is(err, ErrNoResumableSession) {
		t.Errorf("none under root: err = %v", err)
	}
}

func TestCursorResolveLastSession_FormatChanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeCursorChat(t, home, "g1", "chat", map[string]any{"somethingElse": 1})
	_, err := (&Cursor{}).ResolveLastSession(snapRoot, time.Time{})
	if err == nil || errors.Is(err, ErrNoResumableSession) {
		t.Errorf("unreadable format must be a distinct error, got %v", err)
	}
}

func TestSnapshotSessionVendors(t *testing.T) {
	for _, a := range []Adapter{&Codex{}, &Cursor{}} {
		if s, ok := a.(SnapshotSessions); !ok || !s.SessionsKeyedBySnapshot() {
			t.Errorf("%s should resolve resume from the snapshots folder", a.Name())
		}
	}
	for _, a := range []Adapter{&Claude{}, &Copilot{}, &OpenCode{}} {
		if _, ok := a.(SnapshotSessions); ok {
			t.Errorf("%s must keep resolving by working directory", a.Name())
		}
	}
}
