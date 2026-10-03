package sources

import (
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/plugin"
)

// writePluginJSONFile writes data, verbatim, as dir's canonical
// .agents/harness/plugin.json.
func writePluginJSONFile(dir string, data []byte) error {
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644)
}
