package vendor

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// runChildProcess launches a command as a child process with signal forwarding.
// Unlike syscall.Exec, this keeps the parent alive so deferred cleanup can run.
// The child inherits stdin/stdout/stderr. SIGINT and SIGTERM are forwarded.
// The parent exits with the child's exit code.
func runChildProcess(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	// Forward signals to child.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range sigCh {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}()

	err := cmd.Wait()
	signal.Stop(sigCh)
	close(sigCh)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}
