package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// checkpointVersion is the on-disk schema version of checkpoint.json. Bump
// when the Checkpoint shape changes in a way a resuming reader must handle.
const checkpointVersion = 1

// checkpointFile is the fixed filename within the session directory.
const checkpointFile = "checkpoint.json"

// CheckpointPhase identifies which loop phase a checkpoint was taken in.
type CheckpointPhase string

const (
	PhasePlan CheckpointPhase = "plan"
	PhaseAct  CheckpointPhase = "act"
)

// CheckpointBudget is the persisted budget accounting carried across a resume.
// WallConsumedMS is cumulative wall-clock so the duration cap survives a
// relaunch; PlanIterations is retained for observability (the plan phase is
// re-run from scratch on resume, so it is not re-injected into the loop).
//
// The split and cost fields were added later. They are optional so a
// checkpoint written before them still loads, restoring them as not yet
// reported, and the *Reported flags keep "reported zero" apart from "never
// reported".
type CheckpointBudget struct {
	Turns                 int     `json:"turns"`
	Tokens                int64   `json:"tokens"`
	WallConsumedMS        int64   `json:"wall_consumed_ms"`
	PlanIterations        int     `json:"plan_iterations"`
	InputTokens           int64   `json:"input_tokens,omitempty"`
	OutputTokens          int64   `json:"output_tokens,omitempty"`
	CacheReadTokens       int64   `json:"cache_read_tokens,omitempty"`
	UsageReported         bool    `json:"usage_reported,omitempty"`
	CacheReported         bool    `json:"cache_reported,omitempty"`
	CacheCreationTokens   int64   `json:"cache_creation_tokens,omitempty"`
	CacheCreationReported bool    `json:"cache_creation_reported,omitempty"`
	CostUSD               float64 `json:"cost_usd,omitempty"`
	CostReported          bool    `json:"cost_reported,omitempty"`
}

// Checkpoint is the resume source of truth. It is written atomically after
// each fully completed turn (and at phase boundaries) and read back by a
// --resume relaunch. The trajectory NDJSON remains the append-only audit
// record; this sidecar is deliberately small and self-contained so it is
// trivial to read back and unit-test.
//
// PendingMessage is the exact next Send payload that drives the turn after
// LastCompletedTurn (a synthesized feedback block, or the act phase's first
// message). On resume the worker is reconstructed via its native resume
// token — restoring history through LastCompletedTurn's response — and then
// PendingMessage is re-sent, which deterministically redoes the interrupted
// turn. This yields the "at most one turn of work is ever lost" guarantee.
type Checkpoint struct {
	Version           int              `json:"version"`
	SessionID         string           `json:"session_id"`
	Backend           string           `json:"backend"`
	ResumeToken       string           `json:"resume_token,omitempty"`
	Phase             CheckpointPhase  `json:"phase"`
	PlanFinalized     bool             `json:"plan_finalized"`
	ApprovedPlan      string           `json:"approved_plan,omitempty"`
	LastCompletedTurn int              `json:"last_completed_turn"`
	PendingMessage    string           `json:"pending_message,omitempty"`
	PendingApproval   string           `json:"pending_approval,omitempty"`
	Budget            CheckpointBudget `json:"budget"`
	Task              string           `json:"task,omitempty"`
	// Focus is the focus the run was started with, restored by name on a
	// resume given neither --task nor --focus so its bound profile applies
	// again. Task holds the focus's prompt as it was resolved.
	Focus string `json:"focus,omitempty"`
	// HarnessName, Profile and ConvergenceSensor are the run's identity.
	// Without them a resume that omits --harness silently continues with no
	// harness and therefore no sensors, which used to report converged.
	// Recording them lets a resume restore the run it is actually resuming.
	HarnessName       string `json:"harness_name,omitempty"`
	Profile           string `json:"profile,omitempty"`
	ConvergenceSensor string `json:"convergence_sensor,omitempty"`
	// MaxTurns and MaxTokens are the caps, not the counters. Budget carries
	// consumption; without the caps a resume silently re-derives them from
	// defaults and can run far past what the original invocation allowed.
	MaxTurns  int   `json:"max_turns,omitempty"`
	MaxTokens int64 `json:"max_tokens,omitempty"`
	// Effort is the reasoning effort the worker reported, so a resumed run
	// still reports it before the relaunched worker has said again.
	Effort string `json:"effort,omitempty"`
	// Model is the model the worker last reported, so a resumed run still
	// reports it before the relaunched worker has said again.
	Model     string `json:"model,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// checkpointPath returns the checkpoint file path inside a session directory.
func checkpointPath(dir string) string {
	return filepath.Join(dir, checkpointFile)
}

// writeCheckpoint writes cp to <dir>/checkpoint.json atomically: it writes a
// temp file in the same directory, fsyncs it, and renames over the target so
// a crash mid-write never leaves a partial or truncated checkpoint. dir must
// already exist. The Version and UpdatedAt fields are stamped here.
func writeCheckpoint(dir string, cp *Checkpoint) error {
	cp.Version = checkpointVersion
	cp.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling checkpoint: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".checkpoint-*.tmp")
	if err != nil {
		return fmt.Errorf("creating checkpoint temp: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we bail before the rename; a no-op once renamed.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing checkpoint temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing checkpoint temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing checkpoint temp: %w", err)
	}
	if err := os.Rename(tmpName, checkpointPath(dir)); err != nil {
		return fmt.Errorf("renaming checkpoint: %w", err)
	}
	return nil
}

// readCheckpoint loads and validates <dir>/checkpoint.json. It returns a
// distinct error for a missing file vs a corrupt or incompatible one so the
// caller can map each to a precise exit code and never half-resume.
func readCheckpoint(dir string) (*Checkpoint, error) {
	data, err := os.ReadFile(checkpointPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no checkpoint found in %q: %w", dir, err)
		}
		return nil, fmt.Errorf("reading checkpoint: %w", err)
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("corrupt checkpoint %q: %w", checkpointPath(dir), err)
	}
	if cp.Version != checkpointVersion {
		return nil, fmt.Errorf("unsupported checkpoint version %d in %q (want %d)", cp.Version, checkpointPath(dir), checkpointVersion)
	}
	if cp.SessionID == "" {
		return nil, fmt.Errorf("corrupt checkpoint %q: missing session_id", checkpointPath(dir))
	}
	return &cp, nil
}

// sessionDirFromEmit derives the session directory from the --emit-jsonl
// path. Durability requires a real file path; "-" (stdout) and "" (discard)
// have no session directory and so disable checkpointing.
func sessionDirFromEmit(emit string) string {
	if emit == "" || emit == "-" {
		return ""
	}
	return filepath.Dir(emit)
}

// restoreIdentity fills the harness, profile and focus a resume did not name
// from its checkpoint. A run interrupted while planning re-runs the plan, which
// needs the task, and a focus is restored by name so its bound profile applies
// again; either is restored only when the resume named neither a task nor a
// focus.
func restoreIdentity(opts *RunOptions, cp *Checkpoint) {
	taskGiven := opts.Task != "" || opts.Focus != ""
	if opts.HarnessName == "" {
		opts.HarnessName = cp.HarnessName
	}
	if opts.Profile == "" {
		opts.Profile = cp.Profile
	}
	if !taskGiven {
		opts.Focus = cp.Focus
		if opts.Focus == "" {
			opts.Task = cp.Task
		}
	}
}

// ResumedIdentity is opts with the harness, profile and focus a --resume
// restores from its checkpoint, by the same rule RunLoop applies, so a caller
// can describe the run before it starts. Without --resume, or when the
// checkpoint cannot be read (RunLoop then refuses the run itself), opts is
// returned unchanged.
func ResumedIdentity(opts RunOptions) RunOptions {
	if opts.Resume == "" {
		return opts
	}
	cp, err := readCheckpoint(opts.Resume)
	if err != nil {
		return opts
	}
	restoreIdentity(&opts, cp)
	return opts
}
