package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// writeIncludeTestHarness creates a minimal plugin.json in dir.
func writeIncludeTestHarness(t *testing.T, dir, name string) {
	t.Helper()
	hj := &plugin.HarnessJSON{Name: name, Version: "0.1.0"}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
}

func loadTestIncludes(t *testing.T, dir string) []plugin.IncludeMeta {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	return hj.Includes
}

// ---- routing -------------------------------------------------------

func TestCmdInclude_NoArgs(t *testing.T) {
	err := cmdInclude([]string{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdInclude_UnknownSubcommand(t *testing.T) {
	err := cmdInclude([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown include subcommand") {
		t.Errorf("expected unknown subcommand error, got: %v", err)
	}
}

// ---- add -------------------------------------------------------

func TestCmdIncludeAdd_MissingArgs(t *testing.T) {
	var buf bytes.Buffer
	err := cmdIncludeTo([]string{"add"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdIncludeAdd_UnknownFlag(t *testing.T) {
	var buf bytes.Buffer
	err := cmdIncludeTo([]string{"add", "--bogus"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected unknown flag error, got: %v", err)
	}
}

func TestCmdIncludeAdd_PathBased(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	incs := loadTestIncludes(t, dir)
	if len(incs) != 1 || incs[0].Git != "github.com/acme/tools" {
		t.Errorf("expected 1 include, got %+v", incs)
	}
	if !strings.Contains(buf.String(), "Added") {
		t.Errorf("expected 'Added' in output, got: %q", buf.String())
	}
}

func TestCmdIncludeAdd_WithFlags(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools",
		"--path", "plugins/search",
		"--ref", "v2",
	}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	incs := loadTestIncludes(t, dir)
	if len(incs) != 1 {
		t.Fatalf("expected 1 include, got %d", len(incs))
	}
	if incs[0].Path != "plugins/search" || incs[0].Ref != "v2" {
		t.Errorf("unexpected include: %+v", incs[0])
	}
}

func TestCmdIncludeAdd_Duplicate(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools"}, &buf); err != nil {
		t.Fatal(err)
	}

	err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "already present") {
		t.Errorf("expected already-present error, got: %v", err)
	}
}

func TestCmdIncludeAdd_Replace(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--ref", "v1"}, &buf); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--ref", "v2", "--replace"}, &buf); err != nil {
		t.Fatalf("replace failed: %v", err)
	}

	incs := loadTestIncludes(t, dir)
	if len(incs) != 1 || incs[0].Ref != "v2" {
		t.Errorf("expected ref v2 after replace, got %+v", incs)
	}
	if !strings.Contains(buf.String(), "Replaced") {
		t.Errorf("expected 'Replaced' in output, got: %q", buf.String())
	}
}

// ---- remove -------------------------------------------------------

func TestCmdIncludeRemove_MissingArgs(t *testing.T) {
	var buf bytes.Buffer
	err := cmdIncludeTo([]string{"remove"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdIncludeRemove_PathBased(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools"}, &buf); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := cmdIncludeTo([]string{"remove", dir, "github.com/acme/tools"}, &buf); err != nil {
		t.Fatalf("remove: %v", err)
	}

	incs := loadTestIncludes(t, dir)
	if len(incs) != 0 {
		t.Errorf("expected 0 includes after remove, got %d", len(incs))
	}
	if !strings.Contains(buf.String(), "Removed") {
		t.Errorf("expected 'Removed' in output, got: %q", buf.String())
	}
}

func TestCmdIncludeRemove_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdIncludeTo([]string{"remove", dir, "github.com/acme/tools"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}

func TestCmdIncludeRemove_Ambiguous(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--path", "a"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--path", "b"}, &buf); err != nil {
		t.Fatal(err)
	}

	err := cmdIncludeTo([]string{"remove", dir, "github.com/acme/tools"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "disambiguate") {
		t.Errorf("expected disambiguate error, got: %v", err)
	}
}

// ---- update -------------------------------------------------------

func TestCmdIncludeUpdate_MissingArgs(t *testing.T) {
	var buf bytes.Buffer
	err := cmdIncludeTo([]string{"update"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdIncludeUpdate_NoChangeFlags(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdIncludeTo([]string{"update", dir, "github.com/acme/tools"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("expected at-least-one error, got: %v", err)
	}
}

func TestCmdIncludeUpdate_Ref(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--ref", "v1"}, &buf); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := cmdIncludeTo([]string{"update", dir, "github.com/acme/tools", "--ref", "v2"}, &buf); err != nil {
		t.Fatalf("update: %v", err)
	}

	incs := loadTestIncludes(t, dir)
	if incs[0].Ref != "v2" {
		t.Errorf("expected ref v2, got %q", incs[0].Ref)
	}
	if !strings.Contains(buf.String(), "Updated") {
		t.Errorf("expected 'Updated' in output, got: %q", buf.String())
	}
}

func TestCmdIncludeUpdate_Path(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--path", "old"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdIncludeTo([]string{"update", dir, "github.com/acme/tools", "--path", "new"}, &buf); err != nil {
		t.Fatalf("update: %v", err)
	}

	incs := loadTestIncludes(t, dir)
	if incs[0].Path != "new" {
		t.Errorf("expected path=new, got %q", incs[0].Path)
	}
}

func TestCmdIncludeUpdate_FromPath(t *testing.T) {
	dir := t.TempDir()
	writeIncludeTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--path", "a", "--ref", "v1"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--path", "b", "--ref", "v1"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdIncludeTo([]string{"update", dir, "github.com/acme/tools", "--from-path", "a", "--ref", "v2"}, &buf); err != nil {
		t.Fatalf("update with from-path: %v", err)
	}

	incs := loadTestIncludes(t, dir)
	refByPath := map[string]string{}
	for _, inc := range incs {
		refByPath[inc.Path] = inc.Ref
	}
	if refByPath["a"] != "v2" || refByPath["b"] != "v1" {
		t.Errorf("unexpected includes after from-path update: %+v", incs)
	}
}

// ---- splitPick -------------------------------------------------------

func TestSplitPick(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b , c", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
		{"", []string{}},
		{" , , ", []string{}},
	}
	for _, tc := range cases {
		got := splitPick(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitPick(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitPick(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
