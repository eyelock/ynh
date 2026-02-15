package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/plugin"
)

// validName matches safe persona names: alphanumeric, hyphens, underscores, dots.
// Must start with a letter or digit. Prevents path traversal and shell injection.
var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// GitSource holds the common fields for any Git-backed reference.
type GitSource struct {
	Git  string
	Ref  string
	Path string
}

type Include struct {
	GitSource
	Pick []string
}

type Delegate struct {
	GitSource
}

type Persona struct {
	Name          string
	DefaultVendor string
	Includes      []Include
	DelegatesTo   []Delegate
}

// DetectFormat returns "plugin" if dir contains .claude-plugin/plugin.json,
// or "" if not found.
func DetectFormat(dir string) string {
	if plugin.IsPluginDir(dir) {
		return "plugin"
	}
	return ""
}

func Load(name string) (*Persona, error) {
	installDir := InstalledDir(name)
	if DetectFormat(installDir) != "plugin" {
		return nil, fmt.Errorf("persona %q: no .claude-plugin/plugin.json found", name)
	}
	return LoadPluginDir(installDir)
}

func List() ([]string, error) {
	dir := config.PersonasDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			if DetectFormat(subDir) == "plugin" {
				names = append(names, entry.Name())
			}
		}
	}

	return names, nil
}

func InstalledDir(name string) string {
	return filepath.Join(config.PersonasDir(), name)
}

// LoadPluginDir loads a persona from a plugin-format directory.
func LoadPluginDir(dir string) (*Persona, error) {
	pj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		return nil, err
	}

	if !validName.MatchString(pj.Name) {
		return nil, fmt.Errorf("invalid persona name %q: must match %s", pj.Name, validName.String())
	}

	p := &Persona{Name: pj.Name}

	meta, err := plugin.LoadMetadataJSON(dir)
	if err != nil {
		return nil, err
	}
	if meta != nil && meta.YNH != nil {
		p.DefaultVendor = meta.YNH.DefaultVendor
		for _, inc := range meta.YNH.Includes {
			p.Includes = append(p.Includes, Include{
				GitSource: GitSource{Git: inc.Git, Ref: inc.Ref, Path: inc.Path},
				Pick:      inc.Pick,
			})
		}
		for _, del := range meta.YNH.DelegatesTo {
			p.DelegatesTo = append(p.DelegatesTo, Delegate{
				GitSource: GitSource{Git: del.Git, Ref: del.Ref, Path: del.Path},
			})
		}
	}

	return p, nil
}
