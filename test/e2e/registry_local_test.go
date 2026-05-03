//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegistry_FetchAndUpdate sets up a local file:// git registry with one
// harness and verifies `ynh registry update` parses it and `ynh search`
// surfaces the entry. This finally exercises internal/registry, which
// was at 0% E2E coverage before.
func TestRegistry_FetchAndUpdate(t *testing.T) {
	s := newSandbox(t)
	regURL := buildLocalRegistry(t, "Reg One", []registryEntry{
		{name: "harness-a", desc: "first harness from registry"},
	})

	s.mustRunYnh(t, "registry", "add", regURL)
	s.mustRunYnh(t, "registry", "update")

	out, _ := s.mustRunYnh(t, "search", "harness-a")
	if !strings.Contains(out, "harness-a") {
		t.Errorf("search did not find registry entry, got:\n%s", out)
	}
	if !strings.Contains(out, "first harness from registry") {
		t.Errorf("search did not surface description, got:\n%s", out)
	}
}

// TestInstall_FromRegistryName resolves `ynh install <name>` through the
// configured registry: clones the registry repo, navigates to the entry's
// path, installs into a namespaced directory under ~/.ynh/harnesses/.
//
// Locks the runtime registry-resolution path (cmd/ynh/install_resolve.go
// rules 4 + 6) and gives non-zero coverage to internal/namespace.
func TestInstall_FromRegistryName(t *testing.T) {
	s := newSandbox(t)
	regURL := buildLocalRegistry(t, "Reg Two", []registryEntry{
		{name: "regd", desc: "registry-installed harness"},
	})
	s.mustRunYnh(t, "registry", "add", regURL)

	s.mustRunYnh(t, "install", "regd")

	// Harness installed under a namespaced directory derived from the
	// registry URL. Locate the install via `ynh ls --format json` rather
	// than guessing the namespace path.
	out, _ := s.mustRunYnh(t, "ls", "--format", "json")
	if !strings.Contains(out, "regd") {
		t.Fatalf("expected regd in ls output, got:\n%s", out)
	}
	if !strings.Contains(out, "registry") {
		t.Errorf("expected install to record source_type=registry, got:\n%s", out)
	}

	// `ls --format json` must surface the URL-derived namespace so consumers
	// can disambiguate same-named harnesses across registries. Regression
	// guard: the field was previously dropped from the output DTO.
	wantNS := registryNamespace(regURL)
	var ls envelopeLs
	if err := json.Unmarshal([]byte(out), &ls); err != nil {
		t.Fatalf("parsing ls JSON: %v\n%s", err, out)
	}
	var found *envelopeItem
	for i := range ls.Harnesses {
		if ls.Harnesses[i].Name == "regd" {
			found = &ls.Harnesses[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("regd not in ls envelope:\n%s", out)
	}
	assertEqual(t, "harnesses[regd].namespace", found.Namespace, wantNS)

	// `search --format json` must include namespace on registry entries so
	// consumers can preview the namespace pre-install.
	searchOut, _ := s.mustRunYnh(t, "search", "regd", "--format", "json")
	var results []struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace,omitempty"`
	}
	if err := json.Unmarshal([]byte(searchOut), &results); err != nil {
		t.Fatalf("parsing search JSON: %v\n%s", err, searchOut)
	}
	if len(results) == 0 {
		t.Fatalf("search returned no results:\n%s", searchOut)
	}
	assertEqual(t, "search[0].namespace", results[0].Namespace, wantNS)
}

// TestRegistry_NamespaceCollision verifies that two registries publishing a
// harness of the same name install into distinct namespaced directories,
// so neither shadows the other. The previously-deferred namespace test —
// finally landed.
func TestRegistry_NamespaceCollision(t *testing.T) {
	s := newSandbox(t)

	regA := buildLocalRegistry(t, "Reg A", []registryEntry{
		{name: "shared", desc: "from-a"},
	})
	regB := buildLocalRegistry(t, "Reg B", []registryEntry{
		{name: "shared", desc: "from-b"},
	})

	s.mustRunYnh(t, "registry", "add", regA)
	s.mustRunYnh(t, "registry", "add", regB)

	// `ynh install shared` is ambiguous when two registries publish the same
	// name. The user must qualify with `name@<namespace-or-label>`.
	// Namespaces are URL-derived; for file:// URLs they end in the parent
	// directory's basename. Look up via the stored URL — find each by listing
	// the registry namespaces.
	nsA := registryNamespace(regA)
	nsB := registryNamespace(regB)

	s.mustRunYnh(t, "install", "shared@"+nsA)
	s.mustRunYnh(t, "install", "shared@"+nsB)

	// Both installations must be visible in ls.
	out, _ := s.mustRunYnh(t, "ls")
	count := strings.Count(out, "shared")
	if count < 2 {
		t.Errorf("expected two shared harnesses (one per registry), got %d in:\n%s", count, out)
	}
}

// registryEntry describes a harness to seed into a local registry.
type registryEntry struct {
	name string
	desc string
}

// buildLocalRegistry writes a git registry with one or more harness entries
// and returns the file:// URL pointing at it. Each entry's source is a
// relative path inside the registry repo.
func buildLocalRegistry(t *testing.T, regName string, entries []registryEntry) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "registry")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}

	var harnessJSON strings.Builder
	for i, e := range entries {
		hdir := filepath.Join(dir, e.name)
		if err := os.MkdirAll(filepath.Join(hdir, ".ynh-plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":%q,"version":"0.1.0","description":%q}`, e.name, e.desc)
		if err := os.WriteFile(filepath.Join(hdir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			harnessJSON.WriteString(",")
		}
		fmt.Fprintf(&harnessJSON, `{"name":%q,"source":%q,"description":%q}`, e.name, e.name, e.desc)
	}

	mp := fmt.Sprintf(`{"name":%q,"owner":{"name":"e2e","email":"e2e@example.invalid"},"harnesses":[%s]}`, regName, harnessJSON.String())
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "marketplace.json"), []byte(mp), 0o644); err != nil {
		t.Fatal(err)
	}

	mustGit(t, dir, "init", "--quiet", "--initial-branch=main")
	mustGit(t, dir, "config", "user.email", "e2e@example.invalid")
	mustGit(t, dir, "config", "user.name", "e2e")
	mustGit(t, dir, "config", "uploadpack.allowReachableSHA1InWant", "true")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "--quiet", "-m", "init registry")

	return "file://" + dir
}

// registryNamespace returns the URL-derived namespace ynh uses for a registry —
// for file:// URLs, the last two path segments joined with "/". Mirrors
// namespace.DeriveFromURL behaviour for tests.
func registryNamespace(url string) string {
	u := strings.TrimPrefix(url, "file://")
	u = strings.TrimSuffix(u, "/")
	parts := strings.Split(u, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return parts[len(parts)-1]
}
