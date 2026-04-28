package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func writeDelegateTestHarness(t *testing.T, dir, name string) {
	t.Helper()
	hj := &plugin.HarnessJSON{Name: name, Version: "0.1.0"}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
}

func loadTestDelegates(t *testing.T, dir string) []plugin.DelegateMeta {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	return hj.DelegatesTo
}

// ---- routing -------------------------------------------------------

func TestCmdDelegate_NoArgs(t *testing.T) {
	err := cmdDelegate([]string{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdDelegate_UnknownSubcommand(t *testing.T) {
	err := cmdDelegate([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown delegate subcommand") {
		t.Errorf("expected unknown subcommand error, got: %v", err)
	}
}

// ---- add -------------------------------------------------------

func TestCmdDelegateAdd_MissingArgs(t *testing.T) {
	var buf bytes.Buffer
	err := cmdDelegateTo([]string{"add"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdDelegateAdd_UnknownFlag(t *testing.T) {
	var buf bytes.Buffer
	err := cmdDelegateTo([]string{"add", "--bogus"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected unknown flag error, got: %v", err)
	}
}

func TestCmdDelegateAdd_PathBased(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdDelegateTo([]string{"add", dir, "github.com/acme/agent"}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dels := loadTestDelegates(t, dir)
	if len(dels) != 1 || dels[0].Git != "github.com/acme/agent" {
		t.Errorf("expected 1 delegate, got %+v", dels)
	}
	if !strings.Contains(buf.String(), "Added") {
		t.Errorf("expected 'Added' in output, got: %q", buf.String())
	}
}

func TestCmdDelegateAdd_WithFlags(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdDelegateTo([]string{"add", dir, "github.com/acme/agent",
		"--path", "agents/coder",
		"--ref", "v2",
	}, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dels := loadTestDelegates(t, dir)
	if len(dels) != 1 {
		t.Fatalf("expected 1 delegate, got %d", len(dels))
	}
	if dels[0].Path != "agents/coder" || dels[0].Ref != "v2" {
		t.Errorf("unexpected delegate: %+v", dels[0])
	}
}

func TestCmdDelegateAdd_Duplicate(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdDelegateTo([]string{"add", dir, "github.com/acme/agent"}, &buf); err != nil {
		t.Fatal(err)
	}

	err := cmdDelegateTo([]string{"add", dir, "github.com/acme/agent"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "already present") {
		t.Errorf("expected already-present error, got: %v", err)
	}
}

// ---- remove -------------------------------------------------------

func TestCmdDelegateRemove_MissingArgs(t *testing.T) {
	var buf bytes.Buffer
	err := cmdDelegateTo([]string{"remove"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdDelegateRemove_PathBased(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdDelegateTo([]string{"add", dir, "github.com/acme/agent"}, &buf); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := cmdDelegateTo([]string{"remove", dir, "github.com/acme/agent"}, &buf); err != nil {
		t.Fatalf("remove: %v", err)
	}

	dels := loadTestDelegates(t, dir)
	if len(dels) != 0 {
		t.Errorf("expected 0 delegates after remove, got %d", len(dels))
	}
	if !strings.Contains(buf.String(), "Removed") {
		t.Errorf("expected 'Removed' in output, got: %q", buf.String())
	}
}

func TestCmdDelegateRemove_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdDelegateTo([]string{"remove", dir, "github.com/acme/agent"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}

func TestCmdDelegateRemove_Ambiguous(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdDelegateTo([]string{"add", dir, "github.com/acme/mono", "--path", "a"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdDelegateTo([]string{"add", dir, "github.com/acme/mono", "--path", "b"}, &buf); err != nil {
		t.Fatal(err)
	}

	err := cmdDelegateTo([]string{"remove", dir, "github.com/acme/mono"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "disambiguate") {
		t.Errorf("expected disambiguate error, got: %v", err)
	}
}

// ---- update -------------------------------------------------------

func TestCmdDelegateUpdate_MissingArgs(t *testing.T) {
	var buf bytes.Buffer
	err := cmdDelegateTo([]string{"update"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdDelegateUpdate_NoChangeFlags(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdDelegateTo([]string{"update", dir, "github.com/acme/agent"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("expected at-least-one error, got: %v", err)
	}
}

func TestCmdDelegateUpdate_Ref(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdDelegateTo([]string{"add", dir, "github.com/acme/agent", "--ref", "v1"}, &buf); err != nil {
		t.Fatal(err)
	}

	buf.Reset()
	if err := cmdDelegateTo([]string{"update", dir, "github.com/acme/agent", "--ref", "v2"}, &buf); err != nil {
		t.Fatalf("update: %v", err)
	}

	dels := loadTestDelegates(t, dir)
	if dels[0].Ref != "v2" {
		t.Errorf("expected ref v2, got %q", dels[0].Ref)
	}
	if !strings.Contains(buf.String(), "Updated") {
		t.Errorf("expected 'Updated' in output, got: %q", buf.String())
	}
}

func TestCmdDelegateUpdate_Path(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdDelegateTo([]string{"add", dir, "github.com/acme/agent", "--path", "old"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdDelegateTo([]string{"update", dir, "github.com/acme/agent", "--path", "new"}, &buf); err != nil {
		t.Fatalf("update: %v", err)
	}

	dels := loadTestDelegates(t, dir)
	if dels[0].Path != "new" {
		t.Errorf("expected path=new, got %q", dels[0].Path)
	}
}

func TestCmdDelegateUpdate_FromPath(t *testing.T) {
	dir := t.TempDir()
	writeDelegateTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdDelegateTo([]string{"add", dir, "github.com/acme/mono", "--path", "a", "--ref", "v1"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdDelegateTo([]string{"add", dir, "github.com/acme/mono", "--path", "b", "--ref", "v1"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdDelegateTo([]string{"update", dir, "github.com/acme/mono", "--from-path", "a", "--ref", "v2"}, &buf); err != nil {
		t.Fatalf("update with from-path: %v", err)
	}

	dels := loadTestDelegates(t, dir)
	refByPath := map[string]string{}
	for _, del := range dels {
		refByPath[del.Path] = del.Ref
	}
	if refByPath["a"] != "v2" || refByPath["b"] != "v1" {
		t.Errorf("unexpected delegates after from-path update: %+v", dels)
	}
}
