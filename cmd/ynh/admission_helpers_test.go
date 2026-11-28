package main

import (
	"io"

	"github.com/eyelock/ynh/internal/harness"
)

// gateErr is the refusal decideTrust gives a run, nil when it may go ahead.
func gateErr(id string, p *harness.Harness, stderr io.Writer) error {
	return decideTrust(id, p, purposeRun, stderr).err()
}

// admit is the Admission decideTrust gives a run, for tests of what follows it.
func admit(id string, p *harness.Harness) Admission {
	return decideTrust(id, p, purposeRun, io.Discard)
}
