package main

import (
	"errors"
	"io"
	"syscall"

	"go.opentelemetry.io/otel/attribute"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/telemetry"
	"github.com/eyelock/ynh/internal/vendor"
)

// launchRecord is `ynh run`'s own record of one launch, whichever vendor runs
// (docs/telemetry.md, "What a launch emits"). It is the ynh.session span and
// its started and finished events. With no destination it does nothing and
// changes nothing.
type launchRecord struct {
	tel     *telemetry.Telemetry
	session *telemetry.Session
	started bool // the vendor CLI was started
}

// startLaunchRecord opens the record and, when telemetry is on, puts the
// trace context and ynh.session.id into this process's environment, which the
// vendor CLI and every process `ynh run` starts inherit. Call end when the
// launch is over, on every path.
func startLaunchRecord(vendorName string, loaded *harness.Harness, harnessID, focus, profile string, resumed bool, labels map[string]string, stderr io.Writer) *launchRecord {
	opts := telemetryOptions
	opts.Labels = labels
	tel := telemetry.Setup(config.Version, opts, stderr)
	rec := &launchRecord{tel: tel}
	rec.session = tel.StartSession(launchAttributes(vendorName, loaded, harnessID, focus, profile, resumed)...)
	rec.session.ApplyEnv()
	return rec
}

// markStarted is called as the vendor CLI is about to start.
func (r *launchRecord) markStarted() {
	if r != nil {
		r.started = true
	}
}

// end finishes the record with how the launch ended and stops telemetry. err
// is what the launch returned.
func (r *launchRecord) end(err error) {
	outcome, code, sig := launchOutcome(err, r.started)
	attrs := []attribute.KeyValue{telemetry.AttrProcessExitCode.Int(code)}
	if sig != 0 {
		attrs = append(attrs, telemetry.AttrSessionSignal.String(signalName(sig)))
	}
	r.session.Finish(outcome, outcome == telemetry.SessionOutcomeExited, attrs...)
	r.tel.Shutdown()
}

// launchOutcome names how a launch ended, from what the launch returned and
// whether the vendor CLI was started. The code is the one `ynh run` exits
// with: the vendor's, 128+n for a signal as a shell reports it, or 1 for a
// launch ynh refused.
func launchOutcome(err error, started bool) (outcome string, code int, sig syscall.Signal) {
	var ee *vendor.ExitError
	switch {
	case err == nil:
		return telemetry.SessionOutcomeExited, 0, 0
	case errors.As(err, &ee) && ee.Signal != 0:
		return telemetry.SessionOutcomeSignalled, ee.Code, ee.Signal
	case errors.As(err, &ee):
		return telemetry.SessionOutcomeFailed, ee.Code, 0
	case started:
		// The vendor ran and ynh failed after it started.
		return telemetry.SessionOutcomeFailed, 1, 0
	default:
		return telemetry.SessionOutcomeRefused, 1, 0
	}
}

// signalName is the signal's name, such as SIGTERM.
func signalName(s syscall.Signal) string {
	names := map[syscall.Signal]string{
		syscall.SIGHUP: "SIGHUP", syscall.SIGINT: "SIGINT", syscall.SIGQUIT: "SIGQUIT",
		syscall.SIGILL: "SIGILL", syscall.SIGABRT: "SIGABRT", syscall.SIGFPE: "SIGFPE",
		syscall.SIGKILL: "SIGKILL", syscall.SIGSEGV: "SIGSEGV", syscall.SIGPIPE: "SIGPIPE",
		syscall.SIGALRM: "SIGALRM", syscall.SIGTERM: "SIGTERM", syscall.SIGBUS: "SIGBUS",
	}
	if n, ok := names[s]; ok {
		return n
	}
	return s.String()
}

// launchAttributes describes the launch as it was asked for. Every one is an
// id, a name, an enum or a flag (ynr ADR-006 rule 10): no paths, no prompt, no
// content, no tokens or cost.
func launchAttributes(vendorName string, loaded *harness.Harness, harnessID, focus, profile string, resumed bool) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	add := func(k attribute.Key, v string) {
		if v != "" {
			attrs = append(attrs, k.String(v))
		}
	}
	add(telemetry.AttrSessionVendor, vendorName)
	// Only a canonical id: a harness given by path has no id, and a value that
	// is not an id could be a path.
	if namespace.Classify(harnessID) == namespace.RefID {
		add(telemetry.AttrHarnessName, harnessID)
	}
	if loaded != nil {
		add(telemetry.AttrHarnessVersion, loaded.Version)
		if loaded.InstalledFrom != nil {
			add(telemetry.AttrHarnessCommit, loaded.InstalledFrom.SHA)
		}
	}
	add(telemetry.AttrRunFocus, focus)
	add(telemetry.AttrRunProfile, profile)
	if resumed {
		attrs = append(attrs, telemetry.AttrRunResumed.Bool(true))
	}
	return attrs
}
