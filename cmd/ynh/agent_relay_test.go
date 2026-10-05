package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/telemetry"
)

// claudeRelaySettings is what Claude Code is given with a relay at
// relayStubEndpoint: exactly what ynr ADR-004 records as verified, then
// every content switch off.
func claudeRelaySettings(endpoint string) []string {
	return []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY=1",
		"OTEL_TRACES_EXPORTER=otlp",
		"OTEL_METRICS_EXPORTER=otlp",
		"OTEL_LOGS_EXPORTER=otlp",
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_EXPORTER_OTLP_ENDPOINT=" + endpoint,
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=" + endpoint + "/v1/traces",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=" + endpoint + "/v1/metrics",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=" + endpoint + "/v1/logs",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1",
		"OTEL_LOG_USER_PROMPTS=0",
		"OTEL_LOG_ASSISTANT_RESPONSES=0",
		"OTEL_LOG_TOOL_DETAILS=0",
		"OTEL_LOG_TOOL_CONTENT=0",
		"OTEL_LOG_RAW_API_BODIES=0",
		"OTEL_LOG_MANAGED_SETTINGS=0",
		"ENABLE_BETA_TRACING_DETAILED=0",
	}
}

// The endpoint every stub relay prints. Nothing listens there: the stub
// claude in these tests sends nothing.
const relayStubEndpoint = "http://127.0.0.1:9"

// stubRelay is a fake ynr on PATH and the files it records into.
type stubRelay struct{ dir, args, pid, log string }

// installStubYnr puts a shell `ynr` first on PATH that records its
// arguments, pid and SIGTERM, and behaves as mode says:
//
//	serve     prints the endpoint and runs until SIGTERM or stdin closes
//	stubborn  prints the endpoint and ignores SIGTERM
//	die       prints the endpoint and exits at once
//	exit      exits 3 without printing anything
//	silent    runs without printing anything
//	garbage   prints a line that is not the endpoint
func installStubYnr(t *testing.T, mode string) stubRelay {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	dir := t.TempDir()
	s := stubRelay{dir: dir, args: filepath.Join(dir, "args"), pid: filepath.Join(dir, "pid"), log: filepath.Join(dir, "log")}
	ready := `echo '{"endpoint":"` + relayStubEndpoint + `","pid":'$$'}'`
	body := map[string]string{
		// As ynr relay --exit-on-stdin-eof: stdin closing stops it too.
		// fd 3, because a background job's stdin is /dev/null.
		"serve": "trap 'echo term >> \"$D/log\"; exit 0' TERM\nexec 3<&0\n" +
			"( cat <&3 >/dev/null; echo eof >> \"$D/log\"; kill -TERM $$ ) &\n" + ready + "\nwhile :; do sleep 0.05; done\n",
		"stubborn": "trap '' TERM\n" + ready + "\nwhile :; do sleep 0.05; done\n",
		"die":      ready + "\necho 'relay: crashed' >&2\nexit 1\n",
		"exit":     "echo 'relay: cannot open spool' >&2\nexit 3\n",
		"silent":   "trap 'exit 0' TERM\nwhile :; do sleep 0.05; done\n",
		"garbage":  "trap 'exit 0' TERM\necho 'starting up'\nwhile :; do sleep 0.05; done\n",
	}[mode]
	script := "#!/bin/sh\nD='" + dir + "'\necho \"$@\" > \"$D/args\"\necho $$ > \"$D/pid\"\n" + body
	if err := os.WriteFile(filepath.Join(dir, "ynr"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return s
}

// hideFromPath drops every PATH entry that holds name, such as a real ynr
// a developer has installed.
func hideFromPath(t *testing.T, name string) {
	t.Helper()
	var keep []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			keep = append(keep, dir)
		}
	}
	t.Setenv("PATH", strings.Join(keep, string(os.PathListSeparator)))
}

func (s stubRelay) started() bool {
	_, err := os.Stat(s.args)
	return err == nil
}

func (s stubRelay) pidValue(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(s.pid)
	if err != nil {
		t.Fatalf("the relay never started: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// gone waits a moment for pid to disappear, and reports whether it did.
func gone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// recordingClaude puts a fake claude first on PATH that answers each user
// message with one metered turn, and records its environment and its
// arguments, one per line.
func recordingClaude(t *testing.T) (envFile, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	dir := t.TempDir()
	envFile, argsFile = filepath.Join(dir, "env"), filepath.Join(dir, "args")
	script := "#!/bin/sh\nenv > '" + envFile + "'\n" +
		"for a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + argsFile + "'\n" +
		"while IFS= read -r line; do\n" +
		"  case \"$line\" in\n" +
		"    *'\"type\":\"user\"'*)\n" +
		"      echo '{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"claude-stub-1\"}'\n" +
		"      echo '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"done\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}'\n" +
		"      ;;\n" +
		"  esac\n" +
		"done\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return envFile, argsFile
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// The relay starts only when the setting is on, the destination is the
// spool, ynr is on PATH and the backend is claude. Then claude gets exactly
// the relay's settings, in its environment and in --settings, with every
// content switch off even when the harness passes one through turned on,
// and the relay is stopped after the run. In every other case, and when the
// relay fails, the result and exit code are those of a run without it, and
// the worker gets none of the relay's settings.
func TestCmdAgentRun_TelemetryRelay(t *testing.T) {
	tests := []struct {
		name    string
		relay   string // stub ynr mode; "" for none on PATH
		setting string // "env", "flag", "config", "config+env off" or "off"
		spool   string // "spool", "none" or "otlp"
		backend string
		started bool // the relay is asked to start
		serving bool // and the worker is configured for it
		note    string
		code    int // beside agent.ExitStuck, the code a claude run ends with
	}{
		{name: "all conditions", relay: "serve", setting: "env", spool: "spool", started: true, serving: true},
		{name: "flag", relay: "serve", setting: "flag", spool: "spool", started: true, serving: true},
		{name: "configuration", relay: "serve", setting: "config", spool: "spool", started: true, serving: true},
		{name: "the variable turns the configuration off", relay: "serve", setting: "config+env off", spool: "spool"},
		{name: "off", relay: "serve", setting: "off", spool: "spool"},
		{name: "no ynr", setting: "env", spool: "spool", note: "ynr is not on PATH"},
		{name: "no spool", relay: "serve", setting: "env", spool: "none", note: "there is no spool folder"},
		{name: "operator's OTLP", relay: "serve", setting: "env", spool: "otlp", note: "OTEL_EXPORTER_OTLP_* is set"},
		{name: "srt", relay: "serve", setting: "env", spool: "spool", backend: "srt", note: "--sandbox srt would block the worker", code: agent.ExitWorkerError},
		{name: "codex", relay: "serve", setting: "env", spool: "spool", backend: "codex", note: "does not configure codex's telemetry", code: agent.ExitWorkerError},
		{name: "ynr exits at once", relay: "exit", setting: "env", spool: "spool", started: true, note: "relay: cannot open spool"},
		{name: "ynr never prints its endpoint", relay: "silent", setting: "env", spool: "spool", started: true, note: "printed no endpoint within"},
		{name: "ynr prints garbage", relay: "garbage", setting: "env", spool: "spool", started: true, note: "first line is not its endpoint"},
		{name: "ynr dies during the run", relay: "die", setting: "env", spool: "spool", started: true, serving: true, note: "exited during the run"},
		{name: "ynr ignores SIGTERM", relay: "stubborn", setting: "env", spool: "spool", started: true, serving: true, note: "was killed"},
	}
	saved := [2]time.Duration{relayReadyTimeout, relayStopTimeout}
	t.Cleanup(func() { relayReadyTimeout, relayStopTimeout = saved[0], saved[1] })
	relayReadyTimeout, relayStopTimeout = 500*time.Millisecond, 500*time.Millisecond

	var baseline map[string]any
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("YNH_HOME", home)
			t.Setenv("YNH_AGENT_SESSION", "")
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			// Turned on in the operator's environment and passed through:
			// the relay's settings must still win.
			t.Setenv("OTEL_LOG_USER_PROMPTS", "1")
			t.Setenv("OTEL_LOG_TOOL_CONTENT", "1")
			installListTestHarness(t, home, "relayprobe",
				`{"name":"relayprobe","version":"0.1.0","default_vendor":"claude","env_passthrough":["OTEL_LOG_USER_PROMPTS","OTEL_LOG_TOOL_CONTENT"]}`)

			var envFile, argsFile string
			if tt.backend == "codex" || tt.backend == "srt" {
				// srt is absent, so the worker fails to start either way.
				hideFromPath(t, "srt")
				vendorShims(t, "codex is a stub")
			} else {
				envFile, argsFile = recordingClaude(t)
			}
			var stub stubRelay
			if tt.relay != "" {
				stub = installStubYnr(t, tt.relay)
			} else {
				hideFromPath(t, "ynr")
			}
			spoolDir := filepath.Join(t.TempDir(), "spool")
			switch tt.spool {
			case "spool":
				t.Setenv("YNR_SPOOL", spoolDir)
			case "otlp":
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.example:4318")
			}
			args := []string{"--format", "json", "--worktree", t.TempDir(), "--task", "t", "--no-plan", "--harness", "local/relayprobe"}
			switch tt.backend {
			case "srt":
				args = append(args, "--sandbox", "srt")
			case "":
			default:
				args = append(args, "--backend", tt.backend)
			}
			switch tt.setting {
			case "env":
				t.Setenv(telemetryRelayEnv, "1")
			case "flag":
				args = append(args, "--telemetry-relay")
			case "config", "config+env off":
				if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"telemetry_relay":true}`), 0o644); err != nil {
					t.Fatal(err)
				}
				if tt.setting == "config+env off" {
					t.Setenv(telemetryRelayEnv, "0")
				}
			}

			var stdout, stderr bytes.Buffer
			err := cmdAgentRun(args, &stdout, &stderr, strings.NewReader(""))
			// The stub answers every turn alike and the harness has no
			// sensors, so a claude run ends stuck: the same every time.
			if tt.code == 0 {
				tt.code = agent.ExitStuck
			}
			if got := processExitCode(err); got != tt.code {
				t.Fatalf("exit %d, want %d (err %v)\nstderr: %s", got, tt.code, err, stderr.String())
			}
			if tt.note == "" {
				if strings.Contains(stderr.String(), "relay") {
					t.Errorf("stderr mentions the relay:\n%s", stderr.String())
				}
			} else {
				var notes []string
				for _, line := range strings.Split(stderr.String(), "\n") {
					if strings.Contains(line, "relay") {
						notes = append(notes, line)
					}
				}
				if len(notes) != 1 || !strings.Contains(notes[0], tt.note) {
					t.Errorf("stderr = %q, want one note with %q", stderr.String(), tt.note)
				}
			}

			// The result is the same with the relay as without it.
			if tt.backend == "" {
				result := normalisedResult(t, stdout.Bytes())
				delete(result, "worktree")
				delete(result, "session_dir")
				if baseline == nil {
					baseline = result
				} else {
					got, _ := json.Marshal(result)
					want, _ := json.Marshal(baseline)
					if !bytes.Equal(got, want) {
						t.Errorf("result differs:\n got %s\nwant %s", got, want)
					}
				}
			}

			if stub.dir != "" && stub.started() != tt.started {
				t.Fatalf("relay started = %v, want %v", stub.started(), tt.started)
			}
			if tt.started {
				if got := readLines(t, stub.args); len(got) != 1 || got[0] != "relay --spool "+spoolDir+" --format json --exit-on-stdin-eof" {
					t.Errorf("relay args = %q", got)
				}
				pid := stub.pidValue(t)
				if !gone(pid, 2*time.Second) {
					t.Errorf("relay %d still running after the run", pid)
				}
				if tt.relay == "serve" && !slices.Contains(readLines(t, stub.log), "term") {
					t.Errorf("relay was not stopped with SIGTERM")
				}
			}
			if envFile == "" {
				return
			}

			env := readLines(t, envFile)
			claudeArgs := readLines(t, argsFile)
			settingsAt := slices.Index(claudeArgs, "--settings")
			if !tt.serving {
				for _, kv := range env {
					if strings.HasPrefix(kv, "CLAUDE_CODE_ENABLE_TELEMETRY") || strings.HasPrefix(kv, "OTEL_EXPORTER_OTLP") {
						t.Errorf("worker got %s without a relay", kv)
					}
				}
				if settingsAt >= 0 {
					t.Errorf("worker got --settings without a relay: %v", claudeArgs)
				}
				return
			}
			// Each setting exactly once, with ynh's value: the operator's
			// OTEL_LOG_USER_PROMPTS=1 passed through is gone.
			for _, want := range claudeRelaySettings(relayStubEndpoint) {
				name, _, _ := strings.Cut(want, "=")
				var got []string
				for _, kv := range env {
					if strings.HasPrefix(kv, name+"=") {
						got = append(got, kv)
					}
				}
				if len(got) != 1 || got[0] != want {
					t.Errorf("worker env %s = %q, want exactly %q", name, got, want)
				}
			}
			if !slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "TRACEPARENT=00-") }) {
				t.Errorf("worker env has no TRACEPARENT for the run span")
			}
			if settingsAt < 0 || settingsAt+1 >= len(claudeArgs) {
				t.Fatalf("worker args lack --settings: %q", claudeArgs)
			}
			var settings struct {
				Env map[string]string `json:"env"`
			}
			if err := json.Unmarshal([]byte(claudeArgs[settingsAt+1]), &settings); err != nil {
				t.Fatalf("--settings %q: %v", claudeArgs[settingsAt+1], err)
			}
			for _, want := range claudeRelaySettings(relayStubEndpoint) {
				name, value, _ := strings.Cut(want, "=")
				if settings.Env[name] != value {
					t.Errorf("--settings env %s = %q, want %q", name, settings.Env[name], value)
				}
			}
		})
	}
}

func TestTelemetryRelaySetting(t *testing.T) {
	tests := []struct {
		name   string
		flag   bool
		env    string
		config string
		want   bool
		note   bool
	}{
		{name: "nothing", want: false},
		{name: "flag", flag: true, env: "off", want: true},
		{name: "env on", env: "1", want: true},
		{name: "env yes", env: "YES", want: true},
		{name: "env off over config", env: "false", config: `{"telemetry_relay":true}`, want: false},
		{name: "config", config: `{"telemetry_relay":true}`, want: true},
		{name: "config off", config: `{"telemetry_relay":false}`, want: false},
		{name: "unreadable config", config: `{`, want: false},
		{name: "unrecognised env", env: "maybe", config: `{"telemetry_relay":true}`, want: false, note: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("YNH_HOME", home)
			t.Setenv(telemetryRelayEnv, tt.env)
			if tt.config != "" {
				if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(tt.config), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var stderr bytes.Buffer
			if got := telemetryRelaySetting(tt.flag, &stderr); got != tt.want {
				t.Errorf("setting = %v, want %v", got, tt.want)
			}
			if (stderr.Len() > 0) != tt.note {
				t.Errorf("stderr = %q", stderr.String())
			}
		})
	}
}

// Interrupting ynh stops the relay too: none is left running.
func TestAgentRun_RelayStoppedOnInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals and shell stubs")
	}
	sigs := []struct {
		name string
		sig  syscall.Signal
		code int
	}{
		{"SIGINT", syscall.SIGINT, agent.ExitInterrupted},
		{"SIGTERM", syscall.SIGTERM, agent.ExitInterrupted},
		// SIGKILL cannot be handled: the relay's stdin closes with ynh,
		// and that stops it, on every platform.
		{"SIGKILL", syscall.SIGKILL, -1},
	}
	for _, s := range sigs {
		t.Run(s.name, func(t *testing.T) {
			stub := installStubYnr(t, "serve")
			claudeDir := t.TempDir()
			claudePid := filepath.Join(claudeDir, "pid")
			script := "#!/bin/sh\necho $$ > '" + claudePid + "'\nexec sleep 60\n"
			if err := os.WriteFile(filepath.Join(claudeDir, "claude"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for _, f := range []string{claudePid, stub.pid} {
					if data, err := os.ReadFile(f); err == nil {
						if pid, perr := strconv.Atoi(strings.TrimSpace(string(data))); perr == nil {
							_ = syscall.Kill(pid, syscall.SIGKILL)
						}
					}
				}
			})
			cmd := exec.Command(os.Args[0], "agent", "run", "--task", "t", "--no-plan", "--worktree", t.TempDir(), "--telemetry-relay")
			cmd.Env = append(os.Environ(),
				execMainEnv+"=1",
				"PATH="+claudeDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"YNH_HOME="+t.TempDir(),
				"YNH_AGENT_SESSION=",
				"YNR_SPOOL="+filepath.Join(t.TempDir(), "spool"),
			)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(15 * time.Second)
			for {
				_, errC := os.Stat(claudePid)
				_, errR := os.Stat(stub.pid)
				if errC == nil && errR == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatalf("the worker or the relay never started\n%s", stderr.String())
				}
				time.Sleep(20 * time.Millisecond)
			}
			relayPid := stub.pidValue(t)
			if err := cmd.Process.Signal(s.sig); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			if s.code >= 0 {
				var ee *exec.ExitError
				if !errors.As(err, &ee) || ee.ExitCode() != s.code {
					t.Errorf("ynh exited %v, want %d\n%s", err, s.code, stderr.String())
				}
				if !slices.Contains(readLines(t, stub.log), "term") {
					t.Errorf("the relay was not sent SIGTERM")
				}
				if strings.Contains(stderr.String(), "relay") {
					t.Errorf("stderr mentions the relay: %s", stderr.String())
				}
			}
			if !gone(relayPid, 5*time.Second) {
				t.Errorf("relay %d left running after ynh got %s", relayPid, s.name)
			}
			// Killed outright, ynh stopped nothing itself: its end of the
			// relay's stdin closed with it. The worker, still running,
			// must not hold that pipe open.
			if s.sig == syscall.SIGKILL && !slices.Contains(readLines(t, stub.log), "eof") {
				t.Errorf("the relay did not see its stdin close: %q", readLines(t, stub.log))
			}
		})
	}
}

// fakeClaudeEnv makes the test binary act as claude, recording what it got
// into the file the variable names.
const fakeClaudeEnv = "YNH_TEST_FAKE_CLAUDE"

// fakeClaude is a claude that exports one span over OTLP/HTTP JSON, as a
// child of the TRACEPARENT it was given, to OTEL_EXPORTER_OTLP_TRACES_ENDPOINT, then
// answers each user message with one turn.
func fakeClaude(record string) int {
	var notes []string
	notes = append(notes, os.Environ()...)
	tp := strings.Split(os.Getenv("TRACEPARENT"), "-")
	// The traces endpoint ynh pins, with its full OTLP/HTTP path, as
	// Claude Code would use it.
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); endpoint != "" && len(tp) == 4 {
		now := time.Now().UnixNano()
		body := fmt.Sprintf(`{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"claude-code-stub"}}]},`+
			`"scopeSpans":[{"scope":{"name":"stub"},"spans":[{"traceId":%q,"spanId":"5ca1ab1e5ca1ab1e","parentSpanId":%q,`+
			`"name":"claude_code.interaction","kind":1,"startTimeUnixNano":"%d","endTimeUnixNano":"%d"}]}]}]}`,
			tp[1], tp[2], now-1000, now)
		resp, err := http.Post(endpoint, "application/json", strings.NewReader(body))
		if err != nil {
			notes = append(notes, "POST error "+err.Error())
		} else {
			_ = resp.Body.Close()
			notes = append(notes, "POST status "+strconv.Itoa(resp.StatusCode))
		}
	}
	_ = os.WriteFile(record, []byte(strings.Join(notes, "\n")+"\n"), 0o600)
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		if strings.Contains(sc.Text(), `"type":"user"`) {
			fmt.Println(`{"type":"system","subtype":"init","model":"claude-stub-1"}`)
			fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"done","usage":{"input_tokens":10,"output_tokens":5}}`)
		}
	}
	return 0
}

// With the real ynr, a span claude exports lands in the run's spool
// folder, in the run's trace, as a child of the run's span. CI has no ynr
// unless it installs one, and then this skips.
func TestAgentRun_RealRelayJoinsRunTrace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub is POSIX-only")
	}
	if _, err := exec.LookPath("ynr"); err != nil {
		t.Skip("ynr is not on PATH")
	}
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("YNH_AGENT_SESSION", "")
	t.Setenv("TRACEPARENT", testTraceParent)
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := "#!/bin/sh\n" + fakeClaudeEnv + "='" + record + "' exec '" + os.Args[0] + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	spoolDir := filepath.Join(t.TempDir(), "spool")
	t.Setenv("YNR_SPOOL", spoolDir)

	var stdout, stderr bytes.Buffer
	err := cmdAgentRun([]string{"--format", "json", "--worktree", t.TempDir(), "--task", "t", "--no-plan", "--telemetry-relay"},
		&stdout, &stderr, strings.NewReader(""))
	if code := processExitCode(err); code != 0 {
		t.Fatalf("exit %d: %v\n%s", code, err, stderr.String())
	}
	if strings.Contains(stderr.String(), "relay") {
		t.Errorf("stderr mentions the relay: %s", stderr.String())
	}
	got := readLines(t, record)
	if !slices.Contains(got, "POST status 200") {
		t.Fatalf("the stub's export did not succeed: %q", got[len(got)-1])
	}
	if !slices.Contains(got, "OTEL_LOG_USER_PROMPTS=0") {
		t.Errorf("claude ran without prompt logging forced off")
	}

	var run, vendor *spoolRecord
	recs := readSpoolRecords(t, spoolDir)
	for i := range recs {
		switch recs[i].name {
		case telemetry.SpanRun:
			run = &recs[i]
		case "claude_code.interaction":
			vendor = &recs[i]
		}
	}
	if run == nil || vendor == nil {
		t.Fatalf("spool holds %+v, want the run span and the vendor's", recs)
	}
	if vendor.traceID != run.traceID || vendor.parentID != run.spanID {
		t.Errorf("vendor span in trace %s under %s, want trace %s under the run span %s",
			vendor.traceID, vendor.parentID, run.traceID, run.spanID)
	}
	if run.traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("run trace %s, want the caller's", run.traceID)
	}
}
