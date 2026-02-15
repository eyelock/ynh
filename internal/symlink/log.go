package symlink

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/vendor"
)

const logFile = "symlinks.json"

// Installation records a single symlink install operation.
type Installation struct {
	Persona   string                `json:"persona"`
	Vendor    string                `json:"vendor"`
	Project   string                `json:"project"`
	Timestamp time.Time             `json:"timestamp"`
	Symlinks  []vendor.SymlinkEntry `json:"symlinks"`
}

// Log is the persistent symlink transaction log.
type Log struct {
	Installations []Installation `json:"installations"`
}

// LogPath returns the path to the symlink transaction log.
func LogPath() string {
	return filepath.Join(config.HomeDir(), logFile)
}

// LoadLog reads the transaction log from disk.
// Returns an empty log if the file doesn't exist.
func LoadLog() (*Log, error) {
	data, err := os.ReadFile(LogPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Log{}, nil
		}
		return nil, err
	}

	var log Log
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, err
	}
	return &log, nil
}

// Save writes the transaction log to disk.
func (l *Log) Save() error {
	if err := os.MkdirAll(filepath.Dir(LogPath()), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(LogPath(), data, 0o644)
}

// Record adds or updates an installation entry in the log.
func (l *Log) Record(persona, vendorName, projectDir string, entries []vendor.SymlinkEntry) {
	// Update existing entry if present (upsert)
	for i, inst := range l.Installations {
		if inst.Persona == persona && inst.Vendor == vendorName && inst.Project == projectDir {
			l.Installations[i].Timestamp = time.Now()
			l.Installations[i].Symlinks = entries
			return
		}
	}
	l.Installations = append(l.Installations, Installation{
		Persona:   persona,
		Vendor:    vendorName,
		Project:   projectDir,
		Timestamp: time.Now(),
		Symlinks:  entries,
	})
}

// FindInstallation returns the most recent installation matching the criteria.
// Returns nil if not found.
func (l *Log) FindInstallation(persona, vendorName, projectDir string) *Installation {
	for i := len(l.Installations) - 1; i >= 0; i-- {
		inst := &l.Installations[i]
		if inst.Persona == persona && inst.Vendor == vendorName && inst.Project == projectDir {
			return inst
		}
	}
	return nil
}

// RemoveInstallation removes the first matching installation entry.
func (l *Log) RemoveInstallation(persona, vendorName, projectDir string) {
	for i, inst := range l.Installations {
		if inst.Persona == persona && inst.Vendor == vendorName && inst.Project == projectDir {
			l.Installations = append(l.Installations[:i], l.Installations[i+1:]...)
			return
		}
	}
}

// Prune returns installations whose symlinks are all broken (target or link missing).
func (l *Log) Prune() []Installation {
	var orphans []Installation
	for _, inst := range l.Installations {
		if len(inst.Symlinks) == 0 {
			orphans = append(orphans, inst)
			continue
		}
		allBroken := true
		for _, entry := range inst.Symlinks {
			info, err := os.Lstat(entry.Link)
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				allBroken = false
				break
			}
		}
		if allBroken {
			orphans = append(orphans, inst)
		}
	}
	return orphans
}

// RemoveOrphans removes all orphaned installations from the log.
func (l *Log) RemoveOrphans(orphans []Installation) {
	keep := make([]Installation, 0, len(l.Installations))
	orphanSet := make(map[string]bool)
	for _, o := range orphans {
		key := o.Persona + "\x00" + o.Vendor + "\x00" + o.Project
		orphanSet[key] = true
	}
	for _, inst := range l.Installations {
		key := inst.Persona + "\x00" + inst.Vendor + "\x00" + inst.Project
		if !orphanSet[key] {
			keep = append(keep, inst)
		}
	}
	l.Installations = keep
}
