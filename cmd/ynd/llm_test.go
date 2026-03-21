package main

import (
	"fmt"
	"testing"
)

func TestDetectVendorCLI(t *testing.T) {
	tests := []struct {
		name      string
		available map[string]bool // binary name → available
		want      string
	}{
		{
			name:      "claude available",
			available: map[string]bool{"claude": true},
			want:      "claude",
		},
		{
			name:      "codex available",
			available: map[string]bool{"codex": true},
			want:      "codex",
		},
		{
			name:      "cursor agent available",
			available: map[string]bool{"agent": true},
			want:      "cursor",
		},
		{
			name:      "claude preferred over codex",
			available: map[string]bool{"claude": true, "codex": true},
			want:      "claude",
		},
		{
			name:      "claude preferred over cursor",
			available: map[string]bool{"claude": true, "agent": true},
			want:      "claude",
		},
		{
			name:      "codex preferred over cursor",
			available: map[string]bool{"codex": true, "agent": true},
			want:      "codex",
		},
		{
			name:      "none available",
			available: map[string]bool{},
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origLookPath := lookPathFunc
			t.Cleanup(func() { lookPathFunc = origLookPath })

			lookPathFunc = func(name string) (string, error) {
				if tt.available[name] {
					return "/mock/" + name, nil
				}
				return "", fmt.Errorf("not found")
			}

			got := detectVendorCLI()
			if got != tt.want {
				t.Errorf("detectVendorCLI() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQueryLLMImpl_UnsupportedVendor(t *testing.T) {
	_, err := queryLLMImpl("unknown", "test prompt")
	if err == nil {
		t.Fatal("expected error for unsupported vendor")
	}
	want := `unsupported vendor "unknown"`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestQueryLLMImpl_CursorVendorAccepted(t *testing.T) {
	// We can't run the real agent binary, but we can verify cursor
	// doesn't hit the "unsupported vendor" error path by checking
	// it tries to exec (which will fail with a different error).
	_, err := queryLLMImpl("cursor", "test prompt")
	if err == nil {
		// Agent CLI is actually available — that's fine
		return
	}
	// Should NOT be "unsupported vendor"
	if err.Error() == `unsupported vendor "cursor"` {
		t.Error("cursor vendor should be supported")
	}
}
