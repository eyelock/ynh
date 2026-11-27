package presence

import (
	"os/exec"
	"testing"
	"time"
)

// A vendor pid that stops being the recorded vendor between the first signal
// and the kill is never killed: the kill goes to the wrapper instead.
func TestStopKillNeverHitsAPidReusedDuringTheWait(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	// The wrapper ignores SIGTERM, so Stop reaches its kill; the "vendor"
	// ignores it too, and is the process whose pid is "reused".
	wrapper := exec.Command("sh", "-c", `trap "" TERM; sleep 60 & wait`)
	vendor := exec.Command("sh", "-c", `trap "" TERM; sleep 60 & wait`)
	for _, c := range []*exec.Cmd{wrapper, vendor} {
		if err := c.Start(); err != nil {
			t.Skipf("sh unavailable: %v", err)
		}
		go func(c *exec.Cmd) { _ = c.Wait() }(c)
		defer func(c *exec.Cmd) { _ = c.Process.Kill() }(c)
	}
	time.Sleep(200 * time.Millisecond)
	wstart, err := ProcessStart(wrapper.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	vstart, _ := ProcessStart(vendor.Process.Pid)
	rec := Record{SessionID: "s", Harness: "local--h", WrapperPID: wrapper.Process.Pid, StartTime: wstart,
		VendorPID: vendor.Process.Pid, VendorStart: vstart, Mode: ModeInteractive}
	if err := Save(rec); err != nil {
		t.Fatal(err)
	}
	e, err := Find("s")
	if err != nil {
		t.Fatal(err)
	}

	reused := false
	processStart = func(pid int) (int64, error) {
		start, err := ProcessStart(pid)
		if reused && pid == vendor.Process.Pid {
			return start + 999, err // another process now has this pid
		}
		return start, err
	}
	afterTerm = func() { reused = true }
	defer func() { processStart, afterTerm = ProcessStart, nil }()

	// The first target is the vendor, as recorded.
	if got := StopTarget(e.Record); got != vendor.Process.Pid {
		t.Fatalf("before the change the target is %d, want the vendor %d", got, vendor.Process.Pid)
	}
	if err := Stop(e, 300*time.Millisecond); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if Alive(e.Record) {
		t.Error("the wrapper survived the kill")
	}
	if _, err := ProcessStart(vendor.Process.Pid); err != nil {
		t.Errorf("the process that now has the vendor's pid was killed: %v", err)
	}
}
