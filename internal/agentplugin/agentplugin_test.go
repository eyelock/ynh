package agentplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalManifest = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"minimal-plugin"}`

// fullManifest is the "full manifest" example from §5.2 of the specification.
const fullManifest = `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "plugin-name",
  "version": "1.2.0",
  "description": "Brief plugin description",
  "author": {"name": "Author Name", "email": "author@example.com", "url": "https://example.com"},
  "homepage": "https://docs.example.com/plugin",
  "repository": "https://github.com/example/plugin",
  "license": "MIT",
  "keywords": ["keyword1", "keyword2"],
  "extensions": {"com.example.client": {"setting": true}}
}`

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseManifest_SpecExamples(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{{"minimal", minimalManifest}, {"full", fullManifest}} {
		t.Run(tc.name, func(t *testing.T) {
			m, diags, err := ParseManifest([]byte(tc.data))
			if err != nil {
				t.Fatalf("ParseManifest: %v", err)
			}
			if len(diags) != 0 {
				t.Errorf("diagnostics = %v, want none", diags)
			}
			if m.Schema != PluginSchemaID || m.Name == "" {
				t.Errorf("manifest = %+v", m)
			}
		})
	}
	m, _, err := ParseManifest([]byte(fullManifest))
	if err != nil {
		t.Fatal(err)
	}
	if m.Author == nil || m.Author.Email != "author@example.com" {
		t.Errorf("author = %+v", m.Author)
	}
	if string(m.Extensions["com.example.client"]) != `{"setting":true}` {
		t.Errorf("extensions = %s, want the namespace object kept verbatim", m.Extensions["com.example.client"])
	}
}

// §5.5 gives these lists.
func TestParseManifest_NameRule(t *testing.T) {
	for _, name := range []string{"my-plugin", "example.tools", "lint3r", "a"} {
		if _, _, err := ParseManifest([]byte(`{"$schema":"` + PluginSchemaID + `","name":"` + name + `"}`)); err != nil {
			t.Errorf("name %q: unexpected error %v", name, err)
		}
	}
	for _, name := range []string{"My-Plugin", "-start", "has--double", "too.many..dots", "", "end-", strings.Repeat("a", 65)} {
		if _, _, err := ParseManifest([]byte(`{"$schema":"` + PluginSchemaID + `","name":"` + name + `"}`)); err == nil {
			t.Errorf("name %q: expected rejection", name)
		}
	}
}

func TestParseManifest_NonFatalCases(t *testing.T) {
	t.Run("unknown top-level field is reported and ignored", func(t *testing.T) {
		m, diags, err := ParseManifest([]byte(`{"$schema":"` + PluginSchemaID + `","name":"x","hooks":"hooks.json","agents":"agents/"}`))
		if err != nil {
			t.Fatalf("plugin must still load: %v", err)
		}
		if m.Name != "x" || len(diags) != 2 {
			t.Errorf("m=%+v diags=%v, want two unknown-field diagnostics", m, diags)
		}
		if !strings.Contains(diags[0].Message, `"agents"`) || !strings.Contains(diags[1].Message, `"hooks"`) {
			t.Errorf("diags = %v, want sorted field names", diags)
		}
	})
	t.Run("non-object extensions is reported and ignored", func(t *testing.T) {
		m, diags, err := ParseManifest([]byte(`{"$schema":"` + PluginSchemaID + `","name":"x","extensions":["nope"]}`))
		if err != nil {
			t.Fatalf("plugin must still load: %v", err)
		}
		if len(m.Extensions) != 0 || len(diags) != 1 || !strings.Contains(diags[0].Message, "extensions") {
			t.Errorf("m=%+v diags=%v", m, diags)
		}
	})
}

func TestParseManifest_FatalCases(t *testing.T) {
	for name, data := range map[string]string{
		"not an object":          `[]`,
		"missing $schema":        `{"name":"x"}`,
		"wrong version":          `{"$schema":"https://agent-plugins.org/schemas/9.9.9/plugin.schema.json","name":"x"}`,
		"ynh manifest":           `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"x","version":"1"}`,
		"missing name":           `{"$schema":"` + PluginSchemaID + `"}`,
		"name wrong type":        `{"$schema":"` + PluginSchemaID + `","name":3}`,
		"version wrong type":     `{"$schema":"` + PluginSchemaID + `","name":"x","version":1}`,
		"author extra field":     `{"$schema":"` + PluginSchemaID + `","name":"x","author":{"name":"a","twitter":"@a"}}`,
		"keywords not strings":   `{"$schema":"` + PluginSchemaID + `","name":"x","keywords":[1]}`,
		"extension not object":   `{"$schema":"` + PluginSchemaID + `","name":"x","extensions":{"com.example":"yes"}}`,
		"extensions member null": `{"$schema":"` + PluginSchemaID + `","name":"x","extensions":{"com.example":null}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := ParseManifest([]byte(data)); err == nil {
				t.Errorf("expected fatal error for %s", data)
			}
		})
	}
}

// §5.4: metadata strings are validated by JSON type only.
func TestParseManifest_MetadataNotSyntaxChecked(t *testing.T) {
	data := `{"$schema":"` + PluginSchemaID + `","name":"x","version":"not-semver","homepage":"not a url","license":"whatever","author":{"email":"not-an-email"}}`
	if _, diags, err := ParseManifest([]byte(data)); err != nil || len(diags) != 0 {
		t.Errorf("err=%v diags=%v, want clean load", err, diags)
	}
}

func TestIsPluginRoot(t *testing.T) {
	dir := t.TempDir()
	if IsPluginRoot(dir) {
		t.Error("empty dir reported as plugin root")
	}
	write(t, dir, "plugin.json", `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"h","version":"1"}`)
	if IsPluginRoot(dir) {
		t.Error("a ynh manifest at the root must not read as an Agent Plugin")
	}
	write(t, dir, "plugin.json", minimalManifest)
	if !IsPluginRoot(dir) {
		t.Error("Agent Plugins manifest not detected")
	}
}

func TestReadManifest_Missing(t *testing.T) {
	_, _, err := ReadManifest(t.TempDir())
	if err != ErrNotPlugin {
		t.Errorf("err = %v, want ErrNotPlugin", err)
	}
}

func TestReadManifest_SymlinkOutsideRootRejected(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "plugin.json", minimalManifest)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "plugin.json"), filepath.Join(dir, "plugin.json")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, _, err := ReadManifest(dir); err == nil || !strings.Contains(err.Error(), "outside the plugin root") {
		t.Errorf("err = %v, want containment rejection (§4.1)", err)
	}
}

func TestRegexpEngine_TranslatesLookahead(t *testing.T) {
	re, err := regexpEngine(`^(?!.*(?:--|\.\.))[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	for s, want := range map[string]bool{"a-b.c": true, "a--b": false, "a..b": false, "A": false, "-a": false} {
		if got := re.MatchString(s); got != want {
			t.Errorf("%q: got %v want %v", s, got, want)
		}
	}
	if _, err := regexpEngine(`(?<=x)y`); err == nil {
		t.Error("an unrelated unsupported pattern must still fail rather than be guessed at")
	}
}
