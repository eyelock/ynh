package mcpexec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

var awkward = map[string]string{
	"EQUALS":    "a=b=c",
	"QUOTES":    `he said "hi" and 'bye'`,
	"NEWLINES":  "line1\nline2\r\nline3\n",
	"BACKSLASH": `C:\path\${NOT_A_REF}`,
	"HASH":      "# not a comment",
	"EMPTY":     "",
	"SPACES":    "  padded  ",
	"UNICODE":   "caf\u00e9 \u2603",
	"BYTES":     "\x00\x01\xff\xfe",
	"DOLLAR":    "${LOOKS_LIKE_A_REF}",
}

func TestEnvFileRoundTrip(t *testing.T) {
	data, err := Encode(awkward)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != len(awkward) {
		t.Errorf("want one line per variable (%d), got %d:\n%s", len(awkward), got, data)
	}
	back, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, data)
	}
	if !reflect.DeepEqual(back, awkward) {
		t.Errorf("round trip changed the values\n got %q\nwant %q", back, awkward)
	}
}

func TestEncodeRejectsBadName(t *testing.T) {
	for _, name := range []string{"", "1X", "A-B", "A B", "A=B", "A\nB"} {
		if _, err := Encode(map[string]string{name: "v"}); err == nil {
			t.Errorf("name %q should be refused", name)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr string
	}{
		{"comments and blanks", "# note\n\nA=\"1\"\n  # indented\nB=\"2\"\r\n", map[string]string{"A": "1", "B": "2"}, ""},
		{"unquoted value", "A=1\n", nil, "double-quoted"},
		{"single quotes", "A='1'\n", nil, "double-quoted"},
		{"bad name", "1A=\"x\"\n", nil, "NAME"},
		{"no equals", "justaname\n", nil, "NAME"},
		{"duplicate", "A=\"1\"\nA=\"2\"\n", nil, "twice"},
		{"unterminated", "A=\"1\n", nil, "double-quoted"},
		{"empty file", "", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse([]byte(tc.in))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestExpand(t *testing.T) {
	vars := map[string]string{"A": "alpha", "B": "${A}", "C": ""}
	cases := []struct {
		in, want, wantErr string
	}{
		{"plain", "plain", ""},
		{"Bearer ${A}", "Bearer alpha", ""},
		{"${A}-${A}", "alpha-alpha", ""},
		{"${B}", "${A}", ""}, // a value is not expanded again
		{"x${C}y", "xy", ""},
		{"$A and $(A) stay", "$A and $(A) stay", ""},
		{"${MISSING}", "", "${MISSING}"},
		{"ok ${A} then ${NOPE}", "", "${NOPE}"},
	}
	for _, tc := range cases {
		got, err := Expand(tc.in, vars)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Expand(%q): want error naming %s, got %v", tc.in, tc.wantErr, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("Expand(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func envFile(t *testing.T, vars map[string]string) string {
	t.Helper()
	data, err := Encode(vars)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), EnvFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPrepare(t *testing.T) {
	file := envFile(t, map[string]string{"TOKEN": "s3cret", "HOST": "db.example"})

	t.Run("expands argv and environment from the file only", func(t *testing.T) {
		t.Setenv("TOKEN", "from-the-process-env")
		plan, err := Prepare(
			[]string{"--env-file", file, "--", "sh", "-c", "echo ${TOKEN}", "--host=${HOST}", "plain"},
			[]string{"PATH=/usr/bin", "API_KEY=${TOKEN}", "MIXED=a-${HOST}-b", "OTHER=literal"})
		if err != nil {
			t.Fatal(err)
		}
		wantArgv := []string{"sh", "-c", "echo s3cret", "--host=db.example", "plain"}
		if !reflect.DeepEqual(plan.Argv, wantArgv) {
			t.Errorf("argv = %q, want %q", plan.Argv, wantArgv)
		}
		wantEnv := []string{"PATH=/usr/bin", "API_KEY=s3cret", "MIXED=a-db.example-b", "OTHER=literal"}
		if !reflect.DeepEqual(plan.Env, wantEnv) {
			t.Errorf("env = %q, want %q", plan.Env, wantEnv)
		}
		if filepath.Base(plan.Path) != "sh" {
			t.Errorf("path = %q", plan.Path)
		}
	})

	t.Run("equals form of the flag", func(t *testing.T) {
		plan, err := Prepare([]string{"--env-file=" + file, "--", "sh", "${TOKEN}"}, nil)
		if err != nil || plan.Argv[1] != "s3cret" {
			t.Errorf("plan %+v err %v", plan, err)
		}
	})

	errs := []struct {
		name    string
		args    []string
		environ []string
		want    string
	}{
		{"unknown reference in argv", []string{"--env-file", file, "--", "sh", "${NOPE}"}, nil, "${NOPE}"},
		{"no env file flag", []string{"--", "sh"}, nil, "--env-file is required"},
		{"no separator", []string{"--env-file", file, "sh"}, nil, "unexpected argument"},
		{"no command", []string{"--env-file", file, "--"}, nil, "nothing after --"},
		{"missing file", []string{"--env-file", filepath.Join(t.TempDir(), "absent"), "--", "sh"}, nil, "reading env file"},
		{"command not found", []string{"--env-file", file, "--", "no-such-command-ynh"}, nil, "no-such-command-ynh"},
	}
	for _, tc := range errs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Prepare(tc.args, tc.environ)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPrepareReportsBadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), EnvFileName)
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare([]string{"--env-file", path, "--", "sh"}, nil)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("want the error to name the file, got %v", err)
	}
}

func TestHeaders(t *testing.T) {
	file := envFile(t, map[string]string{"TOKEN": `tok"en`})
	var out bytes.Buffer
	if err := Headers([]string{"--env-file", file, "--", "Authorization: Bearer ${TOKEN}", "X-Static:  plain "}, &out); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, out.String())
	}
	want := map[string]string{"Authorization": `Bearer tok"en`, "X-Static": "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}

	for _, tc := range []struct{ name, h, want string }{
		{"unknown", "A: ${NOPE}", "${NOPE}"},
		{"no colon", "Authorization", "Name: value"},
		{"no name", ": v", "Name: value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := Headers([]string{"--env-file", file, "--", tc.h}, &out)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
			if out.Len() != 0 {
				t.Errorf("an error must leave stdout empty, got %q", out.String())
			}
		})
	}
}

func TestNamesAndWrap(t *testing.T) {
	s := plugin.MCPServer{
		Command: "node", Args: []string{"srv.js", "--token=${B}"},
		Env: map[string]string{"A": "${A}", "L": "literal"}, Cwd: "${IGNORED}",
	}
	if got, want := Names(s), []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
	if References(plugin.MCPServer{Command: "node", Args: []string{"x"}}) {
		t.Error("a server with no references needs no launcher")
	}

	w := WrapStdio(s, "/run/delegates/d/.env.ynh")
	wantArgs := []string{"mcp-exec", "--env-file", "/run/delegates/d/.env.ynh", "--", "node", "srv.js", "--token=${B}"}
	if w.Command != "ynh" || !reflect.DeepEqual(w.Args, wantArgs) {
		t.Errorf("wrapped = %q %q", w.Command, w.Args)
	}
	if w.Env["A"] != "${A}" {
		t.Errorf("env keeps its reference for the launcher: %v", w.Env)
	}
	if len(s.Args) != 2 {
		t.Error("WrapStdio must not change its input")
	}
}

func TestSplitHeadersAndCommand(t *testing.T) {
	templated, static := SplitHeaders(map[string]string{"Authorization": "Bearer ${T}", "X-Org": "acme", "X-Q": "it's ${T}"})
	if len(templated) != 2 || len(static) != 1 || static["X-Org"] != "acme" {
		t.Errorf("templated %v static %v", templated, static)
	}
	got := HeadersCommand(templated, "/run/my dir/.env.ynh")
	want := `ynh mcp-headers --env-file '/run/my dir/.env.ynh' -- 'Authorization: Bearer ${T}' 'X-Q: it'\''s ${T}'`
	if got != want {
		t.Errorf("command =\n%s\nwant\n%s", got, want)
	}
}

func TestExpandEnviron_OnlyFileVariables(t *testing.T) {
	vars := map[string]string{"TOKEN": "s3cret"}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"declared reference expands", "TOKEN=${TOKEN}", "TOKEN=s3cret"},
		{"mixed value expands only the declared name", "AUTH=Bearer ${TOKEN} via ${PROXY}", "AUTH=Bearer s3cret via ${PROXY}"},
		{"unrelated inherited reference left as is", "PROMPT_HINT=${USER}@host", "PROMPT_HINT=${USER}@host"},
		{"no reference untouched", "PATH=/usr/bin", "PATH=/usr/bin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandEnviron([]string{tt.in}, vars)
			if err != nil {
				t.Fatalf("ExpandEnviron(%q): %v", tt.in, err)
			}
			if got[0] != tt.want {
				t.Errorf("ExpandEnviron(%q) = %q, want %q", tt.in, got[0], tt.want)
			}
		})
	}
}
