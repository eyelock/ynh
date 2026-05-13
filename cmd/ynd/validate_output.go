// `ynd validate-output --schema <name>` reads a JSON document from stdin
// and validates it against the named published CLI schema. Lets harness
// authors and downstream consumers verify captured ynh responses against
// the contract without running their own schema loader.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/eyelock/ynh/internal/clischema"
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
		return fmt.Errorf("usage: ynd validate-output --schema <name> < some.json")
	}

	schema, err := clischema.Get(schemaName)
	if err != nil {
		return fmt.Errorf("schema %q: %w", schemaName, err)
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
