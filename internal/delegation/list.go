package delegation

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/eyelock/ynh/internal/namespace"
)

// List returns the records of parent's delegations started from folder, oldest
// first. A file that cannot be read as a record is skipped: one damaged record
// must not hide the others. The directory not existing is an empty list.
func List(parent, folder string) ([]*Record, error) {
	dir := filepath.Join(Dir(), namespace.IDToFSName(parent), FolderKey(folder))
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Record
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if r, err := Load(filepath.Join(dir, e.Name())); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
