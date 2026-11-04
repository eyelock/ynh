package vendor

import (
	"fmt"
	"os"
	"path/filepath"
)

// ComposeProjectInstructions makes the project's own AGENTS.md reach Codex
// under `ynh run` (ynh #690).
//
// Codex runs in ynh's run directory, which is not a git repository, so it
// reads only the AGENTS.md in that directory: the harness's. The project's
// file, in the directory ynh was started from, is never seen. Starting Codex
// with -C <project> instead would show it the project's file, but the run
// directory is where ynh stages the harness's agents, hooks and MCP servers
// and where it grants Codex trust (CodexHookTrustArgs), so the harness would
// stop taking effect. The project's file is therefore appended to the run
// directory's AGENTS.md, after the harness's, under a heading naming its
// origin. The run directory is rebuilt from nothing on every run, so the
// composition never accumulates.
//
// projectDir with no AGENTS.md, an empty one, or the run directory itself
// changes nothing. It returns the project file it composed, or "".
func ComposeProjectInstructions(runDir, projectDir string) (string, error) {
	if projectDir == "" || filepath.Clean(projectDir) == filepath.Clean(runDir) {
		return "", nil
	}
	src := filepath.Join(projectDir, "AGENTS.md")
	project, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading project instructions: %w", err)
	}
	if len(project) == 0 {
		return "", nil
	}

	dst := filepath.Join(runDir, "AGENTS.md")
	harnessText, err := os.ReadFile(dst)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("reading harness instructions: %w", err)
	}
	composed := append([]byte{}, harnessText...)
	if len(composed) > 0 && composed[len(composed)-1] != '\n' {
		composed = append(composed, '\n')
	}
	if len(composed) > 0 {
		composed = append(composed, '\n')
	}
	composed = append(composed, []byte("<!-- Project instructions, from "+src+" -->\n\n")...)
	composed = append(composed, project...)
	if err := os.WriteFile(dst, composed, 0o644); err != nil {
		return "", fmt.Errorf("writing composed instructions: %w", err)
	}
	return src, nil
}
