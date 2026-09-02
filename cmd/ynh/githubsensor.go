package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/plugin"
)

// GitHub sensors observe verdicts that are not derivable locally. A command
// sensor can run your linter; it cannot know what a scanner running on
// infrastructure you do not control concluded about this commit.
//
// They shell out to `gh` rather than speaking HTTP. That keeps authentication,
// enterprise hosts, rate limiting and token refresh in the tool that already
// solves them, and keeps ynh's dependency count where it is.
//
// The cost is honest and worth stating: this reaches the network from inside a
// gate. A blocking GitHub sensor can fail because GitHub is slow, and the
// exitCodeGateBroken result below is what says so rather than pretending the
// code under test is at fault.

// githubExitCode is the outcome of a GitHub observation, in the same
// vocabulary the rest of the gate uses.
const (
	ghPass   = 0
	ghFail   = 1
	ghBroken = gate.ExitObservationUnavailable
)

// runGH executes a gh subcommand and returns raw stdout.
func runGH(dir string, args ...string) ([]byte, string, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return []byte(out.String()), errBuf.String(), err
}

// resolveRepo returns "owner/name", preferring the declared value and falling
// back to whatever `gh` infers from the directory under test. The fallback is
// what keeps a sensor portable: the same declaration works in a fork.
func resolveRepo(dir, declared string) (string, error) {
	if declared != "" {
		return declared, nil
	}
	out, stderr, err := runGH(dir, "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
	if err != nil {
		return "", fmt.Errorf("could not determine the repository from %s: %s", dir, ghDetail(stderr))
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", fmt.Errorf("could not determine the repository from %s", dir)
	}
	return name, nil
}

// resolveRef resolves the commit to ask about, in the directory under test.
// Defaulting to HEAD there is what makes these sensors work under --cwd: point
// the gate at an older tree and it asks about that tree's commit.
func resolveRef(dir, declared string) (string, error) {
	ref := declared
	if ref == "" {
		ref = "HEAD"
	}
	// A ref beginning with "-" is read by git as an option, not a ref, so
	// "--upload-pack=..." would run a command of the declaration's choosing.
	// Same hole ynh#358 closed for ls-remote; a sensor manifest is no more
	// trusted than a harness source.
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("github sensor ref %q may not begin with %q", ref, "-")
	}
	// Everything after this is positional, whatever it looks like.
	cmd := exec.Command("git", "rev-parse", "--end-of-options", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("could not resolve ref %q in %s", ref, dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// applyOnMissing converts the configured policy into an exit code.
// Default "broken": a sensor that cannot see is not a sensor that passed,
// the same rule freshness applies to `unknown`.
func applyOnMissing(policy string) int {
	switch policy {
	case "pass":
		return ghPass
	case "fail":
		return ghFail
	default:
		return ghBroken
	}
}

// ghDetail is firstLine (toolversion.go) with a fallback, because gh can fail
// with an exit code and nothing on stderr, and "failed: " reads like a bug in
// ynh rather than an unreachable network.
func ghDetail(stderr string) string {
	if l := firstLine(stderr); l != "" {
		return l
	}
	return "no detail from gh"
}

// observeGitHubStatus selects one commit status by context and reports whether
// it holds the required state.
func observeGitHubStatus(dir string, g *plugin.GitHubStatusSource) (exitCode int, stdout string) {
	repo, err := resolveRepo(dir, g.Repo)
	if err != nil {
		return ghBroken, err.Error()
	}
	sha, err := resolveRef(dir, g.Ref)
	if err != nil {
		return ghBroken, err.Error()
	}
	want := g.Require
	if want == "" {
		want = "success"
	}

	path := fmt.Sprintf("repos/%s/commits/%s/status", repo, sha)
	out, stderr, err := runGH(dir, "api", path)
	if err != nil {
		return ghBroken, fmt.Sprintf("gh api %s failed: %s", path, ghDetail(stderr))
	}
	var payload struct {
		Statuses []struct {
			Context     string `json:"context"`
			State       string `json:"state"`
			TargetURL   string `json:"target_url"`
			Description string `json:"description"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return ghBroken, fmt.Sprintf("could not parse the status payload for %s: %v", sha, err)
	}

	for _, st := range payload.Statuses {
		if st.Context != g.Context {
			continue
		}
		line := fmt.Sprintf("%s: %s (want %s) at %s", st.Context, st.State, want, shortSHA(sha))
		if st.Description != "" {
			line += "\n" + st.Description
		}
		if st.TargetURL != "" {
			line += "\n" + st.TargetURL
		}
		if st.State == want {
			return ghPass, line
		}
		// Pending is not a verdict. Treating "not finished yet" as failure
		// would make a gate race the scanner it is reading.
		if st.State == "pending" && want != "pending" {
			return applyOnMissing(g.OnMissing), line + "\nstill pending — no verdict yet"
		}
		return ghFail, line
	}

	return applyOnMissing(g.OnMissing), fmt.Sprintf(
		"no status with context %q on %s in %s\nseen: %s",
		g.Context, shortSHA(sha), repo, contextList(payloadContexts(payload.Statuses)))
}

func payloadContexts(sts []struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	TargetURL   string `json:"target_url"`
	Description string `json:"description"`
}) []string {
	out := make([]string, 0, len(sts))
	for _, s := range sts {
		out = append(out, s.Context)
	}
	return out
}

// observeGitHubCheck selects one check run by name (optionally narrowed by the
// app that posted it) and reports whether its conclusion is the required one.
func observeGitHubCheck(dir string, g *plugin.GitHubCheckSource) (exitCode int, stdout string) {
	repo, err := resolveRepo(dir, g.Repo)
	if err != nil {
		return ghBroken, err.Error()
	}
	sha, err := resolveRef(dir, g.Ref)
	if err != nil {
		return ghBroken, err.Error()
	}
	want := g.Require
	if want == "" {
		want = "success"
	}

	path := fmt.Sprintf("repos/%s/commits/%s/check-runs", repo, sha)
	out, stderr, err := runGH(dir, "api", path)
	if err != nil {
		return ghBroken, fmt.Sprintf("gh api %s failed: %s", path, ghDetail(stderr))
	}
	var payload struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
			App        struct {
				Slug string `json:"slug"`
			} `json:"app"`
		} `json:"check_runs"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return ghBroken, fmt.Sprintf("could not parse the check-run payload for %s: %v", sha, err)
	}

	var seen []string
	for _, run := range payload.CheckRuns {
		seen = append(seen, run.Name)
		if run.Name != g.Name {
			continue
		}
		if g.App != "" && run.App.Slug != g.App {
			continue
		}
		line := fmt.Sprintf("%s: %s/%s (want %s) at %s",
			run.Name, run.Status, orNone(run.Conclusion), want, shortSHA(sha))
		if run.HTMLURL != "" {
			line += "\n" + run.HTMLURL
		}
		// A run that has not completed has no conclusion at all. That is an
		// absent observation, not a failing one.
		if run.Status != "completed" {
			return applyOnMissing(g.OnMissing), line + "\nnot completed — no conclusion yet"
		}
		if run.Conclusion == want {
			return ghPass, line
		}
		return ghFail, line
	}

	where := fmt.Sprintf("no check run named %q", g.Name)
	if g.App != "" {
		where += fmt.Sprintf(" from app %q", g.App)
	}
	return applyOnMissing(g.OnMissing), fmt.Sprintf(
		"%s on %s in %s\nseen: %s", where, shortSHA(sha), repo, contextList(seen))
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// contextList renders what was actually present. A sensor that found nothing
// should say what it did find: a renamed status is the common cause, and the
// list turns a dead end into a one-line fix.
func contextList(names []string) string {
	if len(names) == 0 {
		return "(none on this commit)"
	}
	return strings.Join(names, ", ")
}
