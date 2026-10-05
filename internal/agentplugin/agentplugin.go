package agentplugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/eyelock/ynh/internal/plugin"
)

// Fixed locations (§6.1). The manifest cannot move them.
const (
	ManifestFile = "plugin.json"
	MCPFile      = "mcp.json"
	SkillsDir    = "skills"
	SkillFile    = "SKILL.md"
)

// Placeholders a client expands in MCP args, env values and cwd (§9.2), and
// the environment variables it provides to every plugin subprocess (§9.1).
const (
	PlaceholderRoot = plugin.MCPPlaceholderRoot
	PlaceholderData = plugin.MCPPlaceholderData
	EnvRoot         = plugin.MCPEnvRoot
	EnvData         = plugin.MCPEnvData
)

var schemaPrinter = message.NewPrinter(language.English)

// Manifest is the closed root plugin.json (§5.2). Extensions holds each
// client namespace's data undecoded: the specification assigns it no
// semantics beyond "an object", and a client that does not implement a
// namespace must not validate its contents (§8.1).
type Manifest struct {
	Schema      string                     `json:"$schema"`
	Name        string                     `json:"name"`
	Version     string                     `json:"version,omitempty"`
	Description string                     `json:"description,omitempty"`
	Author      *Author                    `json:"author,omitempty"`
	Homepage    string                     `json:"homepage,omitempty"`
	Repository  string                     `json:"repository,omitempty"`
	License     string                     `json:"license,omitempty"`
	Keywords    []string                   `json:"keywords,omitempty"`
	Extensions  map[string]json.RawMessage `json:"extensions,omitempty"`
}

// Author is the manifest's author object (§5.4). Every field is optional.
type Author struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// manifestFields is the closed set of top-level keys (§5.2). Anything else
// is reported and ignored rather than rejected.
var manifestFields = map[string]bool{
	"$schema": true, "name": true, "version": true, "description": true,
	"author": true, "homepage": true, "repository": true, "license": true,
	"keywords": true, "extensions": true,
}

// Diagnostic is something a client is required to report but not to fail
// on: an unknown manifest field, a skipped skill, a server entry it could not
// use. Path names the file or entry it is about, relative to the plugin root.
type Diagnostic struct {
	Path    string
	Message string
}

func (d Diagnostic) String() string {
	if d.Path == "" {
		return d.Message
	}
	return d.Path + ": " + d.Message
}

// ErrNotPlugin is returned when dir has no root plugin.json at all, as
// distinct from one that is present and invalid.
var ErrNotPlugin = errors.New("no plugin.json at the plugin root")

// IsPluginRoot reports whether dir carries a root plugin.json that declares
// the Agent Plugins 1.0.0 manifest schema. It decides format only; it does
// not validate. A ynh harness manifest shares the filename, so the $schema
// identifier, not the filename, is what makes a directory an Agent Plugin.
func IsPluginRoot(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return false
	}
	return IsManifest(data)
}

// IsManifest reports whether data is a JSON object whose $schema is the
// Agent Plugins 1.0.0 manifest identifier.
func IsManifest(data []byte) bool {
	var probe struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Schema == PluginSchemaID
}

// ReadManifest loads and validates the root plugin.json (§5).
//
// The non-fatal cases of §5.2 and §8.1 come back as diagnostics: each
// unknown top-level field, and an extensions value that is not an object,
// are reported and dropped. Every other violation is fatal and returns an
// error, in which case the caller must not discover or execute any
// component (§5.3, §11.3).
func ReadManifest(dir string) (*Manifest, []Diagnostic, error) {
	path := filepath.Join(dir, ManifestFile)
	if err := within(dir, path); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotPlugin
		}
		return nil, nil, fmt.Errorf("reading %s: %w", ManifestFile, err)
	}
	return ParseManifest(data)
}

// ParseManifest is ReadManifest over bytes already in hand.
func ParseManifest(data []byte) (*Manifest, []Diagnostic, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("%s: not a JSON object: %w", ManifestFile, err)
	}
	if raw == nil {
		return nil, nil, fmt.Errorf("%s: not a JSON object", ManifestFile)
	}

	// The version check comes before schema validation: an unsupported
	// version is its own rejection with its own message (§5.2), not a
	// const mismatch buried in a schema error.
	var schema string
	if s, ok := raw["$schema"]; !ok {
		return nil, nil, fmt.Errorf("%s: missing required field $schema", ManifestFile)
	} else if err := json.Unmarshal(s, &schema); err != nil || schema != PluginSchemaID {
		return nil, nil, fmt.Errorf("%s: unsupported Agent Plugins version: $schema must be %q", ManifestFile, PluginSchemaID)
	}

	var diags []Diagnostic
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !manifestFields[k] {
			diags = append(diags, Diagnostic{ManifestFile, fmt.Sprintf("unknown field %q ignored (client-specific data belongs under extensions)", k)})
			delete(raw, k)
		}
	}
	if ext, ok := raw["extensions"]; ok {
		if t := bytes.TrimSpace(ext); len(t) == 0 || t[0] != '{' {
			diags = append(diags, Diagnostic{ManifestFile, "extensions is not an object; ignored"})
			delete(raw, "extensions")
		}
	}

	pluginSchema, _, _, err := compiled()
	if err != nil {
		return nil, nil, err
	}
	cleaned, err := json.Marshal(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(cleaned))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	if err := pluginSchema.Validate(doc); err != nil {
		msgs := schemaErrors(err)
		return nil, nil, fmt.Errorf("%s: %s", ManifestFile, joinIssues(msgs))
	}

	var m Manifest
	if err := json.Unmarshal(cleaned, &m); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", ManifestFile, err)
	}
	return &m, diags, nil
}

func joinIssues(msgs []string) string {
	if len(msgs) == 1 {
		return msgs[0]
	}
	var b bytes.Buffer
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(m)
	}
	return b.String()
}

// within reports an error when path, after resolving symlinks, does not sit
// inside root after the same resolution (§4.1). A path that does not exist
// yet is checked by its nearest existing ancestor, which is what matters for
// containment.
func within(root, path string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolving plugin root: %w", err)
	}
	probe := path
	for {
		if _, err := os.Lstat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	real, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", path, err)
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("resolves outside the plugin root")
	}
	return nil
}
