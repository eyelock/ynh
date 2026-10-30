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
		{"cursor", nil, "no flag"},
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
		if (d.flag == "") == (d.unsupported == "") {
			t.Errorf("%s must declare exactly one of a workspace flag or an unsupported reason", name)
		}
	}
}
