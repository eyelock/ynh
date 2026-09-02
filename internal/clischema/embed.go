// Package clischema embeds the published JSON schemas for ynh CLI output and
// exposes a compiled validator over them. Schemas are authored under
// internal/cliSchema/schema/{cli,shared}/ (embedded by this package) and
// mirrored to docs/schema/{cli,shared}/ for public discoverability; a parity
// test asserts the two trees are byte-identical.
//
// The plan's drift detection is wired here: tests round-trip a captured
// golden through the relevant schema, and the compile-on-load step rejects
// unsupported keywords so authors cannot write contracts the validator does
// not actually check.
package clischema

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	schema "github.com/eyelock/ynh/docs/schema"
	"github.com/eyelock/ynh/internal/jsonschema"
)

// schemaFS is the single schema tree, embedded once under docs/schema and
// shared from there. This package deliberately holds no copy of its own.
var schemaFS = schema.FS

var (
	compileOnce sync.Once
	compiled    map[string]*jsonschema.Schema
	compileErr  error
)

// Get returns the compiled per-command schema. The name is the stem of the
// CLI schema file (no .schema.json suffix), e.g. "version", "list", "info",
// "error".
func Get(name string) (*jsonschema.Schema, error) {
	compileOnce.Do(loadAll)
	if compileErr != nil {
		return nil, compileErr
	}
	s, ok := compiled[name]
	if !ok {
		return nil, fmt.Errorf("unknown schema %q", name)
	}
	return s, nil
}

// Names returns every CLI schema name available.
func Names() []string {
	compileOnce.Do(loadAll)
	out := make([]string, 0, len(compiled))
	for n := range compiled {
		out = append(out, n)
	}
	return out
}

// Raw returns the unparsed schema JSON bytes for a named schema. Used by
// `ynh schema <name>`.
//
// A bare name is a CLI response schema — `version`, `check` — which is the
// common case and stays unqualified. A name carrying a family prefix is taken
// as a path, so `agent/trajectory` resolves.
//
// The prefix form exists because `AllRaw` walks the whole tree and therefore
// advertises names this could not fetch: `ynh schema --all` listed
// `agent/trajectory` while `ynh schema agent/trajectory` answered "unknown
// schema". A listing that names something unfetchable is worse than not listing
// it.
func Raw(name string) ([]byte, error) {
	if strings.Contains(name, "/") {
		return schemaFS.ReadFile(name + ".schema.json")
	}
	if data, err := schemaFS.ReadFile("cli/" + name + ".schema.json"); err == nil {
		return data, nil
	}
	// The author-facing schemas — plugin, marketplace, harness — sit at the
	// tree root. `AllRaw` has always listed them and this has never fetched
	// them, so `ynh schema plugin` answered "unknown schema" for a schema the
	// same binary was advertising.
	return schemaFS.ReadFile(name + ".schema.json")
}

// RawAuthored returns the unparsed bytes for an author-facing schema — the
// ones describing files a harness author writes (plugin.json,
// marketplace.json), as opposed to the CLI response schemas under cli/.
//
// These live in the same embed tree so the repo has exactly one embedded
// copy of every schema. They are deliberately not compiled by this package:
// ynd validates them with a full JSON Schema implementation, while this
// package's compiler targets the narrower CLI response shapes.
func RawAuthored(name string) ([]byte, error) {
	return schemaFS.ReadFile(name + ".schema.json")
}

// AllRaw returns every embedded schema keyed by canonical path
// (e.g. "cli/version", "shared/envelope"). Used by `ynh schema --all`.
func AllRaw() (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(schemaFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".schema.json") {
			return nil
		}
		data, rerr := schemaFS.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		name := strings.TrimSuffix(path, ".schema.json")
		out[name] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func loadAll() {
	c := jsonschema.NewCompiler()
	compiled = map[string]*jsonschema.Schema{}

	type entry struct {
		path  string
		url   string
		name  string
		isCLI bool
	}
	var entries []entry

	err := fs.WalkDir(schemaFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".schema.json") {
			return nil
		}
		// Author-facing schemas (plugin, marketplace) sit at the tree root
		// and are served raw via RawAuthored; only the CLI response schemas
		// and their shared defs are compiled here.
		if !strings.HasPrefix(path, "cli/") && !strings.HasPrefix(path, "shared/") {
			return nil
		}
		data, rerr := schemaFS.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		url, idErr := extractID(data)
		if idErr != nil {
			return fmt.Errorf("%s: %w", path, idErr)
		}
		if addErr := c.Add(url, data); addErr != nil {
			return fmt.Errorf("%s: %w", path, addErr)
		}
		stem := strings.TrimSuffix(d.Name(), ".schema.json")
		isCLI := strings.HasPrefix(path, "cli/")
		entries = append(entries, entry{path: path, url: url, name: stem, isCLI: isCLI})
		return nil
	})
	if err != nil {
		compileErr = err
		return
	}

	for _, e := range entries {
		if !e.isCLI {
			continue
		}
		s, err := c.Compile(e.url)
		if err != nil {
			compileErr = fmt.Errorf("compile %s: %w", e.path, err)
			return
		}
		compiled[e.name] = s
	}
}

func extractID(data []byte) (string, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	id, _ := raw["$id"].(string)
	if id == "" {
		return "", fmt.Errorf("schema has no $id")
	}
	return id, nil
}
