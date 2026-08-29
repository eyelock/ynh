package agent

import (
	"errors"
	"os/exec"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/gate"
)

// RunResult is the machine-readable outcome of one agent run.
//
// `--emit-jsonl` is an event *stream*, not a result. A pipeline that wants to
// know what happened had to request the stream, tail it, find the last
// session_end and reconstruct the rest by inference across events — which is
// exactly the bespoke tooling a factory is not allowed to grow. Every other ynh
// command answers `--format json` with one object; this makes `agent run` the
// same.
//
// Everything here already existed inside the loop. Nothing new is measured —
// it is collected and stated once, at the end, in one place.
type RunResult struct {
	Capabilities string `json:"capabilities"`
	YnhVersion   string `json:"ynh_version"`

	// ExitCode mirrors the process exit code, so a consumer reading the JSON
	// and a consumer branching on $? never disagree.
	ExitCode  int    `json:"exit_code"`
	Reason    string `json:"reason,omitempty"`
	Converged bool   `json:"converged"`

	SessionID  string `json:"session_id,omitempty"`
	SessionDir string `json:"session_dir,omitempty"`
	Worktree   string `json:"worktree,omitempty"`
	Backend    string `json:"backend,omitempty"`
	Model      string `json:"model,omitempty"`

	Harness *RunHarness `json:"harness,omitempty"`

	Budgets       BudgetLimits `json:"budgets"`
	BudgetSources BudgetSource `json:"budget_sources"`
	Consumed      RunConsumed  `json:"consumed"`
	// BoundBy names the cap that ended the run — turns, tokens or wall_clock —
	// and is empty when no cap bound it. A cap nobody chose that fires is noise
	// in a batch of a hundred runs; a cap that was chosen and fires is a
	// finding. BudgetSources is what tells those apart.
	BoundBy string `json:"bound_by,omitempty"`

	// Convergence carries the convergence-verifier's last word, when one was
	// declared. Absent means none was declared, which is different from one
	// that ran and disagreed.
	Convergence *RunConvergence `json:"convergence,omitempty"`
	// Sensors is the final gate result — the evidence behind Converged.
	Sensors []gate.Result `json:"sensors,omitempty"`

	// ChangedFiles is what the run actually did to the tree, relative to the
	// commit it started from. A converged run that changed nothing, and a run
	// that rewrote forty files nobody asked about, are both findings.
	ChangedFiles []string `json:"changed_files"`
	BaseCommit   string   `json:"base_commit,omitempty"`

	// budget and planIterations are read by finalise. They are held rather
	// than copied at each of the loop's twenty-odd exit points, because one of
	// those forgetting to copy them would produce a result that looked
	// complete and under-reported what the run actually spent.
	budget         *Budget
	planIterations *int
}

// finalise fills the fields that are only knowable once the run has ended.
//
// It runs deferred, so it covers every exit path including the ones that
// return an error. A pipeline needs to know what a run consumed and what it
// touched precisely when it did *not* converge.
func (r *RunResult) finalise(err error) {
	if err == nil {
		r.ExitCode = ExitConverged
		r.Converged = true
	} else {
		r.Converged = false
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			r.ExitCode = exitErr.Code
			r.Reason = exitErr.Message
		} else {
			// A plain error means the loop failed before it could classify
			// itself. Reporting 0 here would say "converged" about a run that
			// did not, which is the one lie this whole contract exists to
			// prevent.
			r.ExitCode = ExitWorkerError
			r.Reason = err.Error()
		}
	}

	if r.budget != nil {
		r.Consumed.Turns = r.budget.Turns()
		r.Consumed.Tokens = r.budget.Tokens()
		r.Consumed.WallMS = r.budget.WallConsumed().Milliseconds()
	}
	if r.planIterations != nil {
		r.Consumed.PlanIter = *r.planIterations
	}

	r.ChangedFiles = changedFiles(r.Worktree, r.BaseCommit)
}

// RunHarness identifies the harness the run was verified against.
type RunHarness struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	// SHA is the resolved commit of the harness source. Empty until C4 lands;
	// without it a run cannot be pinned to one toolchain.
	SHA string `json:"sha,omitempty"`
}

// RunConsumed is what the run actually spent.
type RunConsumed struct {
	Turns    int   `json:"turns"`
	Tokens   int64 `json:"tokens"`
	WallMS   int64 `json:"wall_ms"`
	PlanIter int   `json:"plan_iterations,omitempty"`
}

// RunConvergence is the convergence-verifier's verdict.
type RunConvergence struct {
	Sensor  string `json:"sensor"`
	Passed  bool   `json:"passed"`
	Summary string `json:"summary,omitempty"`
}

// changedFiles lists paths the run modified relative to base, including
// untracked files.
//
// Tracked changes alone would miss a run whose entire output is new files,
// which is most of them. Best effort: a worktree that is not a git repository
// reports nothing rather than failing the run.
func changedFiles(dir, base string) []string {
	if dir == "" {
		return []string{}
	}
	seen := map[string]bool{}

	if base != "" {
		if out, err := exec.Command("git", "-C", dir, "diff", "--name-only", base).Output(); err == nil {
			for _, l := range strings.Split(string(out), "\n") {
				if l = strings.TrimSpace(l); l != "" {
					seen[l] = true
				}
			}
		}
	}
	// --porcelain covers staged, unstaged and untracked in one pass, which is
	// what "what did this run do" actually means.
	if out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--untracked-files=all").Output(); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			if len(l) < 4 {
				continue
			}
			path := strings.TrimSpace(l[3:])
			// Renames read as "old -> new"; the new path is what exists now.
			if i := strings.Index(path, " -> "); i >= 0 {
				path = path[i+4:]
			}
			if path = strings.Trim(path, `"`); path != "" {
				seen[path] = true
			}
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
