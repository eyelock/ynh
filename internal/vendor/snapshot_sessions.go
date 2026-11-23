package vendor

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SnapshotSessions is an optional capability of the vendors that start in the
// harness's snapshot and keep their own session store, keyed by the directory
// a session started in: Codex and Cursor.
//
// A snapshot is a new folder whenever the harness changes, so "the last
// session in this directory" means nothing to them. Instead ynh finds the
// newest session that started in any snapshot of the harness and resumes it by
// id, from the newest snapshot (so the resumed session reads the harness as it
// is now, as Claude's does). Neither store is documented: both formats are
// pinned by tests on fixtures, and a lookup that cannot read a store is an
// error, never a fall back to the vendor's own "last session".
//
// ResolveLastSession, called with the harness's snapshots folder
// (<run>/.snapshots/<name>), returns the newest session whose recorded working
// directory is that folder or inside it.
type SnapshotSessions interface {
	SessionsKeyedBySnapshot() bool
}

// sessionStartedUnder reports whether a session recorded as started in cwd
// belongs under root: the same folder, or one inside it. Both are compared as
// recorded and with symlinks resolved (/var and /private/var on macOS).
func sessionStartedUnder(cwd, root string) bool {
	if cwd == "" || root == "" {
		return false
	}
	roots := dirCandidates(root)
	for _, c := range dirCandidates(cwd) {
		for _, r := range roots {
			if c == r || strings.HasPrefix(c, strings.TrimSuffix(r, string(filepath.Separator))+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// SessionsKeyedBySnapshot marks a vendor that resolves a resume from the
// harness's snapshots folder (see SnapshotSessions).
func (c *Codex) SessionsKeyedBySnapshot() bool { return true }

// SessionsKeyedBySnapshot marks a vendor that resolves a resume from the
// harness's snapshots folder (see SnapshotSessions).
func (c *Cursor) SessionsKeyedBySnapshot() bool { return true }

// codexHomeDir is where Codex keeps its state: $CODEX_HOME, else ~/.codex.
func codexHomeDir() (string, error) {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d, nil
	}
	home, err := vendorHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// maxFirstLine bounds the first line read of a Codex rollout, which carries
// the session's base instructions.
const maxFirstLine = 16 << 20

// codexSessionMeta is what ynh reads of the first line of a Codex rollout
// file (~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl), seen with codex-cli
// 0.166.0: {"type":"session_meta","payload":{"id":...,"cwd":...}}. Undocumented.
type codexSessionMeta struct {
	Type    string `json:"type"`
	Payload struct {
		ID  string `json:"id"`
		Cwd string `json:"cwd"`
	} `json:"payload"`
}

// ResolveLastSession returns the newest Codex session that started in root or
// inside it. root is the harness's snapshots folder, not a project directory
// (see SnapshotSessions). "Newest" is the rollout file's modification time.
func (c *Codex) ResolveLastSession(root string, notBefore time.Time) (string, error) {
	home, err := codexHomeDir()
	if err != nil {
		return "", err
	}
	sessions := filepath.Join(home, "sessions")
	if _, err := os.Stat(sessions); err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoResumableSession
		}
		return "", fmt.Errorf("reading Codex sessions: %w", err)
	}

	type rollout struct {
		path string
		mod  time.Time
	}
	var files []rollout
	walkErr := filepath.WalkDir(sessions, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // one unreadable entry is not worth failing for
		}
		if d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || filepath.Ext(d.Name()) != ".jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !notBefore.IsZero() && info.ModTime().Before(notBefore) {
			return nil
		}
		files = append(files, rollout{p, info.ModTime()})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("reading Codex sessions: %w", walkErr)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })

	readable := false
	for _, f := range files {
		meta, ok := readCodexSessionMeta(f.path)
		if !ok {
			continue
		}
		readable = true
		if meta.Payload.ID != "" && sessionStartedUnder(meta.Payload.Cwd, root) {
			return meta.Payload.ID, nil
		}
	}
	if len(files) > 0 && !readable {
		return "", errors.New("no Codex session file has a readable session_meta line (the format may have changed)")
	}
	return "", ErrNoResumableSession
}

// readCodexSessionMeta reads the session_meta first line of a rollout file.
func readCodexSessionMeta(path string) (codexSessionMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return codexSessionMeta{}, false
	}
	defer func() { _ = f.Close() }()
	line, err := bufio.NewReaderSize(io.LimitReader(f, maxFirstLine), 64<<10).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return codexSessionMeta{}, false
	}
	var m codexSessionMeta
	if json.Unmarshal(line, &m) != nil || m.Type != "session_meta" {
		return codexSessionMeta{}, false
	}
	return m, true
}

// cursorChatMeta is what ynh reads of ~/.cursor/chats/<hash>/<chatId>/meta.json,
// seen with cursor-agent 2026.11.05. Undocumented; the chat id is the folder's
// name.
type cursorChatMeta struct {
	Cwd         string `json:"cwd"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
}

// ResolveLastSession returns the newest Cursor chat that started in root or
// inside it. root is the harness's snapshots folder, not a project directory
// (see SnapshotSessions). "Newest" is the chat's updatedAtMs.
func (c *Cursor) ResolveLastSession(root string, notBefore time.Time) (string, error) {
	home, err := vendorHomeDir()
	if err != nil {
		return "", err
	}
	chats := filepath.Join(home, ".cursor", "chats")
	groups, err := os.ReadDir(chats)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNoResumableSession
		}
		return "", fmt.Errorf("reading Cursor chats: %w", err)
	}

	var candidates []sessionCandidate
	readable := false
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		chatDirs, err := os.ReadDir(filepath.Join(chats, g.Name()))
		if err != nil {
			continue
		}
		for _, cd := range chatDirs {
			data, err := os.ReadFile(filepath.Join(chats, g.Name(), cd.Name(), "meta.json"))
			if err != nil {
				continue
			}
			var m cursorChatMeta
			if json.Unmarshal(data, &m) != nil || m.Cwd == "" {
				continue
			}
			readable = true
			if sessionStartedUnder(m.Cwd, root) {
				candidates = append(candidates, sessionCandidate{id: cd.Name(), modTime: time.UnixMilli(m.UpdatedAtMs)})
			}
		}
	}
	if len(candidates) == 0 && !readable && len(groups) > 0 {
		// Chats exist, but none has the meta ynh reads.
		for _, g := range groups {
			if g.IsDir() {
				return "", errors.New("no Cursor chat has a readable meta.json with a cwd (the format may have changed)")
			}
		}
	}
	return newestCandidate(candidates, notBefore)
}
