package symlink

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/vendor"
)

// Installation records a single symlink install operation.
type Installation struct {
	Harness   string                `json:"harness"`
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
// Delegates to config.SymlinksPath so every ynh-resolved path has a single home.
func LogPath() string {
	return config.SymlinksPath()
}

// LoadLog reads the transaction log from disk.
// Returns an empty log if the file doesn't exist.
func LoadLog() (*Log, error) {
	data, err := os.ReadFile(LogPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Log{}, nil
		}
		return nil, fmt.Errorf("reading symlink log: %w", err)
	}

	var log Log
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, fmt.Errorf("parsing symlink log: %w", err)
	}
	return &log, nil
}

// Save writes the transaction log to disk.
func (l *Log) Save() error {
	if err := os.MkdirAll(filepath.Dir(LogPath()), 0o755); err != nil {
		return fmt.Errorf("creating symlink log directory: %w", err)
	}

	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling symlink log: %w", err)
	}

	if err := os.WriteFile(LogPath(), data, 0o644); err != nil {
		return fmt.Errorf("writing symlink log: %w", err)
	}
	return nil
}

// Record adds or updates an installation entry in the log.
func (l *Log) Record(harnessName, vendorName, projectDir string, entries []vendor.SymlinkEntry) {
	// Update existing entry if present (upsert)
	for i, inst := range l.Installations {
		if inst.Harness == harnessName && inst.Vendor == vendorName && inst.Project == projectDir {
			l.Installations[i].Timestamp = time.Now()
			l.Installations[i].Symlinks = entries
			return
		}
	}
	l.Installations = append(l.Installations, Installation{
		Harness:   harnessName,
		Vendor:    vendorName,
		Project:   projectDir,
		Timestamp: time.Now(),
		Symlinks:  entries,
	})
}

// FindInstallation returns the most recent installation matching the criteria.
// Returns nil if not found.
func (l *Log) FindInstallation(harnessName, vendorName, projectDir string) *Installation {
	for i := len(l.Installations) - 1; i >= 0; i-- {
		inst := &l.Installations[i]
		if inst.Harness == harnessName && inst.Vendor == vendorName && inst.Project == projectDir {
			return inst
		}
	}
	return nil
}

// RemoveInstallation removes the first matching installation entry.
func (l *Log) RemoveInstallation(harnessName, vendorName, projectDir string) {
	for i, inst := range l.Installations {
		if inst.Harness == harnessName && inst.Vendor == vendorName && inst.Project == projectDir {
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
		key := o.Harness + "\x00" + o.Vendor + "\x00" + o.Project
		orphanSet[key] = true
	}
	for _, inst := range l.Installations {
		key := inst.Harness + "\x00" + inst.Vendor + "\x00" + inst.Project
		if !orphanSet[key] {
			keep = append(keep, inst)
		}
	}
	l.Installations = keep
}
