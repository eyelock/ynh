package marketplace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/vendor"
)

const registryFixture = "../../testdata/registry-build/marketplace.json"

// A golden per vendor: the index each vendor gets from the registry index
// (ynh #395). Regenerate with UPDATE_GOLDEN=1 and review the diff.
func TestBuildFromRegistryIndex_GoldenPerVendor(t *testing.T) {
	idx, err := LoadRegistryIndex(registryFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		vendor, rel string
		// refuses is the entry a vendor cannot carry, "" when it carries all.
		refuses string
	}{
		{"claude", ".claude-plugin/marketplace.json", ""},
		{"cursor", ".cursor-plugin/marketplace.json", ""},
		{"copilot", ".github/plugin/marketplace.json", ""},
		{"codex", ".agents/plugins/marketplace.json", "formatter"},
		{"opencode", "", ""},
	} {
		t.Run(tc.vendor, func(t *testing.T) {
			out := t.TempDir()
			entries := idx.Entries
			if tc.refuses != "" {
				// Codex lists local plugins only: give it the local entries.
				var local []vendor.MarketplacePluginInfo
				for _, e := range entries {
					if e.Source.Type == "local" {
						local = append(local, e)
					}
				}
				entries = local
			}
			sub := *idx
			sub.Entries = entries
			written, err := BuildFromRegistryIndex(&sub, RegistryBuildOptions{OutputDir: out, Vendors: []string{tc.vendor}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.rel == "" {
				if len(written) != 0 {
					t.Errorf("%s has no index, wrote %v", tc.vendor, written)
				}
				return
			}
			got, err := os.ReadFile(filepath.Join(out, tc.rel))
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "registry-"+tc.vendor+".golden.json")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				_ = os.MkdirAll("testdata", 0o755)
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("no golden (UPDATE_GOLDEN=1 writes it): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("%s index differs from its golden:\n%s\nwant:\n%s", tc.vendor, got, want)
			}
		})
	}
}

// A source a vendor cannot carry is refused, naming the entry and the vendor,
// and nothing is written (D4).
func TestBuildFromRegistryIndex_RefusesASourceAVendorCannotCarry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents", "harness", "marketplace.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"$schema":"https://eyelock.github.io/ynh/schema/marketplace.schema.json","name":"a","owner":{"name":"x"},"harnesses":[],
	"plugins":[{"name":"deep","source":{"type":"git-subdir","url":"https://git.test/r.git","path":"p/deep"}}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := LoadRegistryIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	// Copilot CLI cannot install from git-subdir.
	out := t.TempDir()
	_, err = BuildFromRegistryIndex(idx, RegistryBuildOptions{OutputDir: out, Vendors: []string{"claude", "copilot"}})
	var us *vendor.UnsupportedSourceError
	if !errors.As(err, &us) || us.Entry != "deep" || us.Vendor != "Copilot CLI" || !strings.Contains(err.Error(), `"deep"`) || !strings.Contains(err.Error(), "copilot") {
		t.Fatalf("err = %v, want a refusal naming the entry and the vendor", err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Errorf("a refused build wrote %d entries, claude's among them", len(entries))
	}
	// Claude Code carries it.
	if _, err := BuildFromRegistryIndex(idx, RegistryBuildOptions{OutputDir: out, Vendors: []string{"claude"}}); err != nil {
		t.Errorf("claude: %v", err)
	}
	// Codex lists local plugins only.
	if _, err := BuildFromRegistryIndex(idx, RegistryBuildOptions{OutputDir: t.TempDir(), Vendors: []string{"codex"}}); !errors.As(err, &us) || us.Vendor != "Codex" {
		t.Errorf("codex: %v", err)
	}
}

func TestLoadRegistryIndex_Checks(t *testing.T) {
	write := func(body string) string {
		p := filepath.Join(t.TempDir(), "marketplace.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for name, tc := range map[string]struct{ body, want string }{
		"no name":     {`{"owner":{"name":"x"},"harnesses":[{"name":"h","source":"./h"}]}`, "name is required"},
		"no owner":    {`{"name":"a","harnesses":[{"name":"h","source":"./h"}]}`, "owner.name"},
		"empty":       {`{"name":"a","owner":{"name":"x"},"harnesses":[]}`, "at least one"},
		"twice":       {`{"name":"a","owner":{"name":"x"},"harnesses":[{"name":"h","source":"./h"}],"plugins":[{"name":"h","source":"./p"}]}`, "listed twice"},
		"no dotslash": {`{"name":"a","owner":{"name":"x"},"harnesses":[{"name":"h","source":"h"}]}`, "must start with ./"},
		"bad type":    {`{"name":"a","owner":{"name":"x"},"harnesses":[{"name":"h","source":{"type":"npm"}}]}`, "not github"},
	} {
		if _, err := LoadRegistryIndex(write(tc.body)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}
