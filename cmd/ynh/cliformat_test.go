package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCliError_Unstructured(t *testing.T) {
	var stderr bytes.Buffer
	err := cliError(&stderr, false, errCodeNotFound, "thing not found")
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if err.Error() != "thing not found" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
	if stderr.Len() != 0 {
		t.Errorf("unstructured mode should not write to stderr, got: %s", stderr.String())
	}
}

func TestCliError_Structured_ValidJSON(t *testing.T) {
	var stderr bytes.Buffer
	err := cliError(&stderr, true, errCodeInvalidInput, "bad value")
	if !errors.Is(err, errStructuredReported) {
		t.Errorf("expected errStructuredReported sentinel, got %v", err)
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if parseErr := json.Unmarshal(bytes.TrimRight(stderr.Bytes(), "\n"), &env); parseErr != nil {
		t.Fatalf("stderr is not valid JSON: %v\nraw: %s", parseErr, stderr.String())
	}
	if env.Error.Code != errCodeInvalidInput {
		t.Errorf("expected code %q, got %q", errCodeInvalidInput, env.Error.Code)
	}
	if env.Error.Message != "bad value" {
		t.Errorf("expected message %q, got %q", "bad value", env.Error.Message)
	}
}

func TestCliError_Structured_NoHTMLEscape(t *testing.T) {
	var stderr bytes.Buffer
	_ = cliError(&stderr, true, errCodeInvalidInput,
		"usage: ynh sensors run <harness-name> <sensor-name>")

	raw := stderr.String()
	// Go's default JSON encoder HTML-escapes < as <; SetEscapeHTML(false) disables that.
	if strings.Contains(raw, `\u003c`) || strings.Contains(raw, `\u003e`) {
		t.Errorf("angle brackets must not be HTML-escaped in error envelope, got: %s", raw)
	}
	if !strings.Contains(raw, "<harness-name>") {
		t.Errorf("expected literal angle brackets in output, got: %s", raw)
	}
}

func TestCliError_Structured_EndsWithNewline(t *testing.T) {
	var stderr bytes.Buffer
	_ = cliError(&stderr, true, errCodeNotFound, "not found")
	if !bytes.HasSuffix(stderr.Bytes(), []byte("\n")) {
		t.Error("structured error envelope should end with a newline")
	}
}
