package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// runArgs holds parsed arguments for `ynh run`.
//
// `ynh run` has two execution modes:
//
//  1. Vendor launch (default). ynh assembles the harness, optionally
//     installs symlinks, and execs the vendor CLI.
//  2. Agent loop (`--agent --task <text>`). ynh hands off to the autonomous
//     agent loop in internal/agent, bypassing the vendor launch entirely.
//
// Vendor flags consumed by ynh:
//
//	-v <vendor>          Override vendor
//	--profile <name>     Apply a named profile overlay
//	--focus <name>       Load a named focus (sets prompt and profile)
//	--harness-file <p>   Load harness from a non-discovered location
//	--session-name <n>   Session label, not forwarded to vendor
//	--instructions <s>   Per-invocation context injected into vendor pipeline
//	--install            Install symlinks for the vendor in cwd
//	--clean              Remove symlinks for the vendor in cwd
//	--interactive        Stay in session after focus/prompt
//
// Agent flags consumed by ynh (requires --agent):
//
//	--agent              Switch from vendor launch to agent loop
//	--task <text|@file|->        Task text (inline / file / stdin)
//	--backend <name>             Worker backend (claude|codex; default claude)
//	--sandbox <mode>             Sandbox mode (srt|none)
//	--model <name>               Worker model override
//	--max-turns <n>              Turn budget (0 = unlimited)
//	--max-tokens <n>             Token budget
//	--max-wall <duration>        Wall-clock budget
//	--convergence-sensor <name>  Sensor consulted as final done-check
//	--auto-commit                git-commit after each turn
//	--no-plan                    Skip plan phase
//	--worktree <path>            Run worker in <path>
//	--emit-jsonl <path>          Trajectory output (- = stdout)
//	--sensor-overlay <json>      Per-sensor JSON patch
//
// All other arguments are passed through to the vendor CLI verbatim.
// Use -- to separate vendor flags from the prompt when vendor flags take values.
type runArgs struct {
	HarnessName  string
	HarnessFile  string
	VendorFlag   string
	ProfileFlag  string
	FocusFlag    string
	SessionName  string
	Instructions string
	Prompt       string
	VendorArgs   []string
	Action       string // "install", "clean", or ""
	Interactive  bool

	// Agent-loop mode. When Agent is true, vendor-launch fields above are
	// ignored and the agent fields below drive execution.
	Agent            bool
	Task             string
	AgentBackend     string
	AgentSandbox     string
	AgentModel       string
	AgentMaxTurns    int
	AgentMaxTokens   int64
	AgentMaxWall     time.Duration
	AgentConvergence string
	AgentAutoCommit  bool
	AgentNoPlan      bool
	AgentWorktree    string
	AgentEmitJSONL   string
	AgentSensorOver  map[string]json.RawMessage
}

// runArgsError is returned by parseRunArgs when a flag value is malformed.
// The caller (cmdRun) reports it via cliError after parsing finishes so
// behaviour stays identical to the rest of the dispatcher.
type runArgsError struct{ msg string }

func (e *runArgsError) Error() string { return e.msg }

// parseRunArgs separates ynh's own flags from vendor pass-through args and the prompt.
//
// The returned error is non-nil only when an agent-mode flag value is
// malformed (e.g. --max-wall got an unparseable duration). Vendor-mode
// flags currently never produce parse errors — unknown flags are simply
// passed through to the vendor CLI as the user expects.
func parseRunArgs(args []string) (runArgs, error) {
	var ra runArgs
	flagArgs := args

	// First pass: find -- separator and extract trailing prompt
	for i, arg := range args {
		if arg == "--" {
			flagArgs = args[:i]
			if i+1 < len(args) {
				ra.Prompt = args[i+1]
			}
			break
		}
	}

	// Pre-scan for --agent so the second pass knows whether to consume
	// agent-mode flag names (--model, --max-turns, etc.) or let them pass
	// through to the vendor. Without this two-pass approach, harmless
	// invocations like `ynh run david --model opus` would have --model
	// silently swallowed by ynh instead of forwarded to Codex/Claude.
	agentMode := false
	for _, a := range flagArgs {
		if a == "--agent" {
			agentMode = true
			break
		}
	}
	ra.Agent = agentMode

	// Second pass: process flags. Agent-mode flags are consumed only when
	// agentMode is true; otherwise they're vendor passthrough.
	firstPositional := true
	for i := 0; i < len(flagArgs); i++ {
		a := flagArgs[i]

		// Always-ynh flags (consumed regardless of mode).
		switch {
		case a == "-v" && i+1 < len(flagArgs):
			ra.VendorFlag = flagArgs[i+1]
			i++
			continue
		case a == "--profile" && i+1 < len(flagArgs):
			ra.ProfileFlag = flagArgs[i+1]
			i++
			continue
		case a == "--focus" && i+1 < len(flagArgs):
			ra.FocusFlag = flagArgs[i+1]
			i++
			continue
		case a == "--harness-file" && i+1 < len(flagArgs):
			ra.HarnessFile = flagArgs[i+1]
			i++
			continue
		case a == "--session-name" && i+1 < len(flagArgs):
			ra.SessionName = flagArgs[i+1]
			i++
			continue
		case a == "--instructions" && i+1 < len(flagArgs):
			ra.Instructions = flagArgs[i+1]
			i++
			continue
		case a == "--install":
			ra.Action = "install"
			continue
		case a == "--clean":
			ra.Action = "clean"
			continue
		case a == "--interactive":
			ra.Interactive = true
			continue
		case a == "--agent":
			// already captured in pre-scan; consume here so it doesn't
			// fall through to vendor passthrough.
			continue
		}

		// Agent-mode-only flags. Only consumed when --agent is present.
		if agentMode {
			switch {
			case a == "--task" && i+1 < len(flagArgs):
				task, err := readTaskArg(flagArgs[i+1])
				if err != nil {
					return ra, &runArgsError{msg: err.Error()}
				}
				ra.Task = task
				i++
				continue
			case a == "--backend" && i+1 < len(flagArgs):
				ra.AgentBackend = flagArgs[i+1]
				i++
				continue
			case a == "--sandbox" && i+1 < len(flagArgs):
				ra.AgentSandbox = flagArgs[i+1]
				i++
				continue
			case a == "--model" && i+1 < len(flagArgs):
				ra.AgentModel = flagArgs[i+1]
				i++
				continue
			case a == "--max-turns" && i+1 < len(flagArgs):
				n, err := strconv.Atoi(flagArgs[i+1])
				if err != nil || n < 0 {
					return ra, &runArgsError{msg: "--max-turns must be a non-negative integer"}
				}
				ra.AgentMaxTurns = n
				i++
				continue
			case a == "--max-tokens" && i+1 < len(flagArgs):
				n, err := strconv.ParseInt(flagArgs[i+1], 10, 64)
				if err != nil || n < 0 {
					return ra, &runArgsError{msg: "--max-tokens must be a non-negative integer"}
				}
				ra.AgentMaxTokens = n
				i++
				continue
			case a == "--max-wall" && i+1 < len(flagArgs):
				d, err := time.ParseDuration(flagArgs[i+1])
				if err != nil {
					return ra, &runArgsError{msg: fmt.Sprintf("--max-wall: %v", err)}
				}
				ra.AgentMaxWall = d
				i++
				continue
			case a == "--convergence-sensor" && i+1 < len(flagArgs):
				ra.AgentConvergence = flagArgs[i+1]
				i++
				continue
			case a == "--auto-commit":
				ra.AgentAutoCommit = true
				continue
			case a == "--no-plan":
				ra.AgentNoPlan = true
				continue
			case a == "--worktree" && i+1 < len(flagArgs):
				ra.AgentWorktree = flagArgs[i+1]
				i++
				continue
			case a == "--emit-jsonl" && i+1 < len(flagArgs):
				ra.AgentEmitJSONL = flagArgs[i+1]
				i++
				continue
			case a == "--sensor-overlay" && i+1 < len(flagArgs):
				var overlay map[string]json.RawMessage
				if err := json.Unmarshal([]byte(flagArgs[i+1]), &overlay); err != nil {
					return ra, &runArgsError{msg: fmt.Sprintf("--sensor-overlay: invalid JSON: %v", err)}
				}
				ra.AgentSensorOver = overlay
				i++
				continue
			}
		}

		// Fall-through: positional or vendor passthrough.
		switch {
		case !strings.HasPrefix(a, "-"):
			if firstPositional {
				ra.HarnessName = a
				firstPositional = false
			} else if ra.Prompt == "" {
				ra.Prompt = a
			} else {
				ra.VendorArgs = append(ra.VendorArgs, a)
			}
		default:
			ra.VendorArgs = append(ra.VendorArgs, a)
		}
	}

	if ra.FocusFlag == "" {
		ra.FocusFlag = os.Getenv("YNH_FOCUS")
	}
	if ra.HarnessFile == "" {
		ra.HarnessFile = os.Getenv("YNH_HARNESS_FILE")
	}

	return ra, nil
}

// readTaskArg reads the task text from a flag value:
//   - "-" reads from stdin
//   - "@path" reads from the named file
//   - anything else is used as-is (inline text)
func readTaskArg(val string) (string, error) {
	if val == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading task from stdin: %w", err)
		}
		return strings.TrimRight(string(data), "\n"), nil
	}
	if strings.HasPrefix(val, "@") {
		data, err := os.ReadFile(val[1:])
		if err != nil {
			return "", fmt.Errorf("reading task file %q: %w", val[1:], err)
		}
		return strings.TrimRight(string(data), "\n"), nil
	}
	return val, nil
}
