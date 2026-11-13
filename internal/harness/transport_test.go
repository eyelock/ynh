package harness

import (
	"strings"
	"testing"
)

func loadTransportHarness(t *testing.T, body string) *Harness {
	t.Helper()
	dir := t.TempDir()
	if err := writePluginJSONFile(dir, []byte(body)); err != nil {
		t.Fatal(err)
	}
	h, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestTransportOf(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"neither says: a2a",
			`{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a"}]}`,
			[]string{"a2a"}},
		{"harness default",
			`{"name":"h","version":"0.1.0","transport":"native","delegates_to":[{"git":"github.com/o/a"}]}`,
			[]string{"native"}},
		{"entry value",
			`{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a","transport":"native"}]}`,
			[]string{"native"}},
		{"entry wins over the harness default",
			`{"name":"h","version":"0.1.0","transport":"native","delegates_to":[{"git":"github.com/o/a","transport":"a2a"}]}`,
			[]string{"a2a"}},
		{"a mix",
			`{"name":"h","version":"0.1.0","transport":"a2a","delegates_to":[{"git":"github.com/o/a","transport":"native"},{"git":"github.com/o/b"},{"git":"github.com/o/c","transport":"a2a"}]}`,
			[]string{"native", "a2a", "a2a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := loadTransportHarness(t, tc.body)
			if len(h.DelegatesTo) != len(tc.want) {
				t.Fatalf("got %d delegates, want %d", len(h.DelegatesTo), len(tc.want))
			}
			for i, d := range h.DelegatesTo {
				if got := h.TransportOf(d); got != tc.want[i] {
					t.Errorf("delegate %d: transport %q, want %q", i, got, tc.want[i])
				}
			}
		})
	}
}

func TestCheckDelegateTransports(t *testing.T) {
	native := `{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a","transport":"native"}]}`
	a2a := `{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a"}]}`

	t.Run("native refused on cursor, with the fix", func(t *testing.T) {
		err := loadTransportHarness(t, native).CheckDelegateTransports("cursor")
		if err == nil {
			t.Fatal("native on cursor must be refused")
		}
		for _, want := range []string{"transport native", "cursor has no native subagents", "Fix: set transport: a2a on this delegate, or run on a vendor with subagents"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should contain %q:\n%v", want, err)
			}
		}
	})
	t.Run("native by the harness default is refused too", func(t *testing.T) {
		h := loadTransportHarness(t, `{"name":"h","version":"0.1.0","transport":"native","delegates_to":[{"git":"github.com/o/a"}]}`)
		if err := h.CheckDelegateTransports("cursor"); err == nil {
			t.Error("a harness default of native on cursor must be refused")
		}
	})
	t.Run("native allowed where subagents exist", func(t *testing.T) {
		for _, v := range []string{"claude", "codex", "copilot", "opencode"} {
			if err := loadTransportHarness(t, native).CheckDelegateTransports(v); err != nil {
				t.Errorf("%s: %v", v, err)
			}
		}
	})
	t.Run("a2a allowed on cursor", func(t *testing.T) {
		if err := loadTransportHarness(t, a2a).CheckDelegateTransports("cursor"); err != nil {
			t.Error(err)
		}
	})
	t.Run("an entry's a2a overrides a native default on cursor", func(t *testing.T) {
		h := loadTransportHarness(t, `{"name":"h","version":"0.1.0","transport":"native","delegates_to":[{"git":"github.com/o/a","transport":"a2a"}]}`)
		if err := h.CheckDelegateTransports("cursor"); err != nil {
			t.Error(err)
		}
	})
	t.Run("an unknown value is refused", func(t *testing.T) {
		h := loadTransportHarness(t, `{"name":"h","version":"0.1.0","delegates_to":[{"git":"github.com/o/a","transport":"carrier-pigeon"}]}`)
		if err := h.CheckDelegateTransports("claude"); err == nil || !strings.Contains(err.Error(), "not a transport") {
			t.Errorf("want a not-a-transport error, got %v", err)
		}
	})
}
