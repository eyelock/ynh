package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

// The registry index (.agents/harness/marketplace.json) is also the build
// input (ynh #395, M1): `ynd marketplace build` writes each vendor's marketplace
// index from it, in place, with each entry's source mapped to that vendor's
// form and no content built. Harnesses and plugins both are listed; ynh search
// and ynh install read the harnesses only (M2).

// RegistryBuildOptions configures BuildFromRegistryIndex.
type RegistryBuildOptions struct {
	// OutputDir is the repository root the vendor indexes are written under
	// (.claude-plugin/, .cursor-plugin/, .github/plugin/, .agents/plugins/).
	OutputDir string
	// Vendors lists target vendors (default: every registered vendor).
	Vendors []string
}

// RegistryIndex is a registry index read for a build.
type RegistryIndex struct {
	Name, Description, OwnerName, OwnerEmail string
	Entries                                  []vendor.MarketplacePluginInfo
}

// LoadRegistryIndex reads and checks the registry index at path.
func LoadRegistryIndex(path string) (*RegistryIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading registry index: %w", err)
	}
	var mj plugin.MarketplaceJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return nil, fmt.Errorf("parsing registry index %s: %w", path, err)
	}
	if mj.Name == "" {
		return nil, fmt.Errorf("registry index %s: name is required", path)
	}
	if mj.Owner == nil || mj.Owner.Name == "" {
		return nil, fmt.Errorf("registry index %s: owner.name is required", path)
	}
	idx := &RegistryIndex{Name: mj.Name, OwnerName: mj.Owner.Name, OwnerEmail: mj.Owner.Email}
	root := ""
	if mj.Metadata != nil {
		idx.Description = mj.Metadata.Description
		root = mj.Metadata.HarnessRoot
	}
	seen := map[string]bool{}
	for _, list := range []struct {
		kind    string
		entries []plugin.HarnessEntry
	}{{"harnesses", mj.Harnesses}, {"plugins", mj.Plugins}} {
		for i := range list.entries {
			e := &list.entries[i]
			if e.Name == "" {
				return nil, fmt.Errorf("registry index %s: %s[%d]: name is required", path, list.kind, i)
			}
			if seen[e.Name] {
				return nil, fmt.Errorf("registry index %s: %q is listed twice", path, e.Name)
			}
			seen[e.Name] = true
			src, err := indexEntrySource(e, root)
			if err != nil {
				return nil, fmt.Errorf("registry index %s: %s entry %q: %w", path, list.kind, e.Name, err)
			}
			idx.Entries = append(idx.Entries, vendor.MarketplacePluginInfo{
				Name: e.Name, Description: e.Description, Version: e.Version, Source: src,
			})
		}
	}
	if len(idx.Entries) == 0 {
		return nil, fmt.Errorf("registry index %s: at least one harness or plugin is required", path)
	}
	return idx, nil
}

// indexEntrySource maps a registry source onto the vendor-neutral source.
func indexEntrySource(e *plugin.HarnessEntry, harnessRoot string) (*vendor.MarketplaceSource, error) {
	if p, ok := e.SourcePath(); ok {
		if !strings.HasPrefix(p, "./") {
			return nil, fmt.Errorf("source %q: a relative path must start with ./", p)
		}
		if harnessRoot != "" && !strings.HasPrefix(p, strings.TrimSuffix(harnessRoot, "/")+"/") {
			p = strings.TrimSuffix(harnessRoot, "/") + "/" + strings.TrimPrefix(p, "./")
		}
		return &vendor.MarketplaceSource{Type: "local", Path: p}, nil
	}
	r, ok := e.SourceRemote()
	if !ok {
		return nil, fmt.Errorf("source must be a ./ path or a github, url or git-subdir object")
	}
	switch r.Type {
	case "github":
		if r.Repo == "" {
			return nil, fmt.Errorf("a github source needs repo")
		}
	case "url":
		if r.URL == "" {
			return nil, fmt.Errorf("a url source needs url")
		}
	case "git-subdir":
		if r.URL == "" || r.Path == "" {
			return nil, fmt.Errorf("a git-subdir source needs url and path")
		}
	default:
		return nil, fmt.Errorf("source type %q is not github, url or git-subdir", r.Type)
	}
	return &vendor.MarketplaceSource{Type: r.Type, Repo: r.Repo, URL: r.URL, Path: r.Path, Ref: r.Ref, SHA: r.SHA}, nil
}

// BuildFromRegistryIndex writes each vendor's marketplace index from idx under
// opts.OutputDir. Every index is generated before any is written, so an entry a
// vendor cannot carry (a *vendor.UnsupportedSourceError, naming the entry and
// the vendor) leaves the directory as it was. It returns the files written,
// relative to OutputDir.
func BuildFromRegistryIndex(idx *RegistryIndex, opts RegistryBuildOptions) ([]string, error) {
	vendors := opts.Vendors
	if len(vendors) == 0 {
		vendors = vendor.Available()
	}
	cfg := vendor.MarketplaceIndexConfig{Name: idx.Name, Description: idx.Description, OwnerName: idx.OwnerName, OwnerEmail: idx.OwnerEmail}
	type out struct {
		rel  string
		data []byte
	}
	var files []out
	for _, v := range vendors {
		adapter, err := vendor.Get(v)
		if err != nil {
			return nil, err
		}
		data, err := adapter.GenerateMarketplaceIndex(cfg, idx.Entries)
		if err != nil {
			return nil, fmt.Errorf("%s: %w\n  Fix: build the other vendors with -v, or give the entry a source %s can carry", v, err, v)
		}
		if data == nil {
			continue
		}
		files = append(files, out{filepath.Join(adapter.MarketplaceManifestDir(), "marketplace.json"), data})
	}
	var written []string
	for _, f := range files {
		abs := filepath.Join(opts.OutputDir, f.rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return written, err
		}
		if err := os.WriteFile(abs, f.data, 0o644); err != nil {
			return written, err
		}
		written = append(written, f.rel)
	}
	return written, nil
}
