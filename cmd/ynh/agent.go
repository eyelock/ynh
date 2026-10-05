package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/telemetry"
)

func cmdAgent(args []string) error {
	return cmdAgentTo(args, os.Stdout, os.Stderr, os.Stdin)
}

func cmdAgentTo(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	structured := detectJSONFormat(args)
	if len(args) < 1 {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh agent <run> [args]")
	}
	switch args[0] {
	case "run":
		return cmdAgentRun(args[1:], stdout, stderr, stdin)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("unknown agent subcommand: %s", args[0]))
	}
}

func cmdAgentRun(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	// Detect --format json before parsing, so an argument error ahead of it is
	// still reported in the structured error envelope (#513).
	structured := detectJSONFormat(args)
	resultFormat := "text"
	relayFlag := false
	opts := agent.RunOptions{
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  stdin,
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--harness":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--harness requires a value")
			}
			opts.HarnessName = args[i]

		case "--task":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--task requires a value")
			}
			task, err := readTaskArg(args[i])
			if err != nil {
				return cliError(stderr, structured, errCodeIOError, err.Error())
			}
			opts.Task = task

		case "--backend":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--backend requires a value")
			}
			opts.Backend = args[i]

		case "--profile":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--profile requires a value")
			}
			opts.Profile = args[i]

		case "--focus":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--focus requires a value")
			}
			opts.Focus = args[i]

		case "--sandbox":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--sandbox requires a value")
			}
			opts.Sandbox = args[i]

		case "--auto-approve":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--auto-approve requires a value (edits or all)")
			}
			opts.AutoApprove = args[i]

		case "--model":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--model requires a value")
			}
			opts.Model = args[i]

		case "--effort":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--effort requires a value (low, medium or high)")
			}
			opts.Effort = args[i]

		case "--max-turns":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--max-turns requires a value")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return cliError(stderr, structured, errCodeInvalidInput, "--max-turns must be a non-negative integer")
			}
			opts.MaxTurns = n

		case "--max-tokens":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--max-tokens requires a value")
			}
			n, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || n < 0 {
				return cliError(stderr, structured, errCodeInvalidInput, "--max-tokens must be a non-negative integer")
			}
			opts.MaxTokens = n

		case "--max-wall":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--max-wall requires a value")
			}
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("--max-wall: %v", err))
			}
			opts.MaxWall = d

		case "--max-plan-iterations":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--max-plan-iterations requires a value")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return cliError(stderr, structured, errCodeInvalidInput, "--max-plan-iterations must be a non-negative integer")
			}
			opts.MaxPlanIterations = n

		case "--convergence-sensor":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--convergence-sensor requires a value")
			}
			opts.ConvergenceSensor = args[i]

		case "--auto-commit":
			opts.AutoCommit = true

		case "--telemetry-relay":
			relayFlag = true

		case "--interactive":
			opts.Interactive = true

		case "--no-plan":
			opts.NoPlan = true

		case "--worktree":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--worktree requires a value")
			}
			opts.WorktreeDir = args[i]

		case "--format":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			switch args[i] {
			case "json", "text":
				resultFormat = args[i]
			default:
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown format: %s (want text or json)", args[i]))
			}

		case "--emit-jsonl":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--emit-jsonl requires a value")
			}
			opts.EmitJSONL = args[i]

		case "--resume":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--resume requires a value")
			}
			opts.Resume = args[i]

		case "--sensor-overlay":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--sensor-overlay requires a value")
			}
			var overlay map[string]json.RawMessage
			if err := json.Unmarshal([]byte(args[i]), &overlay); err != nil {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("--sensor-overlay: invalid JSON: %v", err))
			}
			opts.SensorOverlay = overlay

		default:
			if strings.HasPrefix(arg, "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", arg))
			}
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("unexpected argument: %s", arg))
		}
	}

	// Mutual exclusion guards mirror `ynh run`:
	// focus already provides both prompt and bound profile.
	if opts.Focus != "" && opts.Task != "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"cannot use --focus and --task together (focus includes a prompt)")
	}
	if opts.Focus != "" && opts.Profile != "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"cannot use --focus and --profile together (focus includes a profile)")
	}

	// On --resume the task is restored from the checkpoint, so it need not be
	// supplied again.
	if opts.Resume == "" && opts.Task == "" && opts.Focus == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"--task or --focus is required")
	}

	// Telemetry starts only once the arguments are good: an argument error is
	// not a run. With no destination this is a no-op and changes nothing.
	tel := telemetry.Setup(config.Version, telemetryOptions, stderr)
	defer tel.Shutdown()
	// On --resume the run is described by the identity its checkpoint
	// restores, not only by the flags given.
	run := tel.StartRun(runStartAttributes(agent.ResumedIdentity(opts))...)
	rt := runTelemetry{run: run}
	if telemetryRelaySetting(relayFlag, stderr) {
		rt.relay = &runRelay{tel: tel, stderr: stderr}
		// Also on a panic: a relay is never left running.
		defer rt.relay.stop()
	}
	opts.Telemetry = rt

	result, err := agent.RunLoop(opts)
	// The vendor has exited: let the relay drain what it sent, then stop it.
	rt.relay.stop()
	run.Finish(runOutcome(result.ExitCode), result.ExitCode == agent.ExitConverged, runEndAttributes(result)...)

	// The result is emitted on every path, converged or not. A pipeline needs
	// to know what a run consumed and what it touched precisely when it did
	// not converge — that is the case worth investigating.
	//
	// It goes to stderr when the trajectory is streaming to stdout, so a
	// consumer piping `--emit-jsonl -` still gets clean NDJSON.
	if resultFormat == "json" {
		sink := stdout
		if opts.EmitJSONL == "-" {
			sink = stderr
		}
		data, encErr := json.MarshalIndent(result, "", "  ")
		if encErr != nil {
			return fmt.Errorf("encoding run result: %w", encErr)
		}
		if _, wErr := fmt.Fprintln(sink, string(data)); wErr != nil {
			return wErr
		}
	}

	if err != nil {
		// The process exits with the code the result reports, so a consumer
		// reading exit_code and one branching on $? never disagree (#509).
		// With --format json the result already carries the reason.
		if resultFormat != "json" {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		}
		return &exitCodeError{code: result.ExitCode, err: err}
	}
	return nil
}

// exitCodeError ends the process with code. Its message has already been
// reported, so main prints nothing more.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

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
