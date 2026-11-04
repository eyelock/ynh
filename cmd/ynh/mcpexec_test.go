package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/mcpexec"
)

// runSelf runs this test binary as ynh with args, the way Claude Code runs the
// launcher: a real process, since mcp-exec replaces itself.
func runSelf(t *testing.T, stdin string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), execMainEnv+"=1")
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		code = ee.ExitCode()
	default:
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

func writeEnv(t *testing.T, vars map[string]string) string {
	t.Helper()
	data, err := mcpexec.Encode(vars)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), mcpexec.EnvFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The launcher becomes the command: its stdin and stdout are the server's,
// the argument and the environment value are expanded from the file, and a
// variable only the process environment holds is not visible to the
// expansion.
func TestMCPExec_RunsCommandWithExpansion(t *testing.T) {
	file := writeEnv(t, map[string]string{"TOKEN": "s3cret=x\"y"})
	stdout, stderr, code := runSelf(t, "from-stdin",
		[]string{"SERVER_TOKEN=${TOKEN}", "LEAKED=tok-from-env"},
		"mcp-exec", "--env-file", file, "--", "sh", "-c", `cat; printf '|%s|%s|%s' "$1" "$SERVER_TOKEN" "$LEAKED"`, "sh", "arg-${TOKEN}")
	if code != 0 || stderr != "" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	want := `from-stdin|arg-s3cret=x"y|s3cret=x"y|tok-from-env`
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// An unknown reference stops the launch: nothing on stdout (it is the
// JSON-RPC channel), the name on stderr, and a non-zero exit.
func TestMCPExec_Errors(t *testing.T) {
	file := writeEnv(t, map[string]string{"TOKEN": "x"})
	cases := []struct {
		name string
		env  []string
		args []string
		want string
	}{
		{"unknown in argv", nil, []string{"mcp-exec", "--env-file", file, "--", "sh", "-c", "echo ${NOPE}"}, "${NOPE}"},
		{"missing file", nil, []string{"mcp-exec", "--env-file", filepath.Join(t.TempDir(), "absent"), "--", "sh"}, "reading env file"},
		{"no arguments", nil, []string{"mcp-exec"}, "usage: ynh mcp-exec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runSelf(t, "", tc.env, tc.args...)
			if stdout != "" {
				t.Errorf("stdout must stay empty, got %q", stdout)
			}
			if code == 0 || !strings.Contains(stderr, tc.want) {
				t.Errorf("code %d stderr %q, want non-zero and %q", code, stderr, tc.want)
			}
		})
	}
}

func TestMCPHeaders(t *testing.T) {
	file := writeEnv(t, map[string]string{"TOKEN": "s3cret"})
	stdout, stderr, code := runSelf(t, "", nil, "mcp-headers", "--env-file", file, "--", "Authorization: Bearer ${TOKEN}")
	if code != 0 || stderr != "" || stdout != `{"Authorization":"Bearer s3cret"}`+"\n" {
		t.Errorf("code %d stdout %q stderr %q", code, stdout, stderr)
	}

	stdout, stderr, code = runSelf(t, "", nil, "mcp-headers", "--env-file", file, "--", "A: ${NOPE}")
	if code == 0 || stdout != "" || !strings.Contains(stderr, "${NOPE}") {
		t.Errorf("unknown reference: code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

// Both commands answer --help without running, and neither is listed in
// `ynh help`.
func TestMCPExec_HelpDoesNotRun(t *testing.T) {
	for _, name := range []string{"mcp-exec", "mcp-headers"} {
		stdout, _, code := runSelf(t, "", nil, name, "--help")
		if code != 0 || !strings.Contains(stdout, "ynh "+name) {
			t.Errorf("%s --help: code %d stdout %q", name, code, stdout)
		}
	}
}

// An inherited variable that happens to hold a literal ${X} of its own is not
// the launcher's business: it is passed through untouched and the server
// starts.
func TestMCPExec_LeavesUnrelatedEnvironmentAlone(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".env.ynh")
	if err := os.WriteFile(file, []byte("TOKEN=\"s3cret\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runSelf(t, "", []string{"UNRELATED=${NOPE2}", "K=${TOKEN}"},
		"mcp-exec", "--env-file", file, "--", "sh", "-c", `printf '%s|%s' "$UNRELATED" "$K"`)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if want := "${NOPE2}|s3cret"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}
