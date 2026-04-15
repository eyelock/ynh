package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdPathsText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var stdout bytes.Buffer
	if err := cmdPathsTo(nil, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdPathsTo: %v", err)
	}

	// Each key must appear on its own line paired with the expected path.
	// Exact column spacing is a tabwriter implementation detail and not asserted.
	wantPairs := map[string]string{
		"home":      home,
		"config":    filepath.Join(home, "config.json"),
		"harnesses": filepath.Join(home, "harnesses"),
		"symlinks":  filepath.Join(home, "symlinks.json"),
		"cache":     filepath.Join(home, "cache"),
		"run":       filepath.Join(home, "run"),
		"bin":       filepath.Join(home, "bin"),
	}
	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	for key, wantPath := range wantPairs {
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, key) && strings.HasSuffix(line, wantPath) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no line pairs %q with %q in output:\n%s", key, wantPath, stdout.String())
		}
	}

	if len(lines) != len(wantPairs) {
		t.Errorf("got %d lines, want %d — full output:\n%s", len(lines), len(wantPairs), stdout.String())
	}
}

func TestCmdPathsJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var stdout bytes.Buffer
	if err := cmdPathsTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdPathsTo: %v", err)
	}

	var got resolvedPaths
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal JSON output: %v\noutput: %s", err, stdout.String())
	}

	want := resolvedPaths{
		Home:      home,
		Config:    filepath.Join(home, "config.json"),
		Harnesses: filepath.Join(home, "harnesses"),
		Symlinks:  filepath.Join(home, "symlinks.json"),
		Cache:     filepath.Join(home, "cache"),
		Run:       filepath.Join(home, "run"),
		Bin:       filepath.Join(home, "bin"),
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// Output must end with a newline — consumers rely on that per cli-structured.md.
	if !strings.HasSuffix(stdout.String(), "\n") {
		t.Error("JSON output does not end with a newline")
	}
}

func TestCmdPathsEnvOverrideIsolation(t *testing.T) {
	// Two sequential invocations with different YNH_HOME values must produce
	// outputs matching each env — a regression guard for any accidental
	// caching of path values across calls.
	run := func(home string) resolvedPaths {
		t.Setenv("YNH_HOME", home)
		var stdout bytes.Buffer
		if err := cmdPathsTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
			t.Fatalf("cmdPathsTo: %v", err)
		}
		var p resolvedPaths
		if err := json.Unmarshal(stdout.Bytes(), &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return p
	}

	a := run(t.TempDir())
	b := run(t.TempDir())
	if a.Home == b.Home {
		t.Fatal("YNH_HOME override did not change resolved home path between invocations")
	}
}

func TestCmdPathsInvalidFormatText(t *testing.T) {
	// Without --format json, errors are plain Go errors — main.go prefixes
	// "Error: " and prints to stderr. Nothing on stderr from the command itself.
	t.Setenv("YNH_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	err := cmdPathsTo([]string{"--format", "yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should mention the invalid value, got: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty in text mode (main.go prints the error), got: %s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on error, got: %s", stdout.String())
	}
}

func TestCmdPathsInvalidFormatJSONEnvelope(t *testing.T) {
	// An error after `--format json` has been parsed must go out as the
	// structured error envelope on stderr, per docs/cli-structured.md.
	t.Setenv("YNH_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	err := cmdPathsTo([]string{"--format", "json", "extra"}, &stdout, &stderr)
	if !errors.Is(err, errStructuredReported) {
		t.Fatalf("expected errStructuredReported, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must be empty on structured error, got: %s", stdout.String())
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("stderr is not valid JSON envelope: %v\nraw: %s", err, stderr.String())
	}
	if env.Error.Code == "" {
		t.Error("envelope missing error.code")
	}
	if env.Error.Message == "" {
		t.Error("envelope missing error.message")
	}
	if !strings.Contains(env.Error.Message, "extra") {
		t.Errorf("envelope message should mention the offending argument, got: %s", env.Error.Message)
	}
}

func TestCmdPathsMissingFormatValue(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdPathsTo([]string{"--format"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing --format value")
	}
}

func TestCmdPathsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdPathsTo([]string{"--nope"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestCmdPathsUnexpectedPositional(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdPathsTo([]string{"extra"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unexpected positional argument")
	}
}

func TestCmdPathsExplicitText(t *testing.T) {
	// `--format text` must behave identically to the default.
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var defaultBuf, explicitBuf bytes.Buffer
	if err := cmdPathsTo(nil, &defaultBuf, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cmdPathsTo([]string{"--format", "text"}, &explicitBuf, io.Discard); err != nil {
		t.Fatal(err)
	}
	if defaultBuf.String() != explicitBuf.String() {
		t.Errorf("default and --format text outputs differ:\ndefault:\n%s\nexplicit:\n%s",
			defaultBuf.String(), explicitBuf.String())
	}
}
