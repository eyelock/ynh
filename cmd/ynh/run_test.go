package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDelegateQuietArgs(t *testing.T) {
	cases := []struct {
		name, vendor, handle string
		args                 []string
		want                 []string
	}{
		{"copilot delegate", "copilot", "dlg-1", nil, []string{"--silent"}},
		{"copilot not a delegate", "copilot", "", nil, nil},
		{"other vendor delegate", "claude", "dlg-1", nil, nil},
		{"already -s", "copilot", "dlg-1", []string{"-s"}, nil},
		{"already --silent", "copilot", "dlg-1", []string{"--model=x", "--silent"}, nil},
	}
	for _, c := range cases {
		got := delegateQuietArgs(c.vendor, c.args, c.handle)
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// A Copilot -p run prints a model-selection banner and a stats block around
// the answer (captured from Copilot CLI 1.0.110 in testdata). A delegate's
// run adds --silent, and with it the output a parent receives is the answer
// alone. The stub prints the captured output, or the answer only with --silent,
// as the real CLI does.
func TestCopilotDelegateResultCarriesNoBanner(t *testing.T) {
	captured, err := os.ReadFile(filepath.Join("testdata", "copilot_p_banner.txt"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "captured.txt"), captured, 0o644); err != nil {
		t.Fatal(err)
	}
	stub := `#!/bin/sh
for a in "$@"; do
  if [ "$a" = "--silent" ]; then echo OK; exit 0; fi
done
cat "` + filepath.Join(dir, "captured.txt") + `"
`
	bin := filepath.Join(dir, "copilot")
	if err := os.WriteFile(bin, []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(handle string) string {
		args := append(delegateQuietArgs("copilot", nil, handle), "-p", "Reply with the single word OK")
		out, err := exec.Command(bin, args...).Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := run(""); !strings.Contains(got, "Copilot Fusion selected") {
		t.Fatalf("the captured sample has no banner to strip: %q", got)
	}
	if got := run("dlg-1"); got != "OK" {
		t.Errorf("a delegate's result is %q, want the answer alone", got)
	}
}
