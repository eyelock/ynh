package vendor

import (
	"os/exec"
)

func init() {
	Register(&Codex{})
}

// Codex implements the Adapter interface for OpenAI Codex CLI.
type Codex struct{}

func (c *Codex) Name() string    { return "codex" }
func (c *Codex) CLIName() string { return "codex" }

func (c *Codex) ConfigDir() string {
	return ".codex"
}

func (c *Codex) InstructionsFile() string { return "codex.md" }

func (c *Codex) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (c *Codex) NeedsSymlinks() bool { return true }

func (c *Codex) Install(stagingDir string, projectDir string) ([]SymlinkEntry, error) {
	return installSymlinks(stagingDir, projectDir, c.ConfigDir(), c.ArtifactDirs())
}

func (c *Codex) Clean(entries []SymlinkEntry) error {
	return cleanSymlinks(entries)
}

func (c *Codex) LaunchInteractive(configPath string, extraArgs []string) error {
	return launchCodex(configPath, extraArgs)
}

func (c *Codex) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	args := append([]string{"exec"}, extraArgs...)
	args = append(args, prompt)
	return launchCodex(configPath, args)
}

func launchCodex(configPath string, extraArgs []string) error {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		return err
	}

	// Launch as child process so ynh stays alive for signal handling.
	// Use cmd.Dir instead of --cd for CWD control.
	cmd := exec.Command(codexBin, extraArgs...)
	cmd.Dir = configPath
	return runChildProcess(cmd)
}
