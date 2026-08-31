package resolver

import (
	"strings"
	"testing"
)

// A ref beginning with "-" is read by git as an option rather than a ref, so
// "--upload-pack=..." would run a command of the caller's choosing. The URL
// argument was already protected by NormalizeGitURL forcing a scheme; the ref
// had no equivalent.
//
// Refs come from a harness manifest, which is inside the local trust
// boundary, so this is defence in depth rather than a remote vector.
func TestLsRemote_RejectsOptionLikeRef(t *testing.T) {
	for _, ref := range []string{
		"--upload-pack=touch /tmp/pwned",
		"--exec=whatever",
		"-o",
	} {
		t.Run(ref, func(t *testing.T) {
			_, err := LsRemoteFunc("https://example.invalid/org/repo", ref)
			if err == nil {
				t.Fatalf("ref %q was accepted", ref)
			}
			if !strings.Contains(err.Error(), "may not begin with") {
				t.Errorf("rejected for the wrong reason: %v", err)
			}
		})
	}
}

// An ordinary ref must still be attempted. The call fails on the unreachable
// host, which is the point: it got past the guard to the network.
func TestLsRemote_AllowsAnOrdinaryRef(t *testing.T) {
	for _, ref := range []string{"main", "v1.2.0", "refs/heads/main", ""} {
		_, err := LsRemoteFunc("https://example.invalid/org/repo", ref)
		if err != nil && strings.Contains(err.Error(), "may not begin with") {
			t.Errorf("ordinary ref %q was rejected by the guard: %v", ref, err)
		}
	}
}
