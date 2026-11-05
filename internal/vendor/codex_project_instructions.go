package vendor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// CodexProjectDocMaxBytes is Codex's default cap on the project instructions it
// reads (project_doc_max_bytes, 32 KiB). Past it Codex cuts the file off at the
// end, and the project's part comes last.
const CodexProjectDocMaxBytes = 32 * 1024

// ProjectInstructions is what ComposeProjectInstructions composed.
type ProjectInstructions struct {
	// Files are the project files appended, outermost directory first.
	Files []string
	// Size is the length of the run directory's AGENTS.md afterwards.
	Size int
}

// ComposeProjectInstructions makes the project's own AGENTS.md files reach
// Codex under `ynh run` (ynh #690).
//
// Codex runs in ynh's run directory, which is not a git repository, so it
// reads only the AGENTS.md in that directory: the harness's. Starting Codex
// with -C <project> instead would show it the project's files, but the run
// directory is where ynh stages the harness's agents, hooks and MCP servers
// and where it grants Codex trust (CodexHookTrustArgs), so the harness would
// stop taking effect. The files Codex would read from the project are
// therefore appended to the run directory's AGENTS.md, after the harness's:
// from the Git root down to projectDir, one per directory, in that order,
// each the directory's AGENTS.override.md if it has one, else its AGENTS.md.
// With no Git root above projectDir, only projectDir's own file. The run
// directory is rebuilt from nothing on every run, so the composition never
// accumulates.
//
// A file whose text the harness's instructions already hold is left out, as
// happens when projectDir is the harness's own directory. Nothing is changed
// when there are no project files, or projectDir is the run directory.
func ComposeProjectInstructions(runDir, projectDir string) (ProjectInstructions, error) {
	var res ProjectInstructions
	if projectDir == "" || filepath.Clean(projectDir) == filepath.Clean(runDir) {
		return res, nil
	}
	dst := filepath.Join(runDir, "AGENTS.md")
	composed, err := os.ReadFile(dst)
	if err != nil && !os.IsNotExist(err) {
		return res, fmt.Errorf("reading harness instructions: %w", err)
	}
	harnessText := composed

	for _, dir := range projectDocDirs(projectDir) {
		src, text, err := readProjectDoc(dir)
		if err != nil {
			return res, err
		}
		if len(text) == 0 || bytes.Contains(harnessText, bytes.TrimSpace(text)) {
			continue
		}
		if len(composed) > 0 && composed[len(composed)-1] != '\n' {
			composed = append(composed, '\n')
		}
		if len(composed) > 0 {
			composed = append(composed, '\n')
		}
		composed = append(composed, []byte("<!-- Project instructions, from "+src+" -->\n\n")...)
		composed = append(composed, text...)
		res.Files = append(res.Files, src)
	}
	if len(res.Files) == 0 {
		return res, nil
	}
	if err := os.WriteFile(dst, composed, 0o644); err != nil {
		return res, fmt.Errorf("writing composed instructions: %w", err)
	}
	res.Size = len(composed)
	return res, nil
}

// projectDocDirs is the directories Codex reads project docs from: the Git
// root down to dir, or just dir when no Git root is above it.
func projectDocDirs(dir string) []string {
	dir = filepath.Clean(dir)
	var chain []string
	for d := dir; ; d = filepath.Dir(d) {
		chain = append([]string{d}, chain...)
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return chain
		}
		if filepath.Dir(d) == d {
			return []string{dir}
		}
	}
}

// readProjectDoc reads dir's AGENTS.override.md, else its AGENTS.md. It
// returns no text when neither exists.
func readProjectDoc(dir string) (string, []byte, error) {
	for _, name := range []string{"AGENTS.override.md", "AGENTS.md"} {
		src := filepath.Join(dir, name)
		text, err := os.ReadFile(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("reading project instructions: %w", err)
		}
		return src, text, nil
	}
	return "", nil, nil
}
