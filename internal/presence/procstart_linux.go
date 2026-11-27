package presence

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ProcessStart returns the process's start as an opaque, stable number, or an
// error when no such process exists. On Linux it is the starttime field of
// /proc/<pid>/stat (clock ticks since boot), which a reused pid cannot share.
func ProcessStart(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// The command name, in parentheses, may hold spaces: split after the last ")".
	s := string(data)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(s[i+1:])
	// fields[0] is state (field 3); starttime is field 22, so index 19.
	if len(fields) < 20 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	return strconv.ParseInt(fields[19], 10, 64)
}
