// Package backend resolves and applies local model backends (e.g. Ollama)
// for vendor CLIs launched by `ynh run`. A backend redirects a vendor's CLI
// at an alternate model server instead of its default cloud API, by
// injecting environment variables or vendor-native config before launch.
//
// A backend is selected as part of the ordinary vendor spec, not a separate
// flag: "-v claude" launches plain Claude Code, "-v ollama/claude/qwen3"
// launches Claude Code redirected at the "ollama" backend's connection for
// the "claude" vendor, with model "qwen3". This keeps a single flag/env/
// config surface (-v, YNH_VENDOR, default_vendor) for both plain and
// backend-redirected launches.
package backend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/config"
)

// Spec is a parsed vendor spec string.
type Spec struct {
	Vendor  string // always set
	Backend string // "" if this spec names a plain vendor with no backend
	Model   string // "" if unset; only meaningful when Backend != ""
}

// ParseSpec parses a vendor spec string in one of three forms:
//
//	"<vendor>"                    - plain vendor, no backend
//	"<backend>/<vendor>"          - backend-redirected, vendor's default model
//	"<backend>/<vendor>/<model>"  - backend-redirected, explicit model
//
// The model segment is everything after the second "/", so a namespaced
// model name containing its own "/" (e.g. "org/model:tag") still parses
// correctly rather than being split further.
func ParseSpec(spec string) (Spec, error) {
	parts := strings.SplitN(spec, "/", 3)
	switch len(parts) {
	case 1:
		return Spec{Vendor: parts[0]}, nil
	case 2:
		return Spec{Backend: parts[0], Vendor: parts[1]}, nil
	default:
		return Spec{Backend: parts[0], Vendor: parts[1], Model: parts[2]}, nil
	}
}

// Lookup finds the connection details for spec.Backend + spec.Vendor in cfg.
// Only meaningful when spec.Backend != "".
func Lookup(cfg *config.Config, spec Spec) (config.BackendConnection, error) {
	def, ok := cfg.Backends[spec.Backend]
	if !ok {
		var available []string
		for k := range cfg.Backends {
			available = append(available, k)
		}
		return config.BackendConnection{}, fmt.Errorf("unknown backend %q (available: %v)", spec.Backend, available)
	}
	conn, ok := def.Vendors[spec.Vendor]
	if !ok {
		return config.BackendConnection{}, fmt.Errorf("backend %q has no config for vendor %q (add backends.%s.vendors.%s to ~/.ynh/config.json)", spec.Backend, spec.Vendor, spec.Backend, spec.Vendor)
	}
	return conn, nil
}

// modelsHTTPClient is overridden in tests to avoid real network calls / long
// default timeouts.
var modelsHTTPClient = &http.Client{Timeout: 2 * time.Second}

// ollamaOrigin derives an Ollama server's root origin from one of its
// per-vendor base_urls, which may carry a vendor-specific "/v1/" (or "/v1")
// suffix that Ollama's native API (unlike its OpenAI-compat endpoint)
// doesn't use.
func ollamaOrigin(baseURL string) string {
	origin := strings.TrimRight(baseURL, "/")
	origin = strings.TrimSuffix(origin, "/v1")
	return origin
}

// ListModels queries backendName's server for installed models, using
// whichever vendor connection is configured for it. Only backends with
// Type == "ollama" support this; other types (or unrecognized/empty types)
// return an error, since ynh has no known API to query them.
func ListModels(cfg *config.Config, backendName string) ([]string, error) {
	def, ok := cfg.Backends[backendName]
	if !ok {
		return nil, fmt.Errorf("unknown backend %q", backendName)
	}
	if def.Type != "ollama" {
		return nil, fmt.Errorf("backend %q has no known model-listing API (type %q)", backendName, def.Type)
	}

	if len(def.Vendors) == 0 {
		return nil, fmt.Errorf("backend %q has no vendor connections configured", backendName)
	}
	var vendorNames []string
	for name := range def.Vendors {
		vendorNames = append(vendorNames, name)
	}
	sort.Strings(vendorNames)
	conn := def.Vendors[vendorNames[0]]

	resp, err := modelsHTTPClient.Get(ollamaOrigin(conn.BaseURL) + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("querying backend %q for models: %w", backendName, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backend %q model listing returned HTTP %d", backendName, resp.StatusCode)
	}

	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding model list from backend %q: %w", backendName, err)
	}

	names := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names, nil
}

// Apply redirects vendorName's CLI at the given backend connection,
// returning extra args to prepend to the vendor invocation. Env vars are set
// on the current process so they're inherited by the vendor CLI at launch
// (syscall.Exec and exec.Command both pick up os.Environ() at launch time,
// after Apply runs).
func Apply(vendorName string, backendName string, conn config.BackendConnection, model string) ([]string, error) {
	switch vendorName {
	case "claude":
		return applyClaude(conn, model)
	case "codex":
		return applyCodex(backendName, conn, model)
	default:
		return nil, fmt.Errorf("backend %q: vendor %q has no known local-model redirection", backendName, vendorName)
	}
}

func applyClaude(conn config.BackendConnection, model string) ([]string, error) {
	if err := os.Setenv("ANTHROPIC_BASE_URL", conn.BaseURL); err != nil {
		return nil, fmt.Errorf("setting ANTHROPIC_BASE_URL: %w", err)
	}
	if err := os.Setenv("ANTHROPIC_AUTH_TOKEN", conn.AuthToken); err != nil {
		return nil, fmt.Errorf("setting ANTHROPIC_AUTH_TOKEN: %w", err)
	}
	// Clear any real API key so it can't leak to the local server alongside
	// the auth token above.
	if err := os.Setenv("ANTHROPIC_API_KEY", ""); err != nil {
		return nil, fmt.Errorf("setting ANTHROPIC_API_KEY: %w", err)
	}
	for k, v := range conn.Env {
		if err := os.Setenv(k, v); err != nil {
			return nil, fmt.Errorf("setting %s: %w", k, err)
		}
	}

	// Set the model via ANTHROPIC_MODEL rather than passing --model on the
	// command line. Claude Code treats a --model flag (or /model command) as
	// an explicit user choice and persists it to ~/.claude/settings.json as
	// the default for all future sessions — which would leak this backend's
	// model (e.g. "qwen3") into unrelated, non-redirected sessions launched
	// afterward. ANTHROPIC_MODEL only affects the current process.
	if model != "" {
		if err := os.Setenv("ANTHROPIC_MODEL", model); err != nil {
			return nil, fmt.Errorf("setting ANTHROPIC_MODEL: %w", err)
		}
	}
	return nil, nil
}

func applyCodex(backendName string, conn config.BackendConnection, model string) ([]string, error) {
	for k, v := range conn.Env {
		if err := os.Setenv(k, v); err != nil {
			return nil, fmt.Errorf("setting %s: %w", k, err)
		}
	}

	if err := ensureCodexProvider(backendName, conn); err != nil {
		return nil, err
	}

	args := []string{"-c", "model_provider=" + backendName}
	if model != "" {
		args = append(args, "-c", "model="+model)
	}
	return args, nil
}

// codexConfigPath returns the path to Codex's own config.toml, which lives
// under the user's real home directory — distinct from YNH_HOME.
func codexConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

var tomlSectionHeader = regexp.MustCompile(`^\[(.+)\]$`)

// ensureCodexProvider idempotently writes a [model_providers.<name>] section
// into ~/.codex/config.toml, replacing any existing section with the same
// name and leaving all other content untouched. Codex has no config.toml
// writer of its own in ynh today, so this is a minimal section-level
// read/replace/write rather than a general TOML library (zero external deps).
func ensureCodexProvider(name string, conn config.BackendConnection) error {
	path, err := codexConfigPath()
	if err != nil {
		return err
	}

	var existing string
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	header := "[model_providers." + name + "]"
	var body strings.Builder
	body.WriteString(header + "\n")
	body.WriteString("name = " + tomlQuote(name) + "\n")
	body.WriteString("base_url = " + tomlQuote(conn.BaseURL) + "\n")
	body.WriteString("wire_api = \"responses\"\n")

	updated := upsertTOMLSection(existing, header, body.String())

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// upsertTOMLSection replaces the section starting with header (up to but not
// including the next top-level "[...]" line, or EOF) with newSection. If no
// such section exists, newSection is appended, separated by a blank line
// from any existing content.
func upsertTOMLSection(content string, header string, newSection string) string {
	lines := strings.Split(content, "\n")

	start := -1
	end := len(lines)
	for i, line := range lines {
		if strings.TrimSpace(line) == header {
			start = i
			for j := i + 1; j < len(lines); j++ {
				if tomlSectionHeader.MatchString(strings.TrimSpace(lines[j])) {
					end = j
					break
				}
			}
			break
		}
	}

	newSection = strings.TrimRight(newSection, "\n")

	if start == -1 {
		trimmed := strings.TrimRight(content, "\n")
		if trimmed == "" {
			return newSection + "\n"
		}
		return trimmed + "\n\n" + newSection + "\n"
	}

	var out []string
	out = append(out, lines[:start]...)
	out = append(out, newSection)
	out = append(out, lines[end:]...)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

func tomlQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
