package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
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

// imageFromTrustRecord is the gate `ynh image --from` applies to a harness
// that is not installed (ynh #986). Nothing has accepted it, so a harness
// with something to accept (hooks, MCP servers, sensors, workspace folders)
// is refused at build time unless --trust says the operator accepts it now.
//
// --trust follows the full-SHA rule of `ynh install --trust`: a remote source
// and every remote include that brings commands must be pinned to a full
// commit SHA, or the build is refused, naming what floats. A local folder is
// the operator's own files and needs no pin.
//
// It returns the record to bake (via: image), or nil when the harness has
// nothing to accept, which builds as before.
func imageFromTrustRecord(p *harness.Harness, cfg *config.Config, ia imageArgs, isLocal bool, ref string) (*trustRecord, error) {
	id := "local/" + p.Name
	empty := trustStore{Version: 1, Harnesses: map[string]trustRecord{}}
	// --trust judges what the includes execute, so they are fetched before
	// the gate reads them, as `ynh install --trust` does (checkInstallTrust).
	// Without this a cold include cache reads as unavailable and --trust can
	// never get past the gate (ynw-twt3a).
	if ia.trust {
		if err := fetchIncludesForTrust(p, cfg); err != nil {
			return nil, err
		}
	}
	st := trustStatusFor(id, p, empty, nil)
	if st.State == trustNoCommands {
		return nil, nil
	}
	if err := installedGate(id, p, empty); err != nil {
		// Unreadable commands or scripts refuse a build whatever --trust says;
		// only an unreviewed harness is the operator's to accept.
		if st.State != trustUnreviewed || len(scriptBlockers(st.Executables, st.Hooks, st.MCPServers)) > 0 {
			msg := strings.Replace(err.Error(), "refusing to run "+id, "refusing to build an image of "+id, 1)
			if st.State == trustUnavailable && !ia.trust {
				// The gate's own hint, 'ynh update local/<name>', names an id
				// that is not installed: no route exists for a --from source
				// except --trust, which fetches the includes itself.
				msg = strings.Replace(msg, "Run 'ynh update "+id+"' to fetch its includes, then review it again.",
					fromUnavailableFix(ia, isLocal), 1)
				// The resolver's own 'run `ynh update`' line is no route either.
				var kept []string
				for _, l := range strings.Split(msg, "\n") {
					if !strings.Contains(l, "run `ynh update`") {
						kept = append(kept, l)
					}
				}
				msg = strings.Join(kept, "\n")
			}
			if isUntrusted(err) {
				return nil, &untrustedError{msg: msg}
			}
			return nil, err
		}
	}
	if !ia.trust {
		var b strings.Builder
		fmt.Fprintf(&b, "refusing to build an image of %s from %s: you have not accepted what it executes.\n", p.Name, ia.from)
		b.WriteString(listExecutables(st.Executables, st.Hooks, st.MCPServers))
		b.WriteString(listWorkspace(p))
		if isLocal {
			fmt.Fprintf(&b, "  Fix: review it, then build with 'ynh image %s --from %s --trust'", ia.name, ia.from)
		} else {
			fmt.Fprintf(&b, "  Fix: review it, then build with 'ynh image %s --from %s --ref <40-character commit SHA> --trust'", ia.name, ia.from)
		}
		return nil, &untrustedError{msg: b.String()}
	}
	remote, err := remoteIncludes(p, "run")
	if err != nil {
		return nil, err
	}
	if bad := unpinnedIncludes(remote); len(bad) > 0 {
		return nil, refuseUnpinned("this image", "--trust", bad)
	}
	rec := trustRecord{
		Digest:     st.Digest,
		Commands:   harnessExecutables(p).count(),
		Scheme:     trustScheme,
		Via:        viaImage,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	// The image's harness/ folder is a clone with no installed.json, so inside
	// the image the harness has no recorded source or commit. The record is
	// keyed by what the image will see, so it carries none either: a record
	// with a source would read "moved" against that clone and the gate would
	// refuse the image's own harness. The pin was enforced above, at build.
	return &rec, nil
}

// fetchIncludesForTrust fetches every include of p, those a profile adds
// included, into the include cache, so the trust gate can read what they
// execute. A fetch that fails refuses --trust, naming the harness.
func fetchIncludesForTrust(p *harness.Harness, cfg *config.Config) error {
	if _, err := resolver.Resolve(p, cfg); err != nil {
		return fmt.Errorf("--trust could not read the includes of %s: %w", p.Name, err)
	}
	profileIncs, err := profileOnlyIncludes(p)
	if err != nil {
		return err
	}
	if len(profileIncs) > 0 {
		if _, err := resolver.Resolve(&harness.Harness{Dir: p.Dir, Includes: profileIncs}, cfg); err != nil {
			return fmt.Errorf("--trust could not read the profile includes of %s: %w", p.Name, err)
		}
	}
	return nil
}

// fromUnavailableFix is the Fix line for a --from build whose includes are not
// in the cache: nothing is installed to update, so it names --trust, which
// fetches them, at a full SHA for a remote source.
func fromUnavailableFix(ia imageArgs, isLocal bool) string {
	if isLocal {
		return fmt.Sprintf("Fix: review it, then build with 'ynh image %s --from %s --trust', which fetches its includes (each remote include must be pinned to a 40-character commit SHA).", ia.name, ia.from)
	}
	return fmt.Sprintf("Fix: review it, then build with 'ynh image %s --from %s --ref <40-character commit SHA> --trust', which fetches its includes (each remote include must be pinned to a 40-character commit SHA).", ia.name, ia.from)
}
