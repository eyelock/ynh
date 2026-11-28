package harness

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/permission"
	"github.com/eyelock/ynh/internal/plugin"
)

func TestAutoApproveLoadsFromManifest(t *testing.T) {
	h := loadTransportHarness(t, `{"name":"h","version":"0.1.0","auto_approve":"edits","delegates_to":[{"git":"github.com/o/a","auto_approve":"none"},{"git":"github.com/o/b"}]}`)
	if h.AutoApprove != "edits" || h.AutoApproveOf() != permission.Edits || h.AutoApproveOrigin() != permission.SourceHarness {
		t.Errorf("harness level = %q (%s)", h.AutoApproveOf(), h.AutoApproveOrigin())
	}
	if h.DelegatesTo[0].AutoApprove != "none" || h.DelegatesTo[1].AutoApprove != "" {
		t.Errorf("entry levels = %q, %q", h.DelegatesTo[0].AutoApprove, h.DelegatesTo[1].AutoApprove)
	}
	if err := h.CheckAutoApprove(); err != nil {
		t.Error(err)
	}
}

func TestUndeclaredAutoApproveOpensAtAll(t *testing.T) {
	h := loadTransportHarness(t, `{"name":"h","version":"0.1.0"}`)
	if h.AutoApproveOf() != permission.All || h.AutoApproveOrigin() != permission.SourceDefault {
		t.Errorf("undeclared = %q (%s)", h.AutoApproveOf(), h.AutoApproveOrigin())
	}
}

func TestInvalidAutoApproveIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"harness": `{"name":"h","version":"0.1.0","auto_approve":"everything"}`,
		"entry":   `{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a","auto_approve":"ALL"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := loadTransportHarness(t, body)
			err := h.CheckDelegateTransports("claude")
			if err == nil || !strings.Contains(err.Error(), "unknown auto-approve level") {
				t.Errorf("want a refusal naming the valid levels, got %v", err)
			}
		})
	}
}

func TestAutoApproveOnANativeEntryIsRefusedAtLoad(t *testing.T) {
	for name, body := range map[string]string{
		"entry transport native":   `{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a","transport":"native","auto_approve":"none"}]}`,
		"harness transport native": `{"name":"h","version":"0.1.0","transport":"native","delegates_to":[{"git":"github.com/o/a","auto_approve":"edits"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := writePluginJSONFile(dir, []byte(body)); err != nil {
				t.Fatal(err)
			}
			// Every loader refuses it, so every path that loads a harness does:
			// run, agent run, trust accept and show, check, update, info, image.
			_, err := LoadDir(dir)
			if err == nil || !strings.Contains(err.Error(), "not accepted on a native delegate") || !strings.Contains(err.Error(), "transport: a2a") {
				t.Errorf("LoadDir: want a refusal naming the parent's level and a2a as the fix, got %v", err)
			}
			_, ferr := LoadFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile))
			if ferr == nil || !strings.Contains(ferr.Error(), "not accepted on a native delegate") {
				t.Errorf("LoadFile: want the same refusal, got %v", ferr)
			}
		})
	}
	// An a2a entry's level, and a native entry with none, are still accepted.
	for name, body := range map[string]string{
		"a2a entry":     `{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a","auto_approve":"none"}]}`,
		"native, none":  `{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a","transport":"native"}]}`,
		"entry a2a/nat": `{"name":"h","version":"0.1.0","transport":"native","delegates_to":[{"git":"github.com/o/a","transport":"a2a","auto_approve":"none"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := loadTransportHarness(t, body).CheckDelegateTransports("claude"); err != nil {
				t.Errorf("must still load: %v", err)
			}
		})
	}
}
