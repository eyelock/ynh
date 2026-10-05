package marketplace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/agentplugin"
)

// setupAgentPluginMarketplace adds a plugin entry that is already an Agent
// Plugins package to the standard fixture.
func setupAgentPluginMarketplace(t *testing.T) (configPath string, configDir string) {
	t.Helper()
	configPath, configDir = setupMarketplace(t)
	pkg := filepath.Join(configDir, "plugins", "portable")
	files := map[string]string{
		"plugin.json":           `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"portable","version":"3.0.0","description":"Already portable"}`,
		"skills/greet/SKILL.md": "---\nname: greet\ndescription: Greet.\n---\nHi.\n",
	}
	for rel, content := range files {
		p := filepath.Join(pkg, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(t, configPath, map[string]any{
		"name":  "test-marketplace",
		"owner": map[string]string{"name": "tester"},
		"harnesses": []map[string]string{
			{"type": "harness", "source": "./harnesses/david"},
			{"type": "plugin", "source": "./plugins/my-tool"},
			{"type": "plugin", "source": "./plugins/portable"},
		},
	})
	return configPath, configDir
}

func TestMarketplaceAgentPluginFormat(t *testing.T) {
	configPath, configDir := setupAgentPluginMarketplace(t)
	outputDir := t.TempDir()
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(cfg, BuildOptions{ConfigDir: configDir, OutputDir: outputDir, AgentPlugin: true}); err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The harness entry is one conformant package with every vendor's share.
	pkg := filepath.Join(outputDir, "plugins", "export-test")
	for _, rel := range []string{"plugin.json", "skills/dev-project/SKILL.md", "com.github.copilot/agents/planner.md", ".claude-plugin/plugin.json", "agents/planner.md"} {
		assertFileExists(t, filepath.Join(pkg, filepath.FromSlash(rel)))
	}
	if _, err := os.Stat(filepath.Join(pkg, ".cursor-plugin")); !os.IsNotExist(err) {
		t.Error("a Cursor manifest has no place in an Agent Plugin")
	}
	if issues := agentplugin.Validate(pkg); len(issues) != 0 {
		t.Errorf("harness entry does not conform: %v", issues)
	}

	// An entry that already is an Agent Plugin is copied untouched, plus
	// the one compatibility manifest for the client outside the format.
	portable := filepath.Join(outputDir, "plugins", "portable")
	assertFileExists(t, filepath.Join(portable, ".claude-plugin", "plugin.json"))
	for _, rel := range []string{".cursor-plugin", ".codex-plugin"} {
		if _, err := os.Stat(filepath.Join(portable, rel)); !os.IsNotExist(err) {
			t.Errorf("%s generated into an Agent Plugin entry", rel)
		}
	}
	data, _ := os.ReadFile(filepath.Join(portable, "plugin.json"))
	if !strings.Contains(string(data), `"name":"portable"`) {
		t.Errorf("package manifest rewritten: %s", data)
	}
	if issues := agentplugin.Validate(portable); len(issues) != 0 {
		t.Errorf("plugin entry does not conform: %v", issues)
	}

	// The legacy Claude plugin entry keeps today's behaviour.
	assertFileExists(t, filepath.Join(outputDir, "plugins", "my-tool", ".cursor-plugin", "plugin.json"))

	// Every vendor index lists all three.
	for _, index := range []string{".claude-plugin/marketplace.json", ".cursor-plugin/marketplace.json", ".agents/plugins/marketplace.json", ".github/plugin/marketplace.json"} {
		raw, err := os.ReadFile(filepath.Join(outputDir, filepath.FromSlash(index)))
		if err != nil {
			t.Fatalf("%s: %v", index, err)
		}
		for _, name := range []string{"export-test", "my-tool", "portable"} {
			if !strings.Contains(string(raw), `"`+name+`"`) {
				t.Errorf("%s does not list %s", index, name)
			}
		}
	}
}

// The vendor format leaves an Agent Plugin entry alone too, except for the
// compatibility manifest, since clients that load the format detect it.
func TestMarketplaceVendorFormatKeepsAgentPluginEntryClean(t *testing.T) {
	configPath, configDir := setupAgentPluginMarketplace(t)
	outputDir := t.TempDir()
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(cfg, BuildOptions{ConfigDir: configDir, OutputDir: outputDir}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	portable := filepath.Join(outputDir, "plugins", "portable")
	assertFileExists(t, filepath.Join(portable, ".claude-plugin", "plugin.json"))
	if _, err := os.Stat(filepath.Join(portable, ".cursor-plugin")); !os.IsNotExist(err) {
		t.Error(".cursor-plugin generated into an Agent Plugin entry")
	}
	// The harness entry is the merged vendor tree it always was.
	assertFileExists(t, filepath.Join(outputDir, "plugins", "export-test", ".cursor-plugin", "plugin.json"))
}

// A harness entry whose name is not a valid Agent Plugins name lands in a
// directory named as the package is, so the indexes resolve, and the
// normalisation is reported.
func TestMarketplaceAgentPluginNormalisedName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Odd_Name")
	if err := os.MkdirAll(filepath.Join(src, ".agents", "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".agents", "harness", "plugin.json"), []byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"Odd_Name","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "AGENTS.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "marketplace.json")
	writeJSON(t, configPath, map[string]any{
		"name": "m", "owner": map[string]string{"name": "t"},
		"harnesses": []map[string]string{{"type": "harness", "source": "./Odd_Name"}},
	})
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	warnings, err := Build(cfg, BuildOptions{ConfigDir: dir, OutputDir: out, AgentPlugin: true, Vendors: []string{"codex"}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The package export's warnings reach the caller like any harness
	// entry's, prefixed with the entry (#496).
	want := `odd-name: name "Odd_Name" is not a valid Agent Plugins name; exported as "odd-name"`
	if len(warnings) != 1 || warnings[0] != want {
		t.Errorf("warnings = %q, want [%q]", warnings, want)
	}
	assertFileExists(t, filepath.Join(out, "plugins", "odd-name", "plugin.json"))
	raw, _ := os.ReadFile(filepath.Join(out, ".agents", "plugins", "marketplace.json"))
	var idx struct {
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Path string `json:"path"`
			} `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil {
		t.Fatal(err)
	}
	if len(idx.Plugins) != 1 || idx.Plugins[0].Name != "odd-name" || idx.Plugins[0].Source.Path != "./plugins/odd-name" {
		t.Errorf("codex index = %s", raw)
	}
}
