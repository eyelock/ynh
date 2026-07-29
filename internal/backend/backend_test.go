package backend

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

func TestParseSpec(t *testing.T) {
	tests := []struct {
		spec string
		want Spec
	}{
		{"claude", Spec{Vendor: "claude"}},
		{"ollama/claude", Spec{Backend: "ollama", Vendor: "claude"}},
		{"ollama/claude/qwen3", Spec{Backend: "ollama", Vendor: "claude", Model: "qwen3"}},
		{"ollama/claude/org/model:tag", Spec{Backend: "ollama", Vendor: "claude", Model: "org/model:tag"}},
		{"", Spec{Vendor: ""}}, // single empty segment, not an error at this layer
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := ParseSpec(tt.spec)
			if err != nil {
				t.Fatalf("ParseSpec(%q) failed: %v", tt.spec, err)
			}
			if got != tt.want {
				t.Errorf("ParseSpec(%q) = %+v, want %+v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestLookup(t *testing.T) {
	cfg := &config.Config{
		Backends: map[string]config.BackendDef{
			"ollama": {
				Type: "ollama",
				Vendors: map[string]config.BackendConnection{
					"claude": {BaseURL: "http://localhost:11434", AuthToken: "ollama"},
				},
			},
		},
	}

	t.Run("found", func(t *testing.T) {
		conn, err := Lookup(cfg, Spec{Backend: "ollama", Vendor: "claude"})
		if err != nil {
			t.Fatalf("Lookup failed: %v", err)
		}
		if conn.BaseURL != "http://localhost:11434" {
			t.Errorf("BaseURL = %q", conn.BaseURL)
		}
	})

	t.Run("unknown backend", func(t *testing.T) {
		if _, err := Lookup(cfg, Spec{Backend: "does-not-exist", Vendor: "claude"}); err == nil {
			t.Fatal("expected error for unknown backend")
		}
	})

	t.Run("unknown vendor for known backend", func(t *testing.T) {
		if _, err := Lookup(cfg, Spec{Backend: "ollama", Vendor: "codex"}); err == nil {
			t.Fatal("expected error for backend with no config for this vendor")
		}
	})
}

func TestOllamaOrigin(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"http://localhost:11434", "http://localhost:11434"},
		{"http://localhost:11434/", "http://localhost:11434"},
		{"http://localhost:11434/v1/", "http://localhost:11434"},
		{"http://localhost:11434/v1", "http://localhost:11434"},
	}
	for _, tt := range tests {
		if got := ollamaOrigin(tt.baseURL); got != tt.want {
			t.Errorf("ollamaOrigin(%q) = %q, want %q", tt.baseURL, got, tt.want)
		}
	}
}

func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:latest"},{"name":"gpt-oss:120b"}]}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		Backends: map[string]config.BackendDef{
			"ollama": {
				Type: "ollama",
				Vendors: map[string]config.BackendConnection{
					"claude": {BaseURL: server.URL},
					"codex":  {BaseURL: server.URL + "/v1/"},
				},
			},
			"untyped": {
				Vendors: map[string]config.BackendConnection{
					"claude": {BaseURL: server.URL},
				},
			},
		},
	}

	t.Run("lists and sorts models", func(t *testing.T) {
		models, err := ListModels(cfg, "ollama")
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		want := []string{"gpt-oss:120b", "qwen3:latest"}
		if strings.Join(models, ",") != strings.Join(want, ",") {
			t.Errorf("models = %v, want %v", models, want)
		}
	})

	t.Run("unrecognized type errors", func(t *testing.T) {
		if _, err := ListModels(cfg, "untyped"); err == nil {
			t.Fatal("expected error for backend with no recognized type")
		}
	})

	t.Run("unknown backend errors", func(t *testing.T) {
		if _, err := ListModels(cfg, "does-not-exist"); err == nil {
			t.Fatal("expected error for unknown backend")
		}
	})
}

func TestApplyClaude(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_API_KEY", "should-be-cleared")

	conn := config.BackendConnection{
		BaseURL:   "http://localhost:11434",
		AuthToken: "ollama",
		Env:       map[string]string{"EXTRA_VAR": "extra-value"},
	}

	args, err := Apply("claude", "ollama", conn, "qwen3")
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	wantArgs := []string{"--model", "qwen3"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}

	if v := os.Getenv("ANTHROPIC_BASE_URL"); v != "http://localhost:11434" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", v)
	}
	if v := os.Getenv("ANTHROPIC_AUTH_TOKEN"); v != "ollama" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q", v)
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want cleared", v)
	}
	if v := os.Getenv("EXTRA_VAR"); v != "extra-value" {
		t.Errorf("EXTRA_VAR = %q", v)
	}
}

func TestApplyClaudeNoModel(t *testing.T) {
	conn := config.BackendConnection{BaseURL: "http://localhost:11434", AuthToken: "ollama"}
	args, err := Apply("claude", "ollama", conn, "")
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want none when model is unset", args)
	}
}

func TestApplyCodexWritesConfigToml(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	conn := config.BackendConnection{BaseURL: "http://localhost:11434/v1/"}

	args, err := Apply("codex", "ollama", conn, "gpt-oss:120b")
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	wantArgs := []string{"-c", "model_provider=ollama", "-c", "model=gpt-oss:120b"}
	if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}

	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("reading config.toml: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[model_providers.ollama]") {
		t.Errorf("config.toml missing provider section:\n%s", content)
	}
	if !strings.Contains(content, `base_url = "http://localhost:11434/v1/"`) {
		t.Errorf("config.toml missing base_url:\n%s", content)
	}
	if !strings.Contains(content, `wire_api = "responses"`) {
		t.Errorf("config.toml missing wire_api:\n%s", content)
	}
}

func TestApplyCodexIsIdempotentAndPreservesOtherContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	preexisting := "profile = \"default\"\n\n[model_providers.other]\nname = \"other\"\nbase_url = \"https://example.com\"\n"
	if err := os.WriteFile(configPath, []byte(preexisting), 0o644); err != nil {
		t.Fatal(err)
	}

	conn := config.BackendConnection{BaseURL: "http://localhost:11434/v1/"}
	if _, err := Apply("codex", "ollama", conn, "qwen3"); err != nil {
		t.Fatalf("first Apply failed: %v", err)
	}
	if _, err := Apply("codex", "ollama", conn, "gpt-oss:120b"); err != nil {
		t.Fatalf("second Apply failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config.toml: %v", err)
	}
	content := string(data)

	if strings.Count(content, "[model_providers.ollama]") != 1 {
		t.Errorf("expected exactly one ollama section, got:\n%s", content)
	}
	if !strings.Contains(content, "[model_providers.other]") {
		t.Errorf("pre-existing provider section was lost:\n%s", content)
	}
	if !strings.Contains(content, "profile = \"default\"") {
		t.Errorf("pre-existing top-level content was lost:\n%s", content)
	}
	if !strings.Contains(content, `base_url = "https://example.com"`) {
		t.Errorf("other provider's base_url was clobbered:\n%s", content)
	}
}

func TestApplyUnsupportedVendorErrors(t *testing.T) {
	_, err := Apply("cursor", "ollama", config.BackendConnection{}, "")
	if err == nil {
		t.Fatal("expected error for unsupported vendor")
	}
}
