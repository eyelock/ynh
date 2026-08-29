package agent

import "testing"

// `--sandbox srt` was read only by the Claude backend; codex and cursor
// contained no reference to it. `--sandbox srt --backend codex` therefore ran
// completely unsandboxed and reported nothing — the operator believed a
// containment control was in force that was never applied.
func TestValidateSandbox(t *testing.T) {
	cases := []struct {
		sandbox, backend string
		wantErr          bool
		why              string
	}{
		{"", "claude", false, "unset means none"},
		{"none", "codex", false, "explicitly unsandboxed is a choice, not a failure"},
		{"srt", "claude", false, "claude implements srt"},
		{"srt", "codex", true, "codex cannot honour srt and must not pretend to"},
		{"srt", "cursor", true, "cursor cannot honour srt and must not pretend to"},
		{"docker", "claude", true, "an unknown sandbox is not silently ignored"},
	}
	for _, c := range cases {
		err := validateSandbox(c.sandbox, c.backend)
		if c.wantErr && err == nil {
			t.Errorf("validateSandbox(%q, %q) = nil, want error — %s", c.sandbox, c.backend, c.why)
		}
		if !c.wantErr && err != nil {
			t.Errorf("validateSandbox(%q, %q) = %v, want nil — %s", c.sandbox, c.backend, err, c.why)
		}
	}
}

// The error has to tell the operator what to do instead, or it just blocks.
func TestValidateSandbox_ErrorIsActionable(t *testing.T) {
	err := validateSandbox("srt", "codex")
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"codex", "container", "--sandbox none"} {
		if !contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
