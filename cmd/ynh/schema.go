// `ynh schema <name>` and `ynh schema --all --format json` expose the
// embedded JSON schemas for ynh's structured CLI output. Consumers that
// want to validate ynh responses at runtime fetch the relevant schema
// once via these commands rather than vendoring the docs/schema/ tree.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/clischema"
	"github.com/eyelock/ynh/internal/config"
)

func cmdSchema(args []string) error {
	return cmdSchemaTo(args, os.Stdout, os.Stderr)
}

func cmdSchemaTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	all := false
	format := "text"
	var name string
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--all":
			all = true
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			if name != "" {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			name = args[i]
		}
		i++
	}

	if all {
		if name != "" {
			return cliError(stderr, structured, errCodeInvalidInput,
				"--all and <name> are mutually exclusive")
		}
		return printSchemaManifest(stdout, format)
	}

	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh schema <name> | ynh schema --all --format json")
	}

	data, err := clischema.Raw(name)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound,
			fmt.Sprintf("unknown schema %q", name))
	}
	_, err = stdout.Write(data)
	return err
}

// schemaManifest is the JSON shape of `ynh schema --all --format json`.
// One round-trip for consumers (TermQ-style MCP servers, codegen tools)
// that want every schema at startup without forking N subprocesses.
type schemaManifest struct {
	Capabilities string                     `json:"capabilities"`
	YnhVersion   string                     `json:"ynh_version"`
	Schemas      map[string]json.RawMessage `json:"schemas"`
}

func printSchemaManifest(w io.Writer, format string) error {
	allRaw, err := clischema.AllRaw()
	if err != nil {
		return fmt.Errorf("loading schemas: %w", err)
	}
	if format == "text" {
		// Text manifest is just a sorted list of names so humans can grep.
		names := make([]string, 0, len(allRaw))
		for n := range allRaw {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			_, _ = fmt.Fprintln(w, n)
		}
		return nil
	}
	if format != "json" {
		return fmt.Errorf("invalid --format value %q (want text or json)", format)
	}
	m := schemaManifest{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		Schemas:      map[string]json.RawMessage{},
	}
	for name, data := range allRaw {
		m.Schemas[name] = json.RawMessage(data)
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}
