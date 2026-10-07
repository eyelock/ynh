package main

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

// lookPathFunc is used to check if a vendor CLI exists. Tests can replace it.
var lookPathFunc = exec.LookPath

// llmVendors are the vendors ynd can send a one-shot prompt to, in the order
// detectVendorCLI prefers them. Each entry holds the arguments that make that
// vendor's CLI read the prompt on stdin, which keeps file content out of the
// process list, and print only the answer, as text, on stdout.
//
// The binary is not here: it is the vendor adapter's CLIName, the one place
// each vendor's binary is spelled, so ynd runs the program `ynh run` launches.
// A copy kept here could drift, as the agent worker's did (#524).
var llmVendors = []struct {
	vendor  string
	args    []string
	install string
}{
	{"claude", []string{"-p", "-", "--output-format", "text"}, "https://docs.anthropic.com/claude-code"},
	// codex exec reads the prompt from stdin when it is "-", streams progress
	// to stderr and prints only the final message on stdout
	// (learn.chatgpt.com/docs/non-interactive-mode). It refuses to run
	// outside a git repository unless told to, and compress runs anywhere.
	{"codex", []string{"exec", "--skip-git-repo-check", "-"}, "https://openai.com/codex"},
	{"cursor", []string{"-p", "-"}, "https://cursor.com/docs/cli/installation"},
	// copilot takes piped stdin as the prompt when no -p is given, and -s
	// prints only the agent's response (docs.github.com, "Running GitHub
	// Copilot CLI programmatically" and the CLI command reference). Nobody is
	// there to answer a question, and no tool is allowed: the answer is text.
	// --no-auto-update for the reason the vendor adapter gives: an update
	// found at launch restarts copilot and drops this invocation.
	{"copilot", []string{"--no-auto-update", "-s", "--no-ask-user"}, "https://docs.github.com/en/copilot/how-tos/copilot-cli"},
}

// llmVendorNames lists the vendors ynd can use, comma-separated.
func llmVendorNames() string {
	names := make([]string, len(llmVendors))
	for i, v := range llmVendors {
		names[i] = v.vendor
	}
	return strings.Join(names, ", ")
}

// llmCLI returns the binary and arguments ynd runs a prompt through for
// vendorName.
func llmCLI(vendorName string) (binary string, args []string, err error) {
	for _, v := range llmVendors {
		if v.vendor != vendorName {
			continue
		}
		adapter, err := vendor.Get(vendorName)
		if err != nil {
			return "", nil, err
		}
		return adapter.CLIName(), v.args, nil
	}
	return "", nil, fmt.Errorf("unsupported vendor %q (supported: %s)", vendorName, llmVendorNames())
}

// checkLLMCLI reports whether ynd can use vendorName: a vendor it supports,
// whose CLI is on PATH.
func checkLLMCLI(vendorName string) error {
	binary, _, err := llmCLI(vendorName)
	if err != nil {
		return err
	}
	if _, err := lookPathFunc(binary); err != nil {
		return fmt.Errorf("%s CLI %q not found on PATH", vendorName, binary)
	}
	return nil
}

// detectVendorCLI returns the first vendor in llmVendors whose CLI is on
// PATH, or "" when there is none.
func detectVendorCLI() string {
	for _, v := range llmVendors {
		if checkLLMCLI(v.vendor) == nil {
			return v.vendor
		}
	}
	return ""
}

// printNoLLMCLI tells the operator that no supported CLI was found, what
// command needs one for, and where to get one.
func printNoLLMCLI(w io.Writer, need, command string) {
	_, _ = fmt.Fprintf(w, "No supported LLM CLI found (checked: %s).\n", llmVendorNames())
	_, _ = fmt.Fprintln(w, need)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Install one of:")
	for _, v := range llmVendors {
		binary, _, err := llmCLI(v.vendor)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %-8s %-8s → %s\n", v.vendor, binary, v.install)
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "Or specify one explicitly: ynd %s -v claude\n", command)
}

// queryLLMFunc is the function used to query the LLM. Tests can replace it.
var queryLLMFunc = queryLLMImpl

// queryLLM sends a prompt to the vendor CLI and returns the response.
func queryLLM(vendorName, prompt string) (string, error) {
	return queryLLMFunc(vendorName, prompt)
}

// queryLLMImpl is the real implementation that shells out to a vendor CLI.
func queryLLMImpl(vendorName, prompt string) (string, error) {
	binary, args, err := llmCLI(vendorName)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(binary, args...)

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
