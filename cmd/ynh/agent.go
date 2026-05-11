package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/agent"
)

func cmdAgent(args []string) error {
	return cmdAgentTo(args, os.Stdout, os.Stderr, os.Stdin)
}

func cmdAgentTo(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
	if len(args) < 1 {
		return cliError(stderr, false, errCodeInvalidInput,
			"usage: ynh agent <run> [args]")
	}
	switch args[0] {
	case "run":
		return cmdAgentRun(args[1:], stdout, stderr, stdin)
	default:
		return cliError(stderr, false, errCodeInvalidInput,
			fmt.Sprintf("unknown agent subcommand: %s", args[0]))
	}
}

func cmdAgentRun(args []string, stdout, stderr io.Writer, stdin io.Reader) error {
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
				return cliError(stderr, false, errCodeInvalidInput, "--harness requires a value")
			}
			opts.HarnessName = args[i]

		case "--task":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--task requires a value")
			}
			task, err := readTaskArg(args[i])
			if err != nil {
				return cliError(stderr, false, errCodeIOError, err.Error())
			}
			opts.Task = task

		case "--backend":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--backend requires a value")
			}
			opts.Backend = args[i]

		case "--sandbox":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--sandbox requires a value")
			}
			opts.Sandbox = args[i]

		case "--model":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--model requires a value")
			}
			opts.Model = args[i]

		case "--max-turns":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--max-turns requires a value")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return cliError(stderr, false, errCodeInvalidInput, "--max-turns must be a non-negative integer")
			}
			opts.MaxTurns = n

		case "--max-tokens":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--max-tokens requires a value")
			}
			n, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil || n < 0 {
				return cliError(stderr, false, errCodeInvalidInput, "--max-tokens must be a non-negative integer")
			}
			opts.MaxTokens = n

		case "--max-wall":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--max-wall requires a value")
			}
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return cliError(stderr, false, errCodeInvalidInput,
					fmt.Sprintf("--max-wall: %v", err))
			}
			opts.MaxWall = d

		case "--convergence-sensor":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--convergence-sensor requires a value")
			}
			opts.ConvergenceSensor = args[i]

		case "--auto-commit":
			opts.AutoCommit = true

		case "--interactive":
			opts.Interactive = true

		case "--no-plan":
			opts.NoPlan = true

		case "--worktree":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--worktree requires a value")
			}
			opts.WorktreeDir = args[i]

		case "--emit-jsonl":
			i++
			if i >= len(args) {
				return cliError(stderr, false, errCodeInvalidInput, "--emit-jsonl requires a value")
			}
			opts.EmitJSONL = args[i]

		default:
			if strings.HasPrefix(arg, "-") {
				return cliError(stderr, false, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", arg))
			}
			return cliError(stderr, false, errCodeInvalidInput,
				fmt.Sprintf("unexpected argument: %s", arg))
		}
	}

	if opts.Task == "" {
		return cliError(stderr, false, errCodeInvalidInput,
			"--task is required")
	}

	if err := agent.RunLoop(opts); err != nil {
		if exitErr, ok := err.(*agent.ExitError); ok {
			_, _ = fmt.Fprintf(stderr, "Error: %v\n", exitErr)
			os.Exit(exitErr.Code)
		}
		return err
	}
	return nil
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
