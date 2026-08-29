package agent

import (
	"strings"
	"testing"
)

func envMap(t *testing.T, env []string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// The manifest contract says env_passthrough names "which variables reach an
// agent worker's process; empty means none", and docs/agent.md says the worker
// does not inherit the operator's environment. Both were true of the
// declaration and false of the process.
func TestWorkerEnv_DoesNotInheritTheOperatorsEnvironment(t *testing.T) {
	t.Setenv("MY_COMPANY_DEPLOY_TOKEN", "ghp_notarealtoken000")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "notarealsecret000000")
	t.Setenv("SOME_UNDECLARED_VAR", "value")

	got := envMap(t, workerEnvFor(nil))
	for _, leaked := range []string{"MY_COMPANY_DEPLOY_TOKEN", "AWS_SECRET_ACCESS_KEY", "SOME_UNDECLARED_VAR"} {
		if _, ok := got[leaked]; ok {
			t.Errorf("%s reached the worker without being declared", leaked)
		}
	}
}

// Without these a run cannot start: the vendor binary is not on PATH and
// cannot find its own credentials. They are process mechanics, not config.
func TestWorkerEnv_KeepsWhatAProcessNeedsToRun(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", "/Users/dev")
	t.Setenv("TMPDIR", "/tmp/x")

	got := envMap(t, workerEnvFor(nil))
	for _, need := range []string{"PATH", "HOME", "TMPDIR"} {
		if _, ok := got[need]; !ok {
			t.Errorf("%s missing — the worker cannot run without it", need)
		}
	}
}

func TestWorkerEnv_PassesDeclaredVariables(t *testing.T) {
	got := envMap(t, workerEnvFor([]string{
		"ANTHROPIC_API_KEY=sk-ant-declared",
		"YNH_AGENT_SESSION=1",
	}))
	if got["ANTHROPIC_API_KEY"] != "sk-ant-declared" {
		t.Errorf("a declared variable must reach the worker, got %q", got["ANTHROPIC_API_KEY"])
	}
	if got["YNH_AGENT_SESSION"] != "1" {
		t.Error("ynh's own worker variables must reach the worker")
	}
}

// A harness that deliberately overrides HOME or TMPDIR means it, so explicit
// entries are appended last and win.
func TestWorkerEnv_DeclaredOverridesTheProcessMinimum(t *testing.T) {
	t.Setenv("HOME", "/Users/dev")
	env := workerEnvFor([]string{"HOME=/sandbox/home"})
	got := envMap(t, env)
	if got["HOME"] != "/sandbox/home" {
		t.Errorf("HOME = %q, want the declared override to win", got["HOME"])
	}
}

// Names only, never values — this is what reaches the trajectory.
func TestEnvNames_ListsNamesAndNeverValues(t *testing.T) {
	names := envNames([]string{"B=2", "A=secretvalue", "A=duplicate"})
	joined := strings.Join(names, ",")
	if joined != "A,B" {
		t.Errorf("got %q, want sorted and deduplicated \"A,B\"", joined)
	}
	if strings.Contains(joined, "secretvalue") {
		t.Error("a value reached the name list")
	}
}
