// Package sessions records which vendor session belongs to which caller-chosen
// session name, so `ynh run --resume --session-name <name>` continues that
// caller's session and not whatever ran last in the directory (#601).
//
// One small JSON file per (harness, directory, name) key lives under
// $YNH_HOME/sessions. Launches for different keys touch different files, so
// concurrent launches never interfere.
package sessions

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eyelock/ynh/internal/config"
)

// Binding is the record for one key. Only SessionID is read back; the other
// fields are there so a person can tell what a file is.
type Binding struct {
	SessionID string    `json:"session_id"`
	Name      string    `json:"name"`
	Cwd       string    `json:"cwd"`
	Harness   string    `json:"harness"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Dir is where the records live.
func Dir() string {
	return filepath.Join(config.HomeDir(), "sessions")
}

// Key derives the file stem for a harness, absolute directory and session
// name. The fields are length-prefixed so no pair of different keys can
// concatenate to the same bytes.
func Key(harness, cwd, name string) string {
	h := sha256.New()
	for _, f := range []string{harness, cwd, name} {
		_, _ = fmt.Fprintf(h, "%d:%s\n", len(f), f)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Load returns the binding for key, and whether there is one. A missing file
// is (zero, false, nil); an unreadable or malformed one is an error.
func Load(key string) (Binding, bool, error) {
	data, err := os.ReadFile(filepath.Join(Dir(), key+".json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Binding{}, false, nil
		}
		return Binding{}, false, fmt.Errorf("reading session record: %w", err)
	}
	var b Binding
	if err := json.Unmarshal(data, &b); err != nil {
		return Binding{}, false, fmt.Errorf("parsing session record: %w", err)
	}
	return b, b.SessionID != "", nil
}

// Save writes the binding for key atomically: a temp file in the same
// directory, then a rename, so a reader never sees a half-written record.
func Save(key string, b Binding) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating sessions dir: %w", err)
	}
	b.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding session record: %w", err)
	}
	tmp, err := os.CreateTemp(dir, key+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing session record: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing session record: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, key+".json")); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("writing session record: %w", err)
	}
	return nil
}

// NewID returns a random version 4 UUID, the form Claude Code's --session-id
// requires.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating session id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}
