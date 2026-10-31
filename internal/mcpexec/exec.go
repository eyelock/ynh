package mcpexec

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/eyelock/ynh/internal/plugin"
)

// Expand replaces each ${VAR} in s with its value in vars. A reference to a
// variable vars does not hold is an error naming it: the launcher reads only
// the file, never its own environment, so a server cannot reach a variable
// the harness did not declare.
func Expand(s string, vars map[string]string) (string, error) {
	return plugin.ExpandEnvRefs(s, func(name string) (string, error) {
		v, ok := vars[name]
		if !ok {
			return "", fmt.Errorf("${%s} is not in the env file", name)
		}
		return v, nil
	})
}

// ExpandEnviron expands ${VAR} in the value of each NAME=value entry, but
// only for variables vars holds. The launcher inherits the whole environment
// Claude Code gives the server, and an unrelated variable may carry a literal
// ${X} of its own; that is left as it is rather than failing the launch.
// ynh validated every reference a server declares before writing the file,
// so a declared reference is always in vars.
func ExpandEnviron(environ []string, vars map[string]string) ([]string, error) {
	out := make([]string, len(environ))
	for i, e := range environ {
		name, value, ok := strings.Cut(e, "=")
		if !ok || !strings.Contains(value, "${") {
			out[i] = e
			continue
		}
		v, err := plugin.ExpandEnvRefs(value, func(ref string) (string, error) {
			if v, ok := vars[ref]; ok {
				return v, nil
			}
			return "${" + ref + "}", nil
		})
		if err != nil {
			return nil, fmt.Errorf("environment variable %s: %w", name, err)
		}
		out[i] = name + "=" + v
	}
	return out, nil
}

// Plan is what `ynh mcp-exec` will run: the program, its argv and its
// environment, all expanded.
type Plan struct {
	Path string
	Argv []string
	Env  []string
}

// parseArgs splits `--env-file <path> -- rest...`.
func parseArgs(args []string, usage string) (envFile string, rest []string, err error) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			if envFile == "" {
				return "", nil, fmt.Errorf("--env-file is required; usage: %s", usage)
			}
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("nothing after --; usage: %s", usage)
			}
			return envFile, args[i+1:], nil
		case a == "--env-file" && i+1 < len(args):
			i++
			envFile = args[i]
		case strings.HasPrefix(a, "--env-file="):
			envFile = strings.TrimPrefix(a, "--env-file=")
		default:
			return "", nil, fmt.Errorf("unexpected argument %q; usage: %s", a, usage)
		}
	}
	return "", nil, fmt.Errorf("missing --; usage: %s", usage)
}

const execUsage = "ynh mcp-exec --env-file <path> -- <command> [args...]"

// Prepare reads the env file named in args (--env-file <path> -- <command>
// [args...]) and expands the command, its arguments and environ from it.
func Prepare(args, environ []string) (*Plan, error) {
	envFile, rest, err := parseArgs(args, execUsage)
	if err != nil {
		return nil, err
	}
	vars, err := readEnvFile(envFile)
	if err != nil {
		return nil, err
	}
	argv := make([]string, 0, len(rest))
	for _, a := range rest {
		v, err := Expand(a, vars)
		if err != nil {
			return nil, fmt.Errorf("argument %q: %w", a, err)
		}
		argv = append(argv, v)
	}
	env, err := ExpandEnviron(environ, vars)
	if err != nil {
		return nil, err
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, err
	}
	return &Plan{Path: path, Argv: argv, Env: env}, nil
}

func readEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading env file: %w", err)
	}
	vars, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("env file %s: %w", path, err)
	}
	return vars, nil
}

// Exec replaces this process with the planned command, so the server's stdin
// and stdout are the launcher's own: it is the MCP stdio channel.
func (p *Plan) Exec() error {
	return syscall.Exec(p.Path, p.Argv, p.Env)
}

const headersUsage = "ynh mcp-headers --env-file <path> -- 'Name: value' ..."

// Headers reads `--env-file <path> -- "Name: template" ...`, expands each
// template from the env file and writes the headers as one JSON object to
// out, which is what Claude Code's headersHelper reads from stdout.
func Headers(args []string, out io.Writer) error {
	envFile, rest, err := parseArgs(args, headersUsage)
	if err != nil {
		return err
	}
	vars, err := readEnvFile(envFile)
	if err != nil {
		return err
	}
	headers := make(map[string]string, len(rest))
	for _, h := range rest {
		name, template, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("header %q is not \"Name: value\"", h)
		}
		v, err := Expand(strings.TrimSpace(template), vars)
		if err != nil {
			return fmt.Errorf("header %s: %w", strings.TrimSpace(name), err)
		}
		headers[strings.TrimSpace(name)] = v
	}
	return json.NewEncoder(out).Encode(headers)
}
