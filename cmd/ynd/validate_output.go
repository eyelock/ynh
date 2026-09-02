// `ynd validate-output --schema <name|path>` reads a JSON document from stdin
// and validates it against a published CLI schema by name, or against any
// JSON Schema file by path. Lets harness authors and downstream consumers
// verify captured responses against a contract without running their own
// schema loader.
//
// The path form exists so a project can gate its own published schemas the
// way ynh gates its own: pipe a command's JSON into this, declare it as a
// sensor, and drift between a schema and the thing it describes fails the
// gate instead of being noticed months later. YAML is deliberately not
// handled here — `yq -o=json` converts before anything reaches a validator,
// which keeps ynh out of the business of choosing a YAML dialect.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/clischema"
	"github.com/eyelock/ynh/internal/jsonschema"
)

func cmdValidateOutput(args []string) error {
	return cmdValidateOutputTo(args, os.Stdin, os.Stdout, os.Stderr)
}

func cmdValidateOutputTo(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	var schemaName string
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--schema":
			if i+1 >= len(args) {
				return fmt.Errorf("--schema requires a value")
			}
			i++
			schemaName = args[i]
		case "-h", "--help":
			return errHelp
		default:
			return fmt.Errorf("unknown argument: %s", args[i])
		}
		i++
	}
	if schemaName == "" {
		return fmt.Errorf("usage: ynd validate-output --schema <name|path> < some.json")
	}

	schema, err := resolveSchema(schemaName)
	if err != nil {
		return err
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("input is not valid JSON: %w", err)
	}
	if err := schema.Validate(v); err != nil {
		_, _ = fmt.Fprintf(stderr, "validation failed: %v\n", err)
		return fmt.Errorf("validation failed")
	}
	_, _ = fmt.Fprintln(stdout, "ok")
	return nil
}

// resolveSchema accepts a published schema name or a path to a JSON Schema
// file.
//
// Names win over paths. A published name is a fixed, documented set, so
// resolving it first means adding a schema to ynh can never change what an
// existing invocation validates against — whereas letting a file called
// "check" in the working directory shadow the published "check" schema would
// do exactly that, silently.
func resolveSchema(nameOrPath string) (*jsonschema.Schema, error) {
	if s, err := clischema.Get(nameOrPath); err == nil {
		return s, nil
	}
	if _, statErr := os.Stat(nameOrPath); statErr != nil {
		// Neither, so say so in terms of both, naming what is available. A
		// bare typo and a wrong path fail identically otherwise.
		return nil, fmt.Errorf(
			"schema %q: not a published schema name and not a readable file\n"+
				"published names: %s", nameOrPath, strings.Join(clischema.Names(), ", "))
	}
	data, err := os.ReadFile(nameOrPath)
	if err != nil {
		return nil, fmt.Errorf("schema file %q: %w", nameOrPath, err)
	}
	// Compiled through the same validator ynh uses on its own schemas rather
	// than a second implementation with different behaviour.
	c := jsonschema.NewCompiler()

	// A published schema set cross-references itself: ynh's own
	// docs/schema/cli/list.schema.json points at ../shared/harness.schema.json.
	// Registering only the named file leaves those refs unresolvable, so
	// register the whole set first. The embedded loader walks its tree for
	// exactly this reason.
	//
	// The set is taken to be the tree rooted at the parent of the schema's
	// own directory, which is what cli/ + shared/ beside each other requires.
	// Registering more than is needed costs nothing; registering less makes a
	// cross-referencing schema fail to compile at all.
	registerSiblingSchemas(c, nameOrPath)

	if err := c.Add(nameOrPath, data); err != nil {
		return nil, fmt.Errorf("schema file %q: %w", nameOrPath, err)
	}
	// Add keys by $id when the document has one, so compile under that and
	// fall back to the path. A schema with an $id is the common case for a
	// published contract, which is exactly what this form is for.
	key := nameOrPath
	var probe struct {
		ID string `json:"$id"`
	}
	if json.Unmarshal(data, &probe) == nil && probe.ID != "" {
		key = probe.ID
	}
	sch, err := c.Compile(key)
	if err != nil {
		return nil, fmt.Errorf("schema file %q: %w", nameOrPath, err)
	}
	return sch, nil
}

// registerSiblingSchemas adds every *.schema.json in the schema set alongside
// the one requested, so internal $refs resolve.
//
// Failures are deliberately ignored: a malformed neighbour must not stop the
// schema the caller actually asked for from compiling, and if the ref it needed
// was in that neighbour, compilation reports the unresolved ref itself, which
// is the more useful error.
func registerSiblingSchemas(c *jsonschema.Compiler, schemaPath string) {
	root := filepath.Dir(filepath.Dir(schemaPath))
	if root == "" || root == "." {
		root = filepath.Dir(schemaPath)
	}
	const maxSiblings = 500
	seen := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || seen >= maxSiblings {
			return nil //nolint:nilerr // a neighbour we cannot read is not this caller's problem
		}
		if !strings.HasSuffix(path, ".schema.json") || path == schemaPath {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if addErr := c.Add(path, data); addErr == nil {
			seen++
		}
		return nil
	})
}
