package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validateOutput runs validateHarness and returns what it printed and its error.
func validateOutput(t *testing.T, dir string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	vErr := validateHarness(dir)
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), vErr
}

func TestValidateHarnessWorkspace(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	hr := filepath.Join(home, "work", "seat")
	mkdirAll(t, hr)
	mkdirAll(t, filepath.Join(home, "work", "ynh"))
	t.Setenv("HOME", home)

	tests := []struct {
		name    string
		ws      string
		invalid bool
		want    string // substring of the output
	}{
		{"sibling folder is valid", `["../ynh"]`, false, ""},
		{"missing folder is a note, not an error", `["../not-checked-out"]`, false, "does not exist here"},
		{"filesystem root", `["/"]`, true, "filesystem root"},
		{"home directory", `["~"]`, true, "home directory"},
		{"parent of the harness", `[".."]`, true, "contains the harness directory"},
		{"the harness itself", `["."]`, true, "contains the harness directory"},
		{"not an array", `"../ynh"`, true, "workspace"},
		{"empty string entry", `[""]`, true, "workspace"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
				[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"seat","version":"0.1.0","workspace":`+tc.ws+`}`))
			out, err := validateOutput(t, hr)
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid = %v, want %v\n%s", err != nil, tc.invalid, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, out)
			}
		})
	}
}
