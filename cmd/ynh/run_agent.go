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
	if ra.Task == "" {
		return cliError(os.Stderr, false, errCodeInvalidInput,
			"--agent requires --task <text|@file|->")
	}
	if ra.HarnessFile != "" {
		return cliError(os.Stderr, false, errCodeInvalidInput,
			"--agent does not support --harness-file (load via harness name)")
	}

	opts := agent.RunOptions{
		HarnessName:       ra.HarnessName,
		Task:              ra.Task,
		Backend:           ra.AgentBackend,
		Sandbox:           ra.AgentSandbox,
		Model:             ra.AgentModel,
		MaxTurns:          ra.AgentMaxTurns,
		MaxTokens:         ra.AgentMaxTokens,
		MaxWall:           ra.AgentMaxWall,
		ConvergenceSensor: ra.AgentConvergence,
		AutoCommit:        ra.AgentAutoCommit,
		Interactive:       ra.Interactive,
		NoPlan:            ra.AgentNoPlan,
		WorktreeDir:       ra.AgentWorktree,
		EmitJSONL:         ra.AgentEmitJSONL,
		SensorOverlay:     ra.AgentSensorOver,
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
