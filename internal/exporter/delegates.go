package exporter

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

// ExportDelegates generates agent files for delegates directly into outputDir/agents/.
// Unlike assembler.AssembleDelegates, this writes to the plugin root, not inside ConfigDir.
func ExportDelegates(outputDir string, delegates []harness.Delegate) error {
	if len(delegates) == 0 {
		return nil
	}

	agentsDir := filepath.Join(outputDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return err
	}

	for _, del := range delegates {
		basePath, _, err := resolver.ResolveGitSource(del.GitSource)
		if err != nil {
			return fmt.Errorf("delegate: %w", err)
		}

		delHarness, err := harness.LoadDir(basePath)
		if err != nil {
			return fmt.Errorf("loading delegate harness %s: %w", del.Git, err)
		}

		agentContent := assembler.BuildDelegateAgent(delHarness, basePath)
		agentFile := filepath.Join(agentsDir, delHarness.Name+".md")
		if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
			return fmt.Errorf("writing delegate agent %s: %w", delHarness.Name, err)
		}
	}

	return nil
}
