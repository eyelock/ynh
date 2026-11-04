package vendor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func approvalRunDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	doc := `{"mcpServers":{"alpha":{"command":"echo"},"beta":{"url":"http://127.0.0.1:1/mcp"},"old":{"type":"sse","url":"http://x/sse"},"bad.name":{"command":"x"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCodexApproveMCPTools_HarnessServersOnly(t *testing.T) {
	got := codexApproveMCPTools(approvalRunDir(t), nil, "", nil)
	want := []string{
		"-c", `mcp_servers.alpha.default_tools_approval_mode="approve"`,
		"-c", `mcp_servers.beta.default_tools_approval_mode="approve"`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCodexApproveMCPTools_KeepsUserChoice(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(cfg, []byte("[mcp_servers.alpha]\ndefault_tools_approval_mode = \"prompt\" # mine\n"), 0o644)
	var warn strings.Builder
	got := codexApproveMCPTools(approvalRunDir(t), []string{"-c", `mcp_servers.beta.default_tools_approval_mode="writes"`}, cfg, &warn)
	if len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
	if !strings.Contains(warn.String(), `"alpha"`) || !strings.Contains(warn.String(), `"beta"`) {
		t.Errorf("warn = %q", warn.String())
	}
}

func TestCodexApproveMCPTools_NoServers(t *testing.T) {
	if got := codexApproveMCPTools(t.TempDir(), nil, "", nil); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestCodexConfigSetsApproval_DottedAndOtherServer(t *testing.T) {
	if !codexConfigSetsApproval("mcp_servers.a.default_tools_approval_mode = \"approve\"\n", "a") {
		t.Error("dotted key missed")
	}
	if codexConfigSetsApproval("[mcp_servers.other]\ndefault_tools_approval_mode = \"prompt\"\n", "a") {
		t.Error("other server matched")
	}
}
