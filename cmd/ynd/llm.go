package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// detectVendorCLI checks for supported LLM CLIs on PATH.
func detectVendorCLI() string {
	for _, name := range []string{"claude", "codex"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
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
		cmd = exec.Command("claude", "-p", prompt, "--output-format", "text")
	case "codex":
		cmd = exec.Command("codex", "-q", prompt)
	default:
		return "", fmt.Errorf("unsupported vendor %q", vendor)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			return "", fmt.Errorf("%s: %w", msg, err)
		}
		return "", err
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return "", fmt.Errorf("LLM returned empty response")
	}

	return result, nil
}
