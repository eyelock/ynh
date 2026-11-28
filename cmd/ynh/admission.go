package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/harness"
)

// trustPurpose is what an Admission is asked for. It changes only the words
// of a refusal ("refusing to run", "refusing to build an image of"), never
// the decision: every purpose gets the same answer for the same harness and
// the same trust record (ynh #1006).
type trustPurpose string

const (
	purposeRun       trustPurpose = "run"
	purposeCheck     trustPurpose = "check"
	purposeSensors   trustPurpose = "sensors"
	purposeResume    trustPurpose = "resume"
	purposeDelegate  trustPurpose = "delegate"
	purposeImage     trustPurpose = "image"
	purposeImageFrom trustPurpose = "image-from"
)

// Admission is the one answer to "may this harness launch what it declares,
// and what does that unlock?". Only decideTrust and decideTrustIn make one:
// the unexported minted field is the token, so a consumer holding the zero
// Admission knows nobody decided and refuses (see Admission.err).
type Admission struct {
	minted bool

	// ID is the installed id, "" for a folder run.
	ID string
	// Harness is the harness as loaded, before a focus or profile reshapes it:
	// the form a trust record covers.
	Harness *harness.Harness
	// Status is the harness's trust state against the record. Zero for a
	// folder run, whose own commands are not checked.
	Status trustStatus
	// Trusted is true for an installed harness whose current commands match
	// an accepted record, or that has none. A folder run is never trusted:
	// the gate does not check its own commands (ynh #952).
	Trusted bool
	// Refusal is nil when the launch may go ahead, else an *untrustedError
	// (or the error that kept the gate from deciding).
	Refusal error
	// Record is the trust record the harness's id matched, nil when none did.
	Record *trustRecord
}

// err is the Admission's refusal, and a refusal too when nobody decided:
// a zero Admission is never an admission.
func (a Admission) err() error {
	if !a.minted {
		return fmt.Errorf("internal error: a launch path skipped the trust decision (decideTrust)")
	}
	return a.Refusal
}

// decideTrust is the only place that decides whether a harness may launch
// what it declares. id is the install id, "" for a folder run; p is the
// harness as loaded. It reads the operator's trust record.
func decideTrust(id string, p *harness.Harness, purpose trustPurpose, stderr io.Writer) Admission {
	store, err := loadTrustStore()
	if err != nil {
		name := id
		if p != nil {
			name = gateName(id, p)
		}
		return Admission{minted: true, ID: id, Harness: p, Refusal: untrustedf("refusing to %s: the trust record could not be read: %v", purposeVerb(purpose, name), err)}
	}
	return decideTrustIn(store, id, p, purpose, stderr)
}

// decideTrustIn is decideTrust against a trust store the caller already has,
// or builds (an image built --from has none: nothing is installed to look up).
func decideTrustIn(store trustStore, id string, p *harness.Harness, purpose trustPurpose, stderr io.Writer) Admission {
	adm := Admission{minted: true, ID: id, Harness: p}
	if p == nil {
		return adm
	}
	if id == "" {
		adm.Refusal = folderGate(p, store, stderr)
		return adm
	}
	adm.Status = trustStatusFor(id, p, store, nil)
	if rec, ok := store.Harnesses[id]; ok {
		adm.Record = &rec
	}
	if err := installedGate(id, p, store); err != nil {
		adm.Refusal = rewordRefusal(err, purpose, id)
		return adm
	}
	adm.Trusted = true
	return adm
}

// withOneShot is the Admission of a one-shot launch (`ynh run <id> -- "<task>"`,
// a delegate session, `ynh agent run`): the level it opens at is part of what
// acceptance covers, so an installed harness that executes nothing, which an
// interactive run lets through unaccepted, is refused until accepted (ynh
// #1058, N2). One that executes something is already decided. A folder run has
// no record to check and is left to the folder route.
func (a Admission) withOneShot() Admission {
	if !a.minted || a.Refusal != nil || a.ID == "" || a.Harness == nil || a.Status.State != trustNoCommands {
		return a
	}
	a.Trusted = false
	a.Refusal = untrustedf("refusing to run %s one-shot: you have not accepted it. It executes no commands, but a one-shot run opens at %s with nobody to approve (%s).\n"+
		"Review it: ynh trust show %s\nThen accept:  ynh trust accept %s", a.ID, a.Harness.AutoApproveOf(), a.Harness.AutoApproveOrigin(), a.ID, a.ID)
	return a
}

// purposeVerb is what a refusal says the launch was: "run <name>" or "build an
// image of <name>".
func purposeVerb(purpose trustPurpose, name string) string {
	if purpose == purposeImage || purpose == purposeImageFrom {
		return "build an image of " + name
	}
	return "run " + name
}

// rewordRefusal puts a refusal in the words of the purpose. The gate words
// everything as a run; an image build says so.
func rewordRefusal(err error, purpose trustPurpose, id string) error {
	if purpose != purposeImage && purpose != purposeImageFrom {
		return err
	}
	msg := strings.Replace(err.Error(), "refusing to run "+id, "refusing to build an image of "+id, 1)
	if isUntrusted(err) {
		return &untrustedError{msg: msg}
	}
	return err
}

// grants is everything an acceptance unlocks for one launch.
type grants struct {
	// MCPApprove: the tools of the harness's own MCP servers are approved on a
	// one-shot run (Claude Code's allow rule, Codex's approval mode, and
	// Copilot's one-shot approval of every tool, which covers them).
	MCPApprove bool
	// CopilotOneShot: a one-shot Copilot run may approve every tool.
	CopilotOneShot bool
	// AutoApprove: `ynh agent run --auto-approve` is accepted.
	AutoApprove bool
	// Workspace: the harness's requested workspace folders are granted.
	Workspace bool
	// CodexHooks: Codex trusts the run directory and the hooks written there.
	CodexHooks bool
	// CursorTrust: cursor-agent is passed --trust.
	CursorTrust bool
}

// grantsFor is the one table of what an acceptance unlocks, for the vendor
// the launch runs on. Every pre-approval in ynh reads it: no other site tests
// trust itself. Nothing is granted without an Admission that is trusted (an
// installed harness accepted and current, or with nothing to accept), and a
// one-shot run's MCP approval is never given to a harness that sets
// mcp_manual_approval. A grant a vendor has no mechanism for is false.
func grantsFor(adm Admission, vendorName string, oneShot, manualApproval bool) grants {
	if !adm.minted || !adm.Trusted || adm.Refusal != nil {
		return grants{}
	}
	return grants{
		MCPApprove:     oneShot && !manualApproval && (vendorName == "claude" || vendorName == "codex" || vendorName == "copilot"),
		CopilotOneShot: oneShot && vendorName == "copilot",
		AutoApprove:    true,
		Workspace:      true,
		CodexHooks:     vendorName == "codex",
		CursorTrust:    vendorName == "cursor",
	}
}

// grantRows names each grant as docs/trust.md's per-vendor table does, in its
// order; a test holds the table to grantsFor.
var grantRows = []struct {
	Label string
	Get   func(grants) bool
}{
	{"Own MCP tools on a one-shot run", func(g grants) bool { return g.MCPApprove }},
	{"One-shot run approves every tool", func(g grants) bool { return g.CopilotOneShot }},
	{"`ynh agent run --auto-approve`", func(g grants) bool { return g.AutoApprove }},
	{"Workspace folders", func(g grants) bool { return g.Workspace }},
	{"Codex hooks and project trust", func(g grants) bool { return g.CodexHooks }},
	{"Cursor `--trust`", func(g grants) bool { return g.CursorTrust }},
}

// agentAdmission is what `ynh agent run` hands the loop: the refusal, or the
// grants of a one-shot run on the backend the loop resolved. It does not read
// mcp_manual_approval: the loop calls it before it resolves --profile and the
// focus's profile, and a profile can turn the setting on or off, so the loop
// reads the resolved setting and the worker withholds the MCP grant itself.
func agentAdmission(adm Admission, backend string) agent.Admission {
	if err := adm.err(); err != nil {
		return agent.Admission{Refusal: err}
	}
	g := grantsFor(adm, backend, true, false)
	return agent.Admission{MCPApprove: g.MCPApprove, AutoApprove: g.AutoApprove}
}
