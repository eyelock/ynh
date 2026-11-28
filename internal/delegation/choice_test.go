package delegation

import (
	"reflect"
	"strings"
	"testing"
)

// Each layer wins over the one below it, and a layer that names nothing is
// passed over.
func TestResolveEachLayerOverridesTheNext(t *testing.T) {
	tests := []struct {
		name               string
		call, entry, deflt string
		want               Setting
	}{
		{"call over entry, delegate default and vendor", "c", "e", "d", Setting{"c", FromCall}},
		{"entry over delegate default and vendor", "", "e", "d", Setting{"e", FromEntry}},
		{"delegate default over vendor", "", "", "d", Setting{"d", FromDelegateDefault}},
		{"vendor default when nothing is named", "", "", "", Setting{"", FromVendorDefault}},
		{"call alone", "c", "", "", Setting{"c", FromCall}},
		{"call over delegate default", "c", "", "d", Setting{"c", FromCall}},
		{"entry alone", "", "e", "", Setting{"e", FromEntry}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.call, tt.entry, tt.deflt); got != tt.want {
				t.Errorf("Resolve(%q, %q, %q) = %+v, want %+v", tt.call, tt.entry, tt.deflt, got, tt.want)
			}
		})
	}
}

func TestCheckModel(t *testing.T) {
	for _, ok := range []string{"", "opus", "claude-opus-4-8", "gpt-5.5", "provider/model#variant"} {
		if err := CheckModel(ok); err != nil {
			t.Errorf("CheckModel(%q) = %v, want none", ok, err)
		}
	}
	for _, bad := range []string{"--dangerously-skip-permissions", "-m", "two words", "tab\there", "nl\n", strings.Repeat("x", 201)} {
		if err := CheckModel(bad); err == nil {
			t.Errorf("CheckModel(%q) = nil, want an error", bad)
		}
	}
}

func TestCheckEffort(t *testing.T) {
	for _, ok := range []string{"", "low", "medium", "high"} {
		if err := CheckEffort(ok); err != nil {
			t.Errorf("CheckEffort(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"max", "xhigh", "HIGH", "fast"} {
		if err := CheckEffort(bad); err == nil {
			t.Errorf("CheckEffort(%q) = nil, want an error", bad)
		}
	}
}

func TestVendorEffortArgs(t *testing.T) {
	if got, err := VendorEffortArgs("claude", "high"); err != nil || !reflect.DeepEqual(got, []string{"--effort", "high"}) {
		t.Errorf("claude = %q, %v", got, err)
	}
	if got, err := VendorEffortArgs("codex", "low"); err != nil || !reflect.DeepEqual(got, []string{"-c", `model_reasoning_effort="low"`}) {
		t.Errorf("codex = %q, %v", got, err)
	}
	if got, err := VendorEffortArgs("cursor", ""); err != nil || got != nil {
		t.Errorf("no effort must be no arguments on any vendor: %q, %v", got, err)
	}
	for _, v := range []string{"cursor", "opencode", "copilot", ""} {
		if _, err := VendorEffortArgs(v, "high"); err == nil || !strings.Contains(err.Error(), "cannot be honoured") {
			t.Errorf("%s: want a refusal, got %v", v, err)
		}
	}
}
