package harness

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/permission"
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
