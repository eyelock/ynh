package vendor

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// codexSkipGitRepoCheck lets `codex exec` start outside a git repository.
// ynh's run directory is not one, and codex-cli 0.166.0 refuses an untrusted
// directory that is not ("Not inside a trusted directory and
// --skip-git-repo-check was not specified"). The interactive `codex` and
// `codex resume` do not take the flag.
const codexSkipGitRepoCheck = "--skip-git-repo-check"

// codexAgentNameKey is what a name must match to be a key in a -c override.
// Codex splits an override's key on every '.', with no handling of quotes
// (codex-rs/config/src/overrides.rs:18-22 at rust-v0.166.0), and splits key
// from value on the first '=', so a name is kept to letters, digits, '-' and
// '_'.
var codexAgentNameKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// CodexAgentArgs returns the config overrides that register, for one launch,
// every custom agent ynh wrote in runDir's .codex/agents/: a harness's own
// agents and its delegates. For each it passes
//
//	-c agents.<name>.config_file="<absolute path to its TOML>"
//	-c agents.<name>.description=<its description>
//
// Codex registers agents only from enabled config layers, and a project's
// .codex/ layer is enabled only when trusted, but the layer the -c overrides
// form is always enabled (codex-rs/agent-roles/src/loader.rs:39-72 and
// codex-rs/config/src/loader/mod.rs:426-431 at rust-v0.166.0). So the agents
// register whatever Codex's working directory is and whether or not anything
// is trusted, and nothing is written to the user's Codex config. Checked live
// on codex-cli 0.166.0 (ynw-89g): a spawn_agent call with the agent as its
// agent_type succeeds.
//
// The description is the file's own TOML string, passed as written. A name
// that cannot be a -c key (one with a '.', for instance) is not registered,
// and a warning says so. A missing directory registers nothing.
func CodexAgentArgs(runDir string) (args []string, warnings []string) {
	if runDir == "" {
		return nil, nil
	}
	dir := filepath.Join(runDir, ".codex", "agents")
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil || len(files) == 0 {
		return nil, nil
	}
	sort.Strings(files)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Codex agent file %s: %v", file, err))
			continue
		}
		nameLit, descLit := codexAgentHeader(string(data))
		name, ok := unquoteSimpleName(nameLit)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("agent %s: Codex cannot register it, since its name must use only letters, digits, '-' and '_' to be passed with -c (a '.' would split the key); it is still written to %s", nameLit, file))
			continue
		}
		args = append(args, "-c", "agents."+name+".config_file="+tomlString(file))
		if descLit != "" {
			args = append(args, "-c", "agents."+name+".description="+descLit)
		}
	}
	return args, warnings
}

// codexAgentHeader returns the raw TOML values of the name and description
// lines codexAgentTOML writes first, before the multi-line instructions.
func codexAgentHeader(toml string) (name, description string) {
	for _, line := range strings.Split(toml, "\n") {
		switch {
		case strings.HasPrefix(line, "name = "):
			name = strings.TrimPrefix(line, "name = ")
		case strings.HasPrefix(line, "description = "):
			description = strings.TrimPrefix(line, "description = ")
		case strings.HasPrefix(line, "developer_instructions"):
			return name, description
		}
	}
	return name, description
}

// unquoteSimpleName returns the name inside a TOML basic string literal when
// it can be a -c key, so needs no escaping.
func unquoteSimpleName(lit string) (string, bool) {
	if len(lit) < 2 || lit[0] != '"' || lit[len(lit)-1] != '"' {
		return "", false
	}
	name := lit[1 : len(lit)-1]
	return name, codexAgentNameKey.MatchString(name)
}
