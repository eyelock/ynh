package vendor

import (
	"encoding/json"
	"testing"
)

// Cursor's marketplace index is not Claude's.
//
// `GenerateMarketplaceIndex` was a byte-for-byte copy of Claude's, so Cursor
// shipped Claude's shape: a top-level `description` and a per-plugin `version`,
// neither of which appears in Cursor's documented format
// (`references/cursor.md:127`) or in this repository's own committed
// `.cursor-plugin/marketplace.json`. Nothing consumed the generated Cursor
// index, so nothing noticed.
func TestCursorMarketplaceIndexShape(t *testing.T) {
	c := &Cursor{}
	data, err := c.GenerateMarketplaceIndex(
		MarketplaceIndexConfig{Name: "m", OwnerName: "o", Description: "d"},
		[]MarketplacePluginInfo{{Name: "p", Description: "pd", Version: "1.2.3"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, data)
	}

	if _, ok := got["description"]; ok {
		t.Error("description must nest under metadata, not sit at the top level")
	}
	meta, ok := got["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("no metadata object: %s", data)
	}
	if meta["description"] != "d" {
		t.Errorf("metadata.description = %v, want %q", meta["description"], "d")
	}

	plugins, _ := got["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("want 1 plugin, got %d", len(plugins))
	}
	p, _ := plugins[0].(map[string]any)
	if _, ok := p["version"]; ok {
		t.Error("Cursor's documented plugin entry carries no version")
	}
	// The source points where `ynd marketplace build` actually puts the plugin.
	// The reference's bare "plugin-name" describes a marketplace whose plugins
	// sit at its root; build copies them into ./plugins/.
	if p["source"] != "./plugins/p" {
		t.Errorf("source = %v, want ./plugins/p", p["source"])
	}
}

// And Claude's is unchanged — the fix must not swap the two.
func TestClaudeMarketplaceIndexKeepsItsShape(t *testing.T) {
	c := &Claude{}
	data, err := c.GenerateMarketplaceIndex(
		MarketplaceIndexConfig{Name: "m", OwnerName: "o", Description: "d"},
		[]MarketplacePluginInfo{{Name: "p", Description: "pd", Version: "1.2.3"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["description"] != "d" {
		t.Errorf("Claude keeps a top-level description, got %v", got["description"])
	}
	if _, ok := got["metadata"]; ok {
		t.Error("Claude has no metadata object")
	}
	p := got["plugins"].([]any)[0].(map[string]any)
	if p["version"] != "1.2.3" {
		t.Errorf("Claude keeps a per-plugin version, got %v", p["version"])
	}
}
