package vendor

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func init() {
	Register(&Claude{})
}

// Claude implements the Adapter interface for Claude Code CLI.
type Claude struct{}

func (c *Claude) Name() string    { return "claude" }
func (c *Claude) CLIName() string { return "claude" }

func (c *Claude) ConfigDir() string {
	return ".claude"
}

func (c *Claude) InstructionsFile() string { return "CLAUDE.md" }

func (c *Claude) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (c *Claude) NeedsSymlinks() bool { return false }

func (c *Claude) Install(stagingDir string, projectDir string) ([]SymlinkEntry, error) {
	return nil, nil
}

func (c *Claude) Clean(entries []SymlinkEntry) error {
	return nil
}

func (c *Claude) LaunchInteractive(configPath string, extraArgs []string) error {
	return launchClaude(configPath, extraArgs)
}

func (c *Claude) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	args := append([]string{"-p", prompt}, extraArgs...)
	return launchClaude(configPath, args)
}

// buildClaudeArgs constructs the argument list for the Claude Code CLI.
// It adds --plugin-dir for artifact loading and --append-system-prompt
// for harness instructions, then appends any vendor pass-through args.
func buildClaudeArgs(configPath string, extraArgs []string) []string {
	args := []string{"claude"}

	// Load assembled artifacts (skills, agents, rules, commands) via --plugin-dir.
	pluginDir := filepath.Join(configPath, ".claude")
	args = append(args, "--plugin-dir", pluginDir)

	// Inject harness instructions if present.
	instructionsPath := filepath.Join(configPath, "CLAUDE.md")
	if data, err := os.ReadFile(instructionsPath); err == nil && len(data) > 0 {
		args = append(args, "--append-system-prompt", string(data))
	}

	args = append(args, extraArgs...)
	return args
}

func launchClaude(configPath string, extraArgs []string) error {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return err
	}

	args := buildClaudeArgs(configPath, extraArgs)
	return syscall.Exec(claudeBin, args, os.Environ())
}
