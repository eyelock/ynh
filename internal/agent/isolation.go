package agent

import (
	"fmt"
	"io"

	"github.com/eyelock/ynh/internal/vendor"
)

// workerMCPIsolation returns what keeps the operator's own MCP servers out of
// a worker: launch arguments, or environment variables for a vendor whose
// isolation is one (OpenCode), the same adapter methods `ynh run` uses. A
// vendor that cannot isolate fully says so in one warning line on w. It is
// computed once per session, not per turn: each turn is its own process, and
// listing the vendor's servers costs one more.
//
// dir is the worker's worktree, whose project config the vendor also loads.
// workerEnv is the environment the worker will run with. The vendor is asked
// which servers it would load under it, not under ynh's own, which can differ
// (the worker's carries a minimum of ynh's, so a CODEX_HOME or XDG_CONFIG_HOME
// of the operator's is not in it unless the harness passes it).
func workerMCPIsolation(a vendor.Adapter, configPath string, workerEnv []string, dir string, w io.Writer) (args, env []string) {
	var warning string
	switch v := a.(type) {
	case *vendor.OpenCode:
		env, warning = v.IsolateMCPEnvIn(configPath, workerEnv, dir)
	case *vendor.Codex:
		args, warning = v.IsolateMCPIn(configPath, workerEnv, dir)
	default:
		args, warning = a.IsolateMCP(configPath)
	}
	if warning != "" && w != nil {
		_, _ = fmt.Fprintf(w, "warning: %s\n", warning)
	}
	return args, env
}

// withIsolationArgs inserts isolation arguments before the last n arguments
// of args (the positional tail: Codex's "-", OpenCode's "--" and prompt).
func withIsolationArgs(args, isolation []string, tail int) []string {
	if len(isolation) == 0 {
		return args
	}
	at := len(args) - tail
	out := make([]string, 0, len(args)+len(isolation))
	out = append(out, args[:at]...)
	out = append(out, isolation...)
	return append(out, args[at:]...)
}
