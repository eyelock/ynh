package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func writeAgentPluginPackage(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"plugin.json":           `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"portable","version":"1.0.0","description":"A package"}`,
		"skills/hello/SKILL.md": "---\nname: hello\ndescription: Hi.\n---\nHi.\n",
		"mcp.json":              `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"docs":{"type":"streamable-http","url":"https://docs.example.com/mcp"}}}`,
		"hooks/hooks.json":      "{}",
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Installing an Agent Plugins package from a local path leaves the package
// untouched, records the format, and gives info a derived manifest.
func TestCmdInstall_AgentPluginPointerForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	pkg := filepath.Join(t.TempDir(), "portable")
	writeAgentPluginPackage(t, pkg)

	var out bytes.Buffer
	var err error
	captureStdout(t, &out, func() { err = cmdInstall([]string{pkg}) })
	if err != nil {
		t.Fatalf("cmdInstall: %v\n%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("note: hooks/hooks.json: client-specific hooks are not imported")) {
		t.Errorf("install output lacks the loader diagnostic:\n%s", out.String())
	}
	for _, md := range []string{".agents", ".ynh-plugin"} {
		if _, statErr := os.Stat(filepath.Join(pkg, md)); !os.IsNotExist(statErr) {
			t.Errorf("install wrote %s into the package", md)
		}
	}

	var ins bytes.Buffer
	if err := cmdInstalledTo([]string{"local/portable", "--format", "json"}, &ins, io.Discard); err != nil {
		t.Fatalf("cmdInstalledTo: %v", err)
	}
	var rec struct {
		Installed struct {
			SourceType string `json:"source_type"`
			Format     string `json:"format"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(ins.Bytes(), &rec); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, ins.String())
	}
	if rec.Installed.SourceType != "local" || rec.Installed.Format != "agent-plugin" {
		t.Errorf("installed = %+v", rec.Installed)
	}

	var info bytes.Buffer
	if err := printInfoJSON(&info, io.Discard, "local/portable", false); err != nil {
		t.Fatalf("printInfoJSON: %v", err)
	}
	var env struct {
		Harness struct {
			Name          string `json:"name"`
			Version       string `json:"version_installed"`
			InstalledFrom struct {
				Format string `json:"format"`
			} `json:"installed_from"`
			Manifest struct {
				Name       string                     `json:"name"`
				MCPServers map[string]json.RawMessage `json:"mcp_servers"`
			} `json:"manifest"`
		} `json:"harness"`
	}
	if err := json.Unmarshal(info.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, info.String())
	}
	h := env.Harness
	if h.Name != "portable" || h.Version != "1.0.0" || h.InstalledFrom.Format != "agent-plugin" || h.Manifest.Name != "portable" || len(h.Manifest.MCPServers) != 1 {
		t.Errorf("info = %s", info.String())
	}
}

// captureStdout collects what f prints; cmdInstall writes with fmt.Print.
func captureStdout(t *testing.T, buf *bytes.Buffer, f func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan struct{})
	go func() { _, _ = buf.ReadFrom(r); close(done) }()
	f()
	_ = w.Close()
	os.Stdout = old
	<-done
}
