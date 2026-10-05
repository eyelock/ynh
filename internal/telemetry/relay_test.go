package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeRelayEnv makes the test binary act as `ynr relay`, in the mode it
// names, instead of running the tests. fakeRelayLogEnv names the file it
// records into.
const (
	fakeRelayEnv    = "YNH_TEST_FAKE_RELAY"
	fakeRelayLogEnv = "YNH_TEST_FAKE_RELAY_LOG"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeRelayEnv); mode != "" {
		os.Exit(fakeRelay(mode, os.Getenv(fakeRelayLogEnv)))
	}
	os.Exit(m.Run())
}

// fakeRelay behaves as `ynr relay` would, or as one that goes wrong:
//
//	serve     prints its endpoint, records each request, exits 0 on SIGTERM
//	stubborn  prints its endpoint and ignores SIGTERM
//	die       prints its endpoint, then exits 1 a moment later
//	exit      exits 3 at once, printing nothing on stdout
//	silent    never prints anything
//	garbage   prints a line that is not the endpoint
//	remote    prints an endpoint that is not on loopback
func fakeRelay(mode, logPath string) int {
	logf := func(format string, args ...any) {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(f, format+"\n", args...)
		_ = f.Close()
	}
	logf("args %s", strings.Join(os.Args[1:], " "))
	stdin, _ := io.ReadAll(os.Stdin)
	logf("stdin %d", len(stdin))
	term := make(chan os.Signal, 1)
	switch mode {
	case "stubborn":
		signal.Ignore(syscall.SIGTERM)
	default:
		signal.Notify(term, syscall.SIGTERM)
	}
	switch mode {
	case "exit":
		fmt.Fprintln(os.Stderr, "boom: no spool")
		return 3
	case "silent":
		<-term
		return 0
	case "garbage":
		fmt.Println("listening, probably")
		<-term
		return 0
	case "remote":
		fmt.Println(`{"endpoint":"http://192.0.2.1:4318","pid":1}`)
		<-term
		return 0
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 1
	}
	go func() {
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			logf("request %s %s %d", r.Method, r.URL.Path, len(body))
		}))
	}()
	data, _ := json.Marshal(map[string]any{"endpoint": "http://" + ln.Addr().String(), "pid": os.Getpid()})
	fmt.Println(string(data))
	fmt.Fprintln(os.Stderr, "fake relay: listening")
	switch mode {
	case "die":
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintln(os.Stderr, "fake relay: crashed")
		return 1
	case "stubborn":
		select {}
	}
	<-term
	logf("sigterm")
	fmt.Fprintln(os.Stderr, "fake relay: 1 accepted")
	return 0
}

// startFake starts the test binary as a fake relay in mode, recording into
// the file it returns.
func startFake(t *testing.T, mode string, ready time.Duration) (*Relay, string, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signals")
	}
	logPath := filepath.Join(t.TempDir(), "relay.log")
	t.Setenv(fakeRelayEnv, mode)
	t.Setenv(fakeRelayLogEnv, logPath)
	r, err := StartRelay(os.Args[0], filepath.Join(t.TempDir(), "spool"), ready)
	if r != nil {
		t.Cleanup(func() { r.kill() })
	}
	return r, logPath, err
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(data)
}

// processGone reports whether pid no longer exists.
func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// A relay that starts is asked for json, gets no stdin, receives on its
// endpoint, and drains and exits on Stop's SIGTERM.
func TestStartRelay_StartsAndStops(t *testing.T) {
	r, logPath, err := startFake(t, "serve", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Endpoint, "http://127.0.0.1:") {
		t.Fatalf("endpoint %q", r.Endpoint)
	}
	resp, err := http.Post(r.Endpoint+"/v1/traces", "application/json", strings.NewReader(`{"resourceSpans":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	pid := r.cmd.Process.Pid
	if err := r.Stop(5 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !processGone(pid) {
		t.Errorf("relay %d still running after Stop", pid)
	}
	log := readLog(t, logPath)
	for _, want := range []string{"args relay --spool ", " --format json", "stdin 0", "request POST /v1/traces 20", "sigterm"} {
		if !strings.Contains(log, want) {
			t.Errorf("relay log lacks %q:\n%s", want, log)
		}
	}
	// A clean stop shows nothing, and a second Stop does nothing.
	if err := r.Stop(time.Second); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

// Every way a relay can fail to start is an error within the bound, and
// leaves no process running.
func TestStartRelay_Failures(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{"exit", "exited without printing its endpoint: boom: no spool"},
		{"silent", "printed no endpoint within"},
		{"garbage", "first line is not its endpoint"},
		{"remote", "not on a loopback address"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			start := time.Now()
			r, logPath, err := startFake(t, tt.mode, 500*time.Millisecond)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if r != nil {
				t.Fatal("a relay that failed to start was returned")
			}
			if d := time.Since(start); d > 3*time.Second {
				t.Errorf("failing took %v", d)
			}
			// The fake logs its args first, so it did start.
			if !strings.Contains(readLog(t, logPath), "args relay") {
				t.Fatal("the fake relay never ran")
			}
		})
	}
}

func TestStartRelay_MissingBinary(t *testing.T) {
	if _, err := StartRelay(filepath.Join(t.TempDir(), "ynr"), t.TempDir(), time.Second); err == nil {
		t.Fatal("a missing binary started")
	}
}

// A relay that ignores SIGTERM is killed once the bound passes.
func TestRelayStop_KillsAfterBound(t *testing.T) {
	r, _, err := startFake(t, "stubborn", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pid := r.cmd.Process.Pid
	start := time.Now()
	err = r.Stop(300 * time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "was killed") {
		t.Fatalf("Stop = %v, want killed after the bound", err)
	}
	if d := time.Since(start); d < 300*time.Millisecond || d > 5*time.Second {
		t.Errorf("Stop took %v", d)
	}
	if !processGone(pid) {
		t.Errorf("relay %d still running", pid)
	}
}

// A relay that dies during the run is reported, with its stderr, when the
// run stops it.
func TestRelayStop_DiedDuringRun(t *testing.T) {
	r, _, err := startFake(t, "die", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !r.Exited() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	err = r.Stop(time.Second)
	if err == nil || !strings.Contains(err.Error(), "exited during the run") || !strings.Contains(err.Error(), "fake relay: crashed") {
		t.Fatalf("Stop = %v", err)
	}
}

func TestParseRelayReady(t *testing.T) {
	tests := []struct {
		line, want, err string
	}{
		{line: `{"endpoint":"http://127.0.0.1:4318","pid":7}` + "\n", want: "http://127.0.0.1:4318"},
		{line: `{"endpoint":"http://[::1]:4318/","pid":7}`, want: "http://[::1]:4318"},
		{line: "", err: "without printing"},
		{line: "http://127.0.0.1:4318", err: "not its endpoint"},
		{line: `{"endpoint":"https://127.0.0.1:4318"}`, err: "not an http"},
		{line: `{"endpoint":"http://127.0.0.1"}`, err: "not an http"},
		{line: `{"endpoint":"http://127.0.0.1:4318/v1/traces"}`, err: "not an http"},
		{line: `{"endpoint":"http://u:p@127.0.0.1:4318"}`, err: "not an http"},
		{line: `{"endpoint":"http://localhost:4318"}`, err: "not on a loopback"},
		{line: `{"endpoint":"http://10.1.2.3:4318"}`, err: "not on a loopback"},
	}
	for _, tt := range tests {
		got, err := parseRelayReady(tt.line)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%q: err = %v, want %q", tt.line, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("%q = %q, %v; want %q", tt.line, got, err, tt.want)
		}
	}
}

func TestTailBuffer(t *testing.T) {
	b := &tailBuffer{limit: 4}
	_, _ = b.Write([]byte("abc"))
	_, _ = b.Write([]byte("defg"))
	if b.String() != "defg" {
		t.Errorf("tail = %q", b.String())
	}
	if got := truncate(strings.Repeat("x", 10), 4); got != "xxxx..." {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("ok", 4); got != "ok" {
		t.Errorf("truncate short = %q", got)
	}
}
