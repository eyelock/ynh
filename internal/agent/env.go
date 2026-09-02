package agent

import (
	"os"
	"sort"
	"strings"
)

// processEnvNames are the variables a subprocess needs in order to run at all.
//
// These are process mechanics, not configuration: without PATH the vendor
// binary cannot be found, without HOME it cannot locate its own credentials or
// config, and without TMPDIR it writes temporary files somewhere unexpected.
// A harness cannot be asked to declare them because a run would not start.
//
// Anything that is policy rather than mechanics stays out — proxy settings
// included, even though their absence is inconvenient behind a corporate
// proxy. A proxy URL can carry credentials, and inheriting it silently is the
// same default this exists to remove. A harness that needs one declares it.
var processEnvNames = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TZ",
	"LANG", "TERM",
}

// processEnvPrefixes are locale variables, which come as a family.
var processEnvPrefixes = []string{"LC_"}

// workerEnv builds the environment for an agent worker.
//
// It starts from the process minimum and adds exactly what the caller passes —
// never os.Environ(). The manifest contract is explicit that env_passthrough
// names "which variables reach an agent worker's process; empty means none",
// and docs/agent.md states the worker does not inherit the operator's
// environment. Both were true of the declaration and false of the process:
// every backend started from os.Environ() and appended the passthrough on top,
// which made the allowlist decorative.
//
// explicit is already the resolved passthrough (plus ynh's own YNH_AGENT_*
// variables) assembled by the loop, so this does not re-read the manifest.
func workerEnvFor(explicit []string) []string {
	out := make([]string, 0, len(processEnvNames)+len(explicit))
	for _, name := range processEnvNames {
		if v, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+v)
		}
	}
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		for _, p := range processEnvPrefixes {
			if strings.HasPrefix(name, p) {
				out = append(out, kv)
				break
			}
		}
	}
	// Explicit entries come last so a declared passthrough wins over the
	// process minimum: a harness that deliberately overrides HOME or TMPDIR
	// means it.
	out = append(out, explicit...)
	return out
}

// envNames lists the variable names in a KEY=VALUE slice, sorted.
//
// Names only, never values — this is what gets recorded on the trajectory so
// that a run failing for want of a variable is diagnosable without the record
// becoming the leak it is meant to prevent.
func envNames(env []string) []string {
	seen := map[string]bool{}
	for _, kv := range env {
		if name, _, ok := strings.Cut(kv, "="); ok && name != "" {
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
