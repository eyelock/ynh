// Tests for `ynh info <name> --installed` (formerly the standalone
// `ynh installed <name>` command). The file name reflects the original
// command name to keep `go test -run` muscle memory working; the tests
// exercise the folded surface.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
)

func TestCmdInfoInstalled_JSONSchemaRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "rt", `{
		"name": "rt",
		"version": "0.1.0",
		"default_vendor": "claude"
	}`)
	insDir := filepath.Join(home, "harnesses", "local--rt", ".ynh-plugin")
	if err := os.MkdirAll(insDir, 0o755); err != nil {
		t.Fatal(err)
	}
	insJSON := `{
		"source_type": "local",
		"source": "/tmp/rt",
		"installed_at": "2026-05-13T12:00:00Z"
	}`
	if err := os.WriteFile(filepath.Join(insDir, "installed.json"), []byte(insJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := cmdInfoTo([]string{"local/rt", "--installed", "--format", "json"}, &out, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo --installed: %v", err)
	}
	var v any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out.String())
	}
	schema, err := clischema.Get("installed")
	if err != nil {
		t.Fatalf("Get installed schema: %v", err)
	}
	if err := schema.Validate(v); err != nil {
		t.Errorf("installed JSON does not validate: %v\noutput: %s", err, out.String())
	}
}

func TestCmdInfoInstalled_NotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	err := cmdInfoTo([]string{"local/missing", "--installed", "--format", "json"}, &out, &errb)
	if !errors.Is(err, errStructuredReported) {
		t.Fatalf("expected errStructuredReported, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty on error, got: %s", out.String())
	}
	var env any
	if err := json.Unmarshal(errb.Bytes(), &env); err != nil {
		t.Fatalf("error envelope not JSON: %v\nstderr: %s", err, errb.String())
	}
	schema, err := clischema.Get("error")
	if err != nil {
		t.Fatalf("Get error schema: %v", err)
	}
	if err := schema.Validate(env); err != nil {
		t.Errorf("error envelope does not validate: %v\nstderr: %s", err, errb.String())
	}
}

func TestCmdInfoInstalled_MutuallyExclusiveWithCheckUpdates(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdInfoTo([]string{"local/x", "--installed", "--check-updates", "--format", "json"}, &out, &errb)
	if err == nil {
		t.Fatal("expected error for --installed + --check-updates")
	}
}
