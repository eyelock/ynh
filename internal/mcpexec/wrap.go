package mcpexec

import (
	"maps"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

// Launcher is the program that starts a wrapped server. It is found through
// PATH, like every other tool of the family.
const Launcher = "ynh"

// References reports whether any field of s that a vendor passes to the
// server on its command line or in its environment holds a ${VAR}.
func References(s plugin.MCPServer) bool {
	return len(Names(s)) > 0
}

// Names returns the variables s references in its command, args, env and
// headers, sorted and without repeats. cwd and url are not counted: the
// launcher cannot expand either.
func Names(s plugin.MCPServer) []string {
	set := make(map[string]bool)
	add := func(v string) {
		for _, n := range plugin.EnvRefNames(v) {
			set[n] = true
		}
	}
	add(s.Command)
	for _, a := range s.Args {
		add(a)
	}
	for _, v := range s.Env {
		add(v)
	}
	for _, v := range s.Headers {
		add(v)
	}
	return slices.Sorted(maps.Keys(set))
}

// WrapStdio returns s started through the launcher: the command becomes
// `ynh mcp-exec --env-file <envFile> -- <command> <args...>`. The args and
// the env keep their ${VAR} literal, because the launcher expands them from
// the env file.
func WrapStdio(s plugin.MCPServer, envFile string) plugin.MCPServer {
	args := append([]string{"mcp-exec", "--env-file", envFile, "--", s.Command}, s.Args...)
	s.Command = Launcher
	s.Args = args
	return s
}

// SplitHeaders separates the headers that reference a variable from those
// that do not. The caller passes the first to the launcher and the second to
// the vendor as they are.
func SplitHeaders(headers map[string]string) (templated, static map[string]string) {
	for name, v := range headers {
		if len(plugin.EnvRefNames(v)) > 0 {
			if templated == nil {
				templated = make(map[string]string)
			}
			templated[name] = v
			continue
		}
		if static == nil {
			static = make(map[string]string)
		}
		static[name] = v
	}
	return templated, static
}

// HeadersCommand is the shell command line a vendor runs to obtain the
// templated headers: `ynh mcp-headers --env-file <envFile> -- 'Name: value'
// ...`. It carries the templates, never the values.
func HeadersCommand(templated map[string]string, envFile string) string {
	parts := []string{Launcher, "mcp-headers", "--env-file", shellQuote(envFile), "--"}
	for _, name := range slices.Sorted(maps.Keys(templated)) {
		parts = append(parts, shellQuote(name+": "+templated[name]))
	}
	return strings.Join(parts, " ")
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
