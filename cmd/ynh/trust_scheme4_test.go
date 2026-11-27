package main

import (
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// scheme4Base is a harness with something to review, so the digest has input,
// and one profile, so a profile's own settings can be moved.
func scheme4Base() *harness.Harness {
	h := hookHarness("ynm hook a")
	h.Profiles = map[string]plugin.Profile{"ci": {}}
	return h
}

func scheme4Digest(h *harness.Harness) string { return executablesDigest(harnessExecutables(h)) }

// Each setting that widens what runs, or what runs without asking, moves the
// digest on its own: changing only that, and nothing else, reads as changed.
func TestDigestScheme4MovesWithEachSetting(t *testing.T) {
	yes, no := true, false
	base := scheme4Digest(scheme4Base())
	for _, tc := range []struct {
		name  string
		apply func(h *harness.Harness)
	}{
		{"env_passthrough", func(h *harness.Harness) { h.EnvPassthrough = []string{"GH_TOKEN"} }},
		{"env_passthrough, one more name", func(h *harness.Harness) { h.EnvPassthrough = []string{"GH_TOKEN", "AWS_PROFILE"} }},
		{"mcp_manual_approval", func(h *harness.Harness) { h.MCPManualApproval = true }},
		{"default_vendor", func(h *harness.Harness) { h.DefaultVendor = "codex" }},
		{"profile env_passthrough", func(h *harness.Harness) {
			h.Profiles["ci"] = plugin.Profile{EnvPassthrough: []string{"GH_TOKEN"}}
		}},
		{"profile mcp_manual_approval true", func(h *harness.Harness) {
			h.Profiles["ci"] = plugin.Profile{MCPManualApproval: &yes}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := scheme4Base()
			tc.apply(h)
			if got := scheme4Digest(h); got == base {
				t.Errorf("digest did not move")
			}
		})
	}

	// A profile turning approval off under a harness that turns it on is the
	// widening the field exists to catch.
	on := scheme4Base()
	on.MCPManualApproval = true
	onDigest := scheme4Digest(on)
	off := scheme4Base()
	off.MCPManualApproval = true
	off.Profiles["ci"] = plugin.Profile{MCPManualApproval: &no}
	if scheme4Digest(off) == onDigest {
		t.Error("a profile turning mcp_manual_approval off did not move the digest")
	}
	// A profile that restates the harness's own setting changes nothing.
	same := scheme4Base()
	same.MCPManualApproval = true
	same.Profiles["ci"] = plugin.Profile{MCPManualApproval: &yes}
	if scheme4Digest(same) != onDigest {
		t.Error("a profile restating the harness's setting moved the digest")
	}
}

// The order env_passthrough is written in is not a change.
func TestDigestScheme4EnvPassthroughOrderDoesNotMatter(t *testing.T) {
	a, b := scheme4Base(), scheme4Base()
	a.EnvPassthrough = []string{"A", "B"}
	b.EnvPassthrough = []string{"B", "A"}
	if scheme4Digest(a) != scheme4Digest(b) {
		t.Error("reordering env_passthrough moved the digest")
	}
}

// A run's own -v or YNH_VENDOR is the operator's choice, not the harness's
// content: it never reaches the digest, which reads the manifest only.
func TestDigestScheme4IgnoresRunTimeVendor(t *testing.T) {
	base := scheme4Digest(scheme4Base())
	t.Setenv("YNH_VENDOR", "codex")
	if scheme4Digest(scheme4Base()) != base {
		t.Error("YNH_VENDOR moved the digest")
	}
}

// The digest is the full SHA-256: 64 hexadecimal characters after the prefix.
func TestDigestScheme4IsFullLength(t *testing.T) {
	if ok, _ := regexp.MatchString(`^sha256:[0-9a-f]{64}$`, scheme4Digest(scheme4Base())); !ok {
		t.Errorf("digest %q is not sha256: and 64 hex characters", scheme4Digest(scheme4Base()))
	}
}

// A scheme 3 record reads changed once, with scheme 4's note, even when
// nothing in the harness moved.
func TestScheme3RecordReadsChangedWithScheme4Note(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	const id = "github.com/someone/harness"
	h := scheme4Base()
	store := trustStore{Harnesses: map[string]trustRecord{
		id: {Digest: "sha256:0123456789abcdef", Scheme: 3, AcceptedAt: "2026-11-01T00:00:00Z"},
	}}
	st := trustStatusFor(id, h, store, nil)
	if st.State != trustChanged || !st.LegacyDigest {
		t.Fatalf("state %q legacy %v, want changed and legacy", st.State, st.LegacyDigest)
	}
	out := showText(t, st)
	for _, want := range []string{"env_passthrough", "mcp_manual_approval", "default_vendor", "accept again"} {
		if !strings.Contains(out, want) {
			t.Errorf("show does not carry scheme 4's note (missing %q):\n%s", want, out)
		}
	}
	store.Harnesses[id] = trustRecord{Digest: trustDigest(h), Scheme: trustScheme}
	if st := trustStatusFor(id, h, store, nil); st.State != trustAccepted || st.LegacyDigest {
		t.Errorf("after accepting again: %+v", st)
	}
}

// An image built by an older ynh bakes a scheme 3 record. Inside the image the
// harness is accepted by the record and nothing else, so the gate refuses it,
// with scheme 4's note, until the image is rebuilt.
func TestTrustGate_Scheme3ImageRecordRefusesInside(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installTrustedTestHarness(t, home, "hooked", hookedHarness)
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	rec := store.Harnesses[id]
	rec.Via, rec.Scheme = viaImage, 3
	rec.Digest = rec.Digest[:len("sha256:")+16] // a scheme 3 digest was 64 bits
	store.Harnesses[id] = rec
	if err := saveTrustStore(store); err != nil {
		t.Fatal(err)
	}
	err = trustGate(id, loadInstalled(t, id), io.Discard)
	if !isUntrusted(err) || !strings.Contains(err.Error(), settingsDigestNote) {
		t.Fatalf("err = %v, want a refusal with scheme 4's note", err)
	}
}
