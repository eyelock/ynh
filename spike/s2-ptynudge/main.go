//go:build !windows

// Command s2-ptynudge is a spike (ynh #908, S2): it runs a vendor CLI inside a
// pty that it owns, passes the human's keystrokes and the vendor's output
// through untouched, and, when a nudge is pending and the session is idle and
// the human is not mid-typing, types the nudge into the vendor.
//
//	s2-ptynudge [-nudge-file F] [-idle D] [-quiet D] [-paste] [-log F] -- cmd args...
//
// A nudge is pending while F exists and is non-empty; it is consumed on send.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

type state struct {
	mu        sync.Mutex
	lastOut   time.Time
	lastIn    time.Time
	dirty     bool // human typed printable bytes since the last Enter/Ctrl-C/Ctrl-U
	bracketed bool // vendor enabled bracketed paste (?2004h)
	altScreen bool
	tail      []byte
}

func main() {
	nf := flag.String("nudge-file", "", "file whose content is the pending nudge")
	idle := flag.Duration("idle", 2*time.Second, "vendor output quiet for this long = idle")
	quiet := flag.Duration("quiet", 3*time.Second, "human input quiet for this long")
	paste := flag.Bool("paste", false, "wrap the nudge in bracketed paste when the vendor enabled it")
	holdDirty := flag.Bool("hold-dirty", true, "never nudge while the human has unsent typed text")
	delay := flag.Duration("enter-delay", 150*time.Millisecond, "gap between nudge text and Enter")
	logp := flag.String("log", "", "event log file")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: s2-ptynudge [flags] -- cmd args")
		os.Exit(2)
	}
	var lf *os.File
	if *logp != "" {
		lf, _ = os.OpenFile(*logp, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	logf := func(f string, a ...any) {
		if lf != nil {
			fmt.Fprintf(lf, "%s "+f+"\n", append([]any{time.Now().Format("15:04:05.000")}, a...)...)
		}
	}

	cmd := exec.Command(args[0], args[1:]...)
	ptmx, err := pty.StartWithSize(cmd, mustSize())
	if err != nil {
		fmt.Fprintln(os.Stderr, "pty:", err)
		os.Exit(1)
	}
	defer ptmx.Close()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			_ = pty.InheritSize(os.Stdin, ptmx)
		}
	}()

	if term.IsTerminal(int(os.Stdin.Fd())) {
		old, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err == nil {
			defer term.Restore(int(os.Stdin.Fd()), old)
		}
	}

	st := &state{lastOut: time.Now(), lastIn: time.Now()}

	// human -> vendor
	go func() {
		esc := 0
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				st.mu.Lock()
				// Terminal replies (cursor position, device attributes, focus
				// events) arrive on stdin too and are not the human typing.
				for _, b := range buf[:n] {
					switch esc {
					case 0:
						if b == 0x1b {
							esc = 1
							continue
						}
					case 1:
						if b == '[' || b == 'O' {
							esc = 2
							continue
						}
						esc = 0 // lone ESC + key: human
						st.lastIn = time.Now()
						st.dirty = false
						continue
					case 2:
						if b >= 0x40 && b <= 0x7e {
							esc = 0
						}
						continue
					}
					st.lastIn = time.Now()
					switch {
					case b == '\r' || b == '\n' || b == 0x03 || b == 0x15:
						st.dirty = false
					case b >= 0x20 && b != 0x7f:
						st.dirty = true
					}
				}
				st.mu.Unlock()
				_, _ = ptmx.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// vendor -> human
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 32768)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				st.mu.Lock()
				st.lastOut = time.Now()
				st.tail = append(st.tail, buf[:n]...)
				if len(st.tail) > 4096 {
					st.tail = st.tail[len(st.tail)-4096:]
				}
				if i := lastIndex(st.tail, "\x1b[?2004"); i >= 0 && i+7 < len(st.tail) {
					st.bracketed = st.tail[i+7] == 'h'
				}
				st.mu.Unlock()
				_, _ = os.Stdout.Write(buf[:n])
			}
			if err != nil {
				close(done)
				return
			}
		}
	}()

	// nudger
	go func() {
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		var waitedSince time.Time
		lastHeld := ""
		for range t.C {
			if *nf == "" {
				continue
			}
			b, err := os.ReadFile(*nf)
			if err != nil || len(b) == 0 {
				waitedSince = time.Time{}
				continue
			}
			if waitedSince.IsZero() {
				waitedSince = time.Now()
				logf("nudge pending")
			}
			st.mu.Lock()
			now := time.Now()
			okIdle := now.Sub(st.lastOut) >= *idle
			okQuiet := now.Sub(st.lastIn) >= *quiet
			dirty := st.dirty
			br := st.bracketed
			st.mu.Unlock()
			if !okIdle || !okQuiet || (dirty && *holdDirty) {
				if k := fmt.Sprint(okIdle, okQuiet, dirty); k != lastHeld {
					lastHeld = k
					logf("held idle=%v quiet=%v dirty=%v", okIdle, okQuiet, dirty)
				}
				continue
			}
			_ = os.Remove(*nf)
			txt := string(b)
			for len(txt) > 0 && (txt[len(txt)-1] == '\n') {
				txt = txt[:len(txt)-1]
			}
			if *paste && br {
				txt = "\x1b[200~" + txt + "\x1b[201~"
			}
			_, _ = ptmx.Write([]byte(txt))
			time.Sleep(*delay)
			_, _ = ptmx.Write([]byte("\r"))
			logf("nudge sent after %v bracketed=%v paste=%v", time.Since(waitedSince), br, *paste)
			waitedSince = time.Time{}
		}
	}()

	err = cmd.Wait()
	<-done
	if ee, ok := err.(*exec.ExitError); ok {
		os.Exit(ee.ExitCode())
	}
	_ = io.EOF
}

func mustSize() *pty.Winsize {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return &pty.Winsize{Rows: 40, Cols: 120}
	}
	return &pty.Winsize{Rows: uint16(h), Cols: uint16(w)}
}

func lastIndex(b []byte, s string) int {
	for i := len(b) - len(s); i >= 0; i-- {
		if string(b[i:i+len(s)]) == s {
			return i
		}
	}
	return -1
}
