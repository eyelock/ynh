// Agent-mode dispatcher for `ynh run --agent`. Translates runArgs into the
// internal/agent.RunOptions struct and invokes the loop; maps ExitError to
// os.Exit so the CLI surfaces the worker's exit code cleanly.
package main

import (
	"fmt"
	"os"

	"github.com/eyelock/ynh/internal/agent"
)

func runAgentMode(ra runArgs) error {
	if ra.HarnessFile != "" {
		return cliError(os.Stderr, false, errCodeInvalidInput,
			"--agent does not support --harness-file (load via harness name)")
	}

	profileName := ra.ProfileFlag
	if profileName == "" {
		profileName = os.Getenv("YNH_PROFILE")
	}

	// Mutual exclusion guards mirror `ynh run`: focus already provides both
	// prompt and bound profile.
	if ra.FocusFlag != "" && ra.Task != "" {
		return cliError(os.Stderr, false, errCodeInvalidInput,
			"cannot use --focus and --task together (focus includes a prompt)")
	}
	if ra.FocusFlag != "" && profileName != "" {
		return cliError(os.Stderr, false, errCodeInvalidInput,
			"cannot use --focus and --profile together (focus includes a profile)")
	}

	// On --resume the task is restored from the checkpoint, so it need not
	// be supplied again.
	if ra.AgentResume == "" && ra.Task == "" && ra.FocusFlag == "" {
		return cliError(os.Stderr, false, errCodeInvalidInput,
			"--agent requires --task <text|@file|-> or --focus")
	}

	opts := agent.RunOptions{
		HarnessName:       ra.HarnessName,
		Task:              ra.Task,
		Profile:           profileName,
		Focus:             ra.FocusFlag,
		Backend:           ra.AgentBackend,
		Sandbox:           ra.AgentSandbox,
		Model:             ra.AgentModel,
		MaxTurns:          ra.AgentMaxTurns,
		MaxTokens:         ra.AgentMaxTokens,
		MaxWall:           ra.AgentMaxWall,
		MaxPlanIterations: ra.AgentMaxPlanIter,
		ConvergenceSensor: ra.AgentConvergence,
		AutoCommit:        ra.AgentAutoCommit,
		Interactive:       ra.Interactive,
		NoPlan:            ra.AgentNoPlan,
		WorktreeDir:       ra.AgentWorktree,
		EmitJSONL:         ra.AgentEmitJSONL,
		SensorOverlay:     ra.AgentSensorOver,
		Resume:            ra.AgentResume,
		Stdout:            os.Stdout,
		Stderr:            os.Stderr,
		Stdin:             os.Stdin,
	}

	if err := agent.RunLoop(opts); err != nil {
		if exitErr, ok := err.(*agent.ExitError); ok {
			fmt.Fprintf(os.Stderr, "Error: %v\n", exitErr)
			os.Exit(exitErr.Code)
		}
		return err
	}
	return nil
}
