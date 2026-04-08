package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// lookPathFunc is used to check if a vendor CLI exists. Tests can replace it.
var lookPathFunc = exec.LookPath

// detectVendorCLI checks for supported LLM CLIs on PATH.
func detectVendorCLI() string {
	checks := []struct{ vendor, binary string }{
		{"claude", "claude"},
		{"codex", "codex"},
		{"cursor", "agent"},
	}
	for _, c := range checks {
		if _, err := lookPathFunc(c.binary); err == nil {
			return c.vendor
		}
	}
	return ""
}

// queryLLMFunc is the function used to query the LLM. Tests can replace it.
var queryLLMFunc = queryLLMImpl

// queryLLM sends a prompt to the vendor CLI and returns the response.
func queryLLM(vendor, prompt string) (string, error) {
	return queryLLMFunc(vendor, prompt)
}

// queryLLMImpl is the real implementation that shells out to a vendor CLI.
func queryLLMImpl(vendor, prompt string) (string, error) {
	var cmd *exec.Cmd
	switch vendor {
	case "claude":
		cmd = exec.Command("claude", "-p", "-", "--output-format", "text")
	case "codex":
		cmd = exec.Command("codex", "-q", "-")
	case "cursor":
		cmd = exec.Command("agent", "-p", "-")
	default:
		return "", fmt.Errorf("unsupported vendor %q", vendor)
	}

	// Pass prompt via stdin to avoid exposing file content in process list
	cmd.Stdin = strings.NewReader(prompt)

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			if msg != "" {
				return "", fmt.Errorf("%s: %w", msg, err)
			}
		}
		return "", err
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return "", fmt.Errorf("llm returned empty response")
	}

	return result, nil
}
