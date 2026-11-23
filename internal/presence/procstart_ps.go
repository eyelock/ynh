//go:build !linux && !windows

package presence

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ProcessStart returns the process's start time in Unix seconds, from ps(1)
// (macOS and the BSDs), or an error when no such process exists.
func ProcessStart(pid int) (int64, error) {
	cmd := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprint(pid))
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "TZ=UTC")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	s := strings.Join(strings.Fields(string(out)), " ")
	if s == "" {
		return 0, fmt.Errorf("no process %d", pid)
	}
	t, err := time.Parse("Mon Jan 2 15:04:05 2006", s)
	if err != nil {
		return 0, fmt.Errorf("parsing ps start time %q: %w", s, err)
	}
	return t.Unix(), nil
}
