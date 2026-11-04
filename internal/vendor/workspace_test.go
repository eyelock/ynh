package vendor

import (
	"reflect"
	"strings"
	"testing"
)

func TestWorkspaceArgs(t *testing.T) {
	dirs := []string{"/a/one", "/a/two"}
	tests := []struct {
		vendor string
		want   []string
		unsup  string
	}{
		{"claude", []string{"--add-dir", "/a/one", "--add-dir", "/a/two"}, ""},
		{"copilot", []string{"--add-dir", "/a/one", "--add-dir", "/a/two"}, ""},
		{"codex", []string{"--add-dir", "/a/one", "--add-dir", "/a/two"}, ""},
		{"cursor", []string{"--add-dir", "/a/one", "--add-dir", "/a/two"}, ""},
		{"opencode", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.vendor, func(t *testing.T) {
			got, unsup, err := WorkspaceArgs(tc.vendor, dirs)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("args = %v, want %v", got, tc.want)
			}
			if (tc.unsup == "") != (unsup == "") || !strings.Contains(unsup, tc.unsup) {
				t.Errorf("unsupported = %q, want it to contain %q", unsup, tc.unsup)
			}
		})
	}
	if _, _, err := WorkspaceArgs("nope", dirs); err == nil {
		t.Error("want an error for an unknown vendor")
	}
}

// TestEveryVendorDeclaresWorkspace: a registered vendor with no declaration, or
// with both a flag and a reason, or neither, is a defect.
func TestEveryVendorDeclaresWorkspace(t *testing.T) {
	for _, name := range Available() {
		d, ok := workspaceDeclarations[name]
		if !ok {
			t.Errorf("%s has no workspace declaration", name)
			continue
		}
		n := 0
		for _, s := range []string{d.flag, d.unsupported, d.byDefault} {
			if s != "" {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s must declare exactly one of a workspace flag, an unsupported reason or a default", name)
		}
	}
}

func TestGrantAgentWorkspace(t *testing.T) {
	deny := "---\ndescription: d\npermissions:\n  - action: \"*\"\n    resource: \"*\"\n    effect: deny\n  - action: read\n    resource: \"*\"\n    effect: allow\nmodel: x/y\n---\nbody\n"
	out, err := grantAgentWorkspace([]byte(deny), []string{"/a/one", "/a/two/"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	want := "    effect: allow\n  - action: external_directory\n    resource: \"/a/one/**\"\n    effect: allow\n  - action: external_directory\n    resource: \"/a/two/**\"\n    effect: allow\nmodel: x/y\n---\nbody\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("rules not appended after the last permission, before the next key:\n%s", got)
	}
	// Idempotent.
	if again, err := grantAgentWorkspace(out, []string{"/a/one", "/a/two/"}); err != nil || again != nil {
		t.Errorf("second grant = %q, %v; want no change", again, err)
	}
	// No permissions: inherits * * allow, left alone.
	if out, err := grantAgentWorkspace([]byte("---\ndescription: d\n---\nb\n"), []string{"/a"}); err != nil || out != nil {
		t.Errorf("agent without permissions changed: %q, %v", out, err)
	}
	// Inline permissions cannot take the grant, and say so.
	if _, err := grantAgentWorkspace([]byte("---\npermissions: [{action: read}]\n---\nb\n"), []string{"/a"}); err == nil {
		t.Error("want an error for inline permissions")
	}
}
