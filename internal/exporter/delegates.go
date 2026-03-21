package exporter

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/persona"
	"github.com/eyelock/ynh/internal/resolver"
)

// ExportDelegates generates agent files for delegates directly into outputDir/agents/.
// Unlike assembler.AssembleDelegates, this writes to the plugin root, not inside ConfigDir.
func ExportDelegates(outputDir string, delegates []persona.Delegate) error {
	if len(delegates) == 0 {
		return nil
	}

	agentsDir := filepath.Join(outputDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return err
	}

	for _, del := range delegates {
		basePath, err := resolver.ResolveGitSource(del.GitSource)
		if err != nil {
			return fmt.Errorf("delegate: %w", err)
		}

		delPersona, err := persona.LoadPluginDir(basePath)
		if err != nil {
			return fmt.Errorf("loading delegate persona %s: %w", del.Git, err)
		}

		agentContent := assembler.BuildDelegateAgent(delPersona, basePath)
		agentFile := filepath.Join(agentsDir, delPersona.Name+".md")
		if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
			return fmt.Errorf("writing delegate agent %s: %w", delPersona.Name, err)
		}
	}

	return nil
}
