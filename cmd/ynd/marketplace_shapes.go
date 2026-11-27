package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

// Three files are called marketplace.json (ynh #922, #395):
//
//   - the registry index ynh reads, in the manifest directory
//     (.agents/harness/), validated against marketplace.schema.json;
//   - a vendor index `ynd marketplace build` writes for one vendor's own
//     marketplace system, in that vendor's directory (.claude-plugin/,
//     .cursor-plugin/, .github/plugin/, .agents/plugins/);
//   - the build config `ynd marketplace build` reads, anywhere else, which
//     lists "harnesses".
//
// classifyMarketplace tells them apart; a file that is none of them is
// refused rather than linted as whichever shape was assumed.
type marketplaceShape int

const (
	shapeUnknown marketplaceShape = iota
	shapeRegistry
	shapeVendorIndex
	shapeBuildConfig
)

func (s marketplaceShape) String() string {
	switch s {
	case shapeRegistry:
		return "registry index"
	case shapeVendorIndex:
		return "vendor index"
	case shapeBuildConfig:
		return "build config"
	}
	return "unknown"
}

// vendorIndexDirs are the directories vendors keep their marketplace index in
// (each vendor adapter's MarketplaceManifestDir).
var vendorIndexDirs = []string{".claude-plugin", ".cursor-plugin", ".github/plugin", ".agents/plugins"}

func inVendorIndexDir(path string) (string, bool) {
	parent := filepath.ToSlash(filepath.Dir(path))
	for _, d := range vendorIndexDirs {
		if parent == d || strings.HasSuffix(parent, "/"+d) {
			return d, true
		}
	}
	return "", false
}

// classifyMarketplace decides which shape the marketplace.json at path is:
// by its directory when that is one a shape owns, else by its keys (a
// "harnesses" list is a build config). Anything else is shapeUnknown.
func classifyMarketplace(path string) marketplaceShape {
	if plugin.InManifestDir(path) {
		return shapeRegistry
	}
	if _, ok := inVendorIndexDir(path); ok {
		return shapeVendorIndex
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return shapeUnknown
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(data, &keys) != nil {
		return shapeUnknown
	}
	if _, ok := keys["harnesses"]; ok {
		return shapeBuildConfig
	}
	return shapeUnknown
}

// lintMarketplaceFile validates a marketplace.json by its shape.
func lintMarketplaceFile(path string) []lintIssue {
	switch classifyMarketplace(path) {
	case shapeRegistry:
		return lintRegistryMarketplace(path)
	case shapeVendorIndex:
		return lintVendorIndex(path)
	case shapeBuildConfig:
		return lintMarketplaceConfig(path)
	}
	return []lintIssue{{File: path, Message: "cannot tell which marketplace.json this is: a registry index lives in .agents/harness/, a vendor index in .claude-plugin/, .cursor-plugin/, .github/plugin/ or .agents/plugins/, and a build config lists \"harnesses\"; move it or fix its shape"}}
}

// unsupportedCopilotSources are the plugin source types Copilot CLI's
// marketplace does not install from.
var unsupportedCopilotSources = []string{"git-subdir", "npm", "archive", "command"}

// lintVendorIndex checks a vendor index's shape: a name, and plugins each
// with a name and a source (a path string, or an object naming where it
// comes from). Under .github/plugin, a source of a type Copilot cannot install
// from is refused.
func lintVendorIndex(path string) []lintIssue {
	data, err := os.ReadFile(path)
	if err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("cannot read: %v", err)}}
	}
	var idx struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string          `json:"name"`
			Source json.RawMessage `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("vendor index: parsing: %v", err)}}
	}
	var issues []lintIssue
	add := func(format string, a ...any) {
		issues = append(issues, lintIssue{File: path, Message: "vendor index: " + fmt.Sprintf(format, a...)})
	}
	if idx.Name == "" {
		add("name is required")
	}
	if len(idx.Plugins) == 0 {
		add("plugins is required and must not be empty")
	}
	dir, _ := inVendorIndexDir(path)
	for i, p := range idx.Plugins {
		if p.Name == "" {
			add("plugin %d: name is required", i)
		}
		if len(p.Source) == 0 || string(p.Source) == "null" {
			add("plugin %d: source is required", i)
			continue
		}
		var obj map[string]any
		if json.Unmarshal(p.Source, &obj) != nil {
			continue // a path string
		}
		kind, _ := obj["source"].(string)
		if dir == ".github/plugin" {
			for _, bad := range unsupportedCopilotSources {
				if kind == bad {
					add("plugin %d (%s): source type %q is not supported in %s", i, p.Name, kind, dir)
				}
			}
		}
	}
	return issues
}
