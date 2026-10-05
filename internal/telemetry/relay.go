package telemetry

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Relay bounds. The relay is started for the length of one run, so neither
// its start nor its stop may hold the run up for long (NFR-1).
const (
	// RelayReadyTimeout bounds the wait for the relay's first line, its
	// endpoint.
	RelayReadyTimeout = 5 * time.Second
	// RelayStopTimeout bounds the wait, after SIGTERM, for the relay to
	// drain the requests in flight, flush and exit. Past it the relay is
	// killed.
	RelayStopTimeout = 10 * time.Second
	// relayStderrLimit keeps the tail of the relay's stderr, which ynh shows
	// only when the relay fails.
	relayStderrLimit = 8 << 10
)

// Relay is a running `ynr relay`: an OTLP/HTTP receiver on a loopback port
// that writes what a vendor CLI exports into a spool folder (ynr ADR-004).
// ynh starts one for the length of a run when the telemetry relay setting is
// on, and stops it after the vendor exits.
type Relay struct {
	// Endpoint is the relay's OTLP/HTTP base URL, such as
	// http://127.0.0.1:41234, as the relay printed it.
	Endpoint string

	cmd    *exec.Cmd
	stdin  io.WriteCloser // held open for the run; closing it stops the relay
	stderr *tailBuffer
	done   chan struct{} // closed when the process has been reaped
	err    error         // how it exited; read only after done

	stopOnce sync.Once
}

// relayReady is the relay's first line of output with --format json.
type relayReady struct {
	Endpoint string `json:"endpoint"`
	PID      int    `json:"pid"`
}

// StartRelay runs `<bin> relay --spool <spoolDir> --format json
// --exit-on-stdin-eof` and waits, for at most readyTimeout, for its first
// line: the endpoint. Its stdin is a pipe ynh holds open until Stop, and its stderr is kept in a bounded buffer for Stop to report
// if it fails. Anything other than a well-formed loopback endpoint in time
// is a failure, and the process is killed before StartRelay returns, so a
// relay that cannot be used is never left running.
func StartRelay(bin, spoolDir string, readyTimeout time.Duration) (*Relay, error) {
	cmd := exec.Command(bin, "relay", "--spool", spoolDir, "--format", "json", "--exit-on-stdin-eof")
	// The relay's stdin is a pipe only ynh holds, and never writes to. When
	// ynh exits, by any means, kill -9 included, the kernel closes it, and
	// --exit-on-stdin-eof makes the relay drain and exit as on SIGTERM. So
	// no relay outlives its run. It never reads the operator's terminal or
	// ynh's control channel. Go opens the pipe close-on-exec, so no other
	// child of ynh, such as the worker, holds it open.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("relay stdin: %w", err)
	}
	tail := &tailBuffer{limit: relayStderrLimit}
	cmd.Stderr = tail
	cmd.SysProcAttr = relaySysProcAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("relay stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting relay: %w", err)
	}
	r := &Relay{cmd: cmd, stdin: stdin, stderr: tail, done: make(chan struct{})}

	lines := make(chan string, 1)
	go func() {
		br := bufio.NewReader(stdout)
		line, _ := br.ReadString('\n')
		lines <- line
		// Keep reading, so a relay that writes more to stdout never blocks
		// on a full pipe; the first line is the whole contract.
		_, _ = io.Copy(io.Discard, br)
		r.err = cmd.Wait()
		close(r.done)
	}()

	timer := time.NewTimer(readyTimeout)
	defer timer.Stop()
	select {
	case line := <-lines:
		endpoint, perr := parseRelayReady(line)
		if perr != nil {
			r.kill()
			return nil, fmt.Errorf("%w%s", perr, r.stderrNote())
		}
		r.Endpoint = endpoint
		return r, nil
	case <-timer.C:
		r.kill()
		return nil, fmt.Errorf("relay printed no endpoint within %v%s", readyTimeout, r.stderrNote())
	}
}

// parseRelayReady reads the relay's first line. The endpoint must be plain
// http on a loopback address: it is handed to the vendor CLI as where to
// send its telemetry, and anything else would send it off the host.
func parseRelayReady(line string) (string, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", errors.New("relay exited without printing its endpoint")
	}
	var ready relayReady
	if err := json.Unmarshal([]byte(line), &ready); err != nil {
		return "", fmt.Errorf("relay's first line is not its endpoint: %q", truncate(line, 120))
	}
	u, err := url.Parse(ready.Endpoint)
	if err != nil || u.Scheme != "http" || u.Port() == "" || u.Path != "" && u.Path != "/" || u.User != nil {
		return "", fmt.Errorf("relay endpoint %q is not an http://host:port address", truncate(ready.Endpoint, 120))
	}
	if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("relay endpoint %q is not on a loopback address", truncate(ready.Endpoint, 120))
	}
	return "http://" + u.Host, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Exited reports whether the relay has exited, by Stop or on its own.
func (r *Relay) Exited() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// Stop closes the relay's stdin and sends it SIGTERM, so it drains the requests in flight,
// flushes and closes its spool file, and waits at most timeout for it to
// exit; past that it is killed. Stop is safe to call more than once.
//
// It returns an error, carrying the tail of the relay's stderr, when the
// relay had already died during the run or did not exit cleanly. A clean
// stop returns nil and its summary is not shown.
func (r *Relay) Stop(timeout time.Duration) error {
	var err error
	r.stopOnce.Do(func() {
		select {
		case <-r.done:
			// Died before it was asked to stop.
			err = fmt.Errorf("relay exited during the run (%v)%s", r.err, r.stderrNote())
			return
		default:
		}
		// Either one makes the relay drain and exit; both, in case a
		// signal is lost or stdin is not watched.
		_ = r.stdin.Close()
		_ = r.cmd.Process.Signal(syscall.SIGTERM)
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-r.done:
			if r.err != nil {
				err = fmt.Errorf("relay exited with %v%s", r.err, r.stderrNote())
			}
		case <-timer.C:
			r.kill()
			err = fmt.Errorf("relay did not exit within %v of SIGTERM and was killed%s", timeout, r.stderrNote())
		}
	})
	return err
}

// kill ends the relay at once and waits for it to be reaped.
func (r *Relay) kill() {
	_ = r.stdin.Close()
	_ = r.cmd.Process.Kill()
	<-r.done
}

// stderrNote is the tail of the relay's stderr, for an error message.
func (r *Relay) stderrNote() string {
	s := strings.TrimSpace(r.stderr.String())
	if s == "" {
		return ""
	}
	return ": " + s
}

// tailBuffer keeps the last limit bytes written to it.
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.limit; over > 0 {
		b.buf = append(b.buf[:0], b.buf[over:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
