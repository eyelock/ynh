package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/harness"
)

// viaImage is the `via` of a trust record `ynh image` writes (design decision
// 16): the operator accepted the harness on the host, and the image carries
// that acceptance to where the harness runs.
const viaImage = "image"

// imageTrustRecord is the gate `ynh image` applies to an installed harness
// (design decision 9). It is the run-time gate itself, so what refuses a run
// refuses a build: a harness whose current commands do not match an accepted
// record, includes that cannot be read, or a script the digest could not read
// in full. The refusal is the same *untrustedError, naming what to review and
// the command that accepts it.
//
// It returns the record to bake, under the digest the host just computed, or
// nil when the harness executes nothing: the image's gate lets such a harness
// through without a record, and a record for it would claim an acceptance
// nobody gave.
func imageTrustRecord(id string, p *harness.Harness) (*trustRecord, error) {
	store, err := loadTrustStore()
	if err != nil {
		return nil, untrustedf("refusing to build an image of %s: the trust record could not be read: %v", id, err)
	}
	if err := installedGate(id, p, store); err != nil {
		// The gate's words, about a build: its text is "refusing to run <id>".
		msg := strings.Replace(err.Error(), "refusing to run "+id, "refusing to build an image of "+id, 1)
		if isUntrusted(err) {
			return nil, &untrustedError{msg: msg}
		}
		return nil, err
	}
	st := trustStatusFor(id, p, store, nil)
	if st.State != trustAccepted {
		return nil, nil
	}
	rec := trustRecord{
		Digest:     st.Digest,
		Commands:   store.Harnesses[id].Commands,
		Scheme:     trustScheme,
		Via:        viaImage,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if pr := p.InstalledFrom; pr != nil {
		rec.Source, rec.Ref, rec.SHA = pr.Source, pr.Ref, pr.SHA
	}
	return &rec, nil
}

// writeImageTrust writes the record into the image's home, keyed
// local/<name>: the id the image's entrypoint runs the harness under, whatever
// id it was installed under on the host (the image bakes every harness at
// local--<name>).
func writeImageTrust(homeDir, name string, rec trustRecord) error {
	store := trustStore{Version: 1, Harnesses: map[string]trustRecord{"local/" + name: rec}}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the image's trust record: %w", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, trustFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing the image's trust record: %w", err)
	}
	return nil
}
