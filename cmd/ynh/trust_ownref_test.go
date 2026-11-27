package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// ynh trust accept refuses an installed remote harness whose own ref is not a
// full SHA, with the same Fix line as the other routes and nothing recorded;
// a full SHA, and a local install, are accepted (ynh #1023).
func TestTrustAccept_OwnRefMustBeFullSHA(t *testing.T) {
	const full = "0123456789abcdef0123456789abcdef01234567"
	cases := []struct {
		name, sourceType, ref string
		wantErr               string
	}{
		{"branch", "git", "main", `"main" is a tag or branch`},
		{"tag", "registry", "v1.2.0", `"v1.2.0" is a tag or branch`},
		{"short", "git", "0123456", "short SHA"},
		{"none", "git", "", "no ref"},
		{"full", "git", full, ""},
		{"local", "local", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("YNH_HOME", home)
			id := installListTestHarness(t, home, "hooked", hookedHarness)
			dir := harness.InstalledDirByID(id)
			if err := plugin.SaveInstalledJSON(dir, &plugin.InstalledJSON{
				SourceType: tc.sourceType, Source: "github.com/acme/hooked", Ref: tc.ref, InstalledAt: "2026-11-27T00:00:00Z",
			}); err != nil {
				t.Fatal(err)
			}
			var out, errb bytes.Buffer
			err := cmdTrustAccept([]string{id}, &out, &errb)
			store, serr := loadTrustStore()
			if serr != nil {
				t.Fatal(serr)
			}
			_, recorded := store.Harnesses[id]
			if tc.wantErr == "" {
				if err != nil || !recorded {
					t.Fatalf("err = %v, recorded = %v; want accepted", err, recorded)
				}
				return
			}
			if err == nil && !strings.Contains(errb.String(), tc.wantErr) {
				t.Fatalf("accepted; want a refusal naming %q", tc.wantErr)
			}
			msg := errb.String()
			if err != nil {
				msg += err.Error()
			}
			for _, want := range []string{"the harness's own ref", tc.wantErr, "Pin it to a 40-character commit SHA", "ynh install github.com/acme/hooked --ref <40-character SHA>"} {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal lacks %q:\n%s", want, msg)
				}
			}
			if strings.Contains(msg, "accept it by hand") {
				t.Errorf("refusal sends the operator back to the command that refused:\n%s", msg)
			}
			if recorded {
				t.Error("a refused accept recorded the harness")
			}
		})
	}
}
