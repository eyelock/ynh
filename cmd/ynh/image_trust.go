package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
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
	if st.State == trustNoCommands {
		// The image's entrypoint is a one-shot run, which needs the harness
		// accepted even when it executes nothing (ynh #1058, N2): an image
		// built without the acceptance could not run its task, and nothing
		// inside the image can fix that.
		return nil, untrustedf("refusing to build an image of %s: you have not accepted it. It executes no commands, but the image runs one-shot, and a one-shot run opens at %s with nobody to approve (%s).\n"+
			"  Fix: review it with 'ynh trust show %s', accept it with 'ynh trust accept %s', then build the image again", id, p.AutoApproveOf(), p.AutoApproveOrigin(), id, id)
	}
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
func writeImageTrust(homeDir, name string, rec trustRecord, tree map[string]trustRecord) error {
	store := trustStore{Version: 1, Harnesses: map[string]trustRecord{}}
	for id, r := range tree {
		store.Harnesses[id] = r
	}
	store.Harnesses["local/"+name] = rec
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
func imageFromTrustRecord(p *harness.Harness, cfg *config.Config, ia imageArgs, isLocal bool, ref string) (*trustRecord, *imageTree, error) {
	id := "local/" + p.Name
	empty := trustStore{Version: 1, Harnesses: map[string]trustRecord{}}
	// --trust judges what the includes execute, so they are fetched before
	// the gate reads them, as `ynh install --trust` does (checkInstallTrust).
	// Without this a cold include cache reads as unavailable and --trust can
	// never get past the gate (ynw-twt3a).
	if ia.trust {
		// A floating native delegate entry is judged from the manifest alone,
		// before anything is fetched: an unpinned one is never fetched, so it
		// would otherwise read as unreadable rather than as unpinned.
		if err := refuseUnpinnedDelegates("this image", "--trust", p); err != nil {
			return nil, nil, err
		}
		if err := fetchIncludesForTrust(p, cfg); err != nil {
			return nil, nil, err
		}
	}
	st := trustStatusFor(id, p, empty, nil)
	if st.State == trustNoCommands {
		if ia.trust {
			return bakeNoCommandsRecord(p, st, ia)
		}
		// The image runs one-shot, which needs an acceptance baked in even
		// for a harness that executes nothing (ynh #1058, N2); without
		// --trust there is none to bake.
		var b strings.Builder
		fmt.Fprintf(&b, "refusing to build an image of %s from %s: you have not accepted it. It executes no commands, but the image runs one-shot, and a one-shot run opens at %s with nobody to approve (%s).\n", p.Name, ia.from, p.AutoApproveOf(), p.AutoApproveOrigin())
		if isLocal {
			fmt.Fprintf(&b, "  Fix: review it, then build with 'ynh image %s --from %s --trust'", ia.name, ia.from)
		} else {
			fmt.Fprintf(&b, "  Fix: review it, then build with 'ynh image %s --from %s --ref <40-character commit SHA> --trust'", ia.name, ia.from)
		}
		return nil, nil, &untrustedError{msg: b.String()}
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
				return nil, nil, &untrustedError{msg: msg}
			}
			return nil, nil, err
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
		return nil, nil, &untrustedError{msg: b.String()}
	}
	remote, err := remoteIncludes(p, "run")
	if err != nil {
		return nil, nil, err
	}
	if bad := unpinnedForAcceptance(p, remote); len(bad) > 0 {
		return nil, nil, refuseUnpinned("this image", "--trust", bad)
	}
	rec := trustRecord{
		Digest:     st.Digest,
		Commands:   harnessExecutables(p).count(),
		Scheme:     trustScheme,
		Via:        viaImage,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
	}
	tree, err := imageA2ATree(p, ia)
	if err != nil {
		return nil, nil, err
	}
	// Source, ref and commit are filled in by the caller for a remote source
	// (bakeFromProvenance), which also bakes the installed.json they match: a
	// record naming a source the image's harness does not carry would read
	// "moved" and the gate would refuse the image's own harness. The pin was
	// enforced above, at build.
	return &rec, tree, nil
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

// bakeNoCommandsRecord is the record --trust bakes for a harness that executes
// nothing: the level it opens at is part of what --trust accepted, and the
// image's one-shot run is refused without one (N2).
func bakeNoCommandsRecord(p *harness.Harness, st trustStatus, ia imageArgs) (*trustRecord, *imageTree, error) {
	tree, err := imageA2ATree(p, ia)
	if err != nil {
		return nil, nil, err
	}
	rec := trustRecord{Digest: st.Digest, Scheme: trustScheme, Via: viaImage, AcceptedAt: time.Now().UTC().Format(time.RFC3339)}
	return &rec, tree, nil
}

// imageTree is the a2a delegate tree an image built with --trust carries: each
// delegate's record, keyed by its install id, and the scratch home the tree was
// accepted and installed in. The host's own installs and trust.json are not
// touched by a build (ynw-tqeq5). A nil *imageTree is an empty tree.
type imageTree struct {
	records map[string]trustRecord
	home    string
	shared  bool // home is the host's own, which Close leaves alone
}

// Records returns the delegates' records to bake beside the parent's.
func (t *imageTree) Records() map[string]trustRecord {
	if t == nil {
		return nil
	}
	return t.records
}

// Close removes the scratch home.
func (t *imageTree) Close() {
	if t != nil && t.home != "" && !t.shared {
		_ = os.RemoveAll(t.home)
	}
}

// withYnhHome runs fn with YNH_HOME set to home, then puts it back.
func withYnhHome(home string, fn func() error) error {
	old, had := os.LookupEnv("YNH_HOME")
	if err := os.Setenv("YNH_HOME", home); err != nil {
		return err
	}
	defer func() {
		if had {
			_ = os.Setenv("YNH_HOME", old)
		} else {
			_ = os.Unsetenv("YNH_HOME")
		}
	}()
	return fn()
}

// imageA2ATree accepts the parent's whole a2a delegate tree for an image built
// with --trust (ynh #1058, N1), in a scratch home seeded with the host's
// config and include cache, so the host's installs and trust.json are left as
// they were: the tree is baked into the image, not accepted on the host. It is
// the one acceptance function, so it is all or nothing: if any delegate cannot
// be accepted nothing is baked and the error names the delegate. A dry run
// installs and records nothing: it only checks that every delegate could be
// accepted, and bakes no delegate records.
func imageA2ATree(p *harness.Harness, ia imageArgs) (*imageTree, error) {
	if len(a2aEntries(p)) == 0 {
		return nil, nil
	}
	if ia.dryRun {
		for _, n := range planA2ATree(p) {
			if n.Problem != "" {
				return nil, fmt.Errorf("nothing was accepted: delegate %s of %s cannot be accepted: %s", n.Label, n.Parent, n.Problem)
			}
		}
		return nil, nil
	}
	scratch, err := os.MkdirTemp("", "ynh-image-tree-*")
	if err != nil {
		return nil, err
	}
	tree := &imageTree{records: map[string]trustRecord{}, home: scratch}
	fail := func(err error) (*imageTree, error) { tree.Close(); return nil, err }
	// The cache the delegates are read from and the config's source allow-list.
	if err := copyIfExists(config.CacheDir(), filepath.Join(scratch, filepath.Base(config.CacheDir()))); err != nil {
		return fail(err)
	}
	if err := copyIfExists(config.ConfigPath(), filepath.Join(scratch, filepath.Base(config.ConfigPath()))); err != nil {
		return fail(err)
	}
	err = withYnhHome(scratch, func() error {
		done, err := acceptA2ATree(p, "local/"+p.Name, viaImage, nil, os.Stderr)
		if err != nil {
			return err
		}
		store, err := loadTrustStore()
		if err != nil {
			return err
		}
		for _, d := range done {
			tree.records[d.ID] = store.Harnesses[d.ID]
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	return tree, nil
}

// copyIfExists copies a file or folder, and does nothing when src is absent.
func copyIfExists(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return nil
	}
	if fi.IsDir() {
		return assembler.CopyTree(src, dst)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// bakeImageDelegates copies each accepted a2a delegate's install from the
// scratch home it was accepted in into the image's home, at the same id, with
// its includes' cache, so a delegate session inside the image resolves it by
// source and finds its record. A delegate installed from a local folder has no
// source the image could find again, so it refuses the build.
func bakeImageDelegates(homeDir string, tree *imageTree, cfg *config.Config) error {
	if tree == nil {
		return nil
	}
	ids := make([]string, 0, len(tree.records))
	for id := range tree.records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return withYnhHome(tree.home, func() error {
		for _, id := range ids {
			h, err := harness.LoadByID(id)
			if err != nil {
				return fmt.Errorf("baking delegate %s: %w", id, err)
			}
			if h.InstalledFrom == nil || h.InstalledFrom.SourceType == "local" {
				return fmt.Errorf("delegate %s is installed from a local folder, which an image cannot find again: pin it to a git source", id)
			}
			fs := namespace.IDToFSName(id)
			dst := filepath.Join(homeDir, "harnesses", fs)
			if err := assembler.CopyTree(h.Dir, dst); err != nil {
				return fmt.Errorf("baking delegate %s: %w", id, err)
			}
			if err := bakeIncludeCache(h, h.Includes, homeDir, path.Join(imageHome, "harnesses", fs)); err != nil {
				return fmt.Errorf("baking delegate %s: %w", id, err)
			}
		}
		return nil
	})
}

// installedImageTree is the a2a delegate tree an image of an installed parent
// carries (ynw-s8nke): the tree the host already accepted. Every delegate must
// be installed and accepted on the host, or the build is refused naming it;
// each one's record is taken from the host's trust.json as it stands, and its
// install and includes cache are baked from the host's own home, which is read
// and never changed.
func installedImageTree(id string, p *harness.Harness) (*imageTree, error) {
	if len(a2aEntries(p)) == 0 {
		return nil, nil
	}
	store, err := loadTrustStore()
	if err != nil {
		return nil, untrustedf("refusing to build an image of %s: the trust record could not be read: %v", id, err)
	}
	tree := &imageTree{records: map[string]trustRecord{}, home: config.HomeDir(), shared: true}
	for _, n := range planA2ATree(p) {
		if n.Problem != "" {
			return nil, untrustedf("refusing to build an image of %s: delegate %s of %s cannot be read: %s", id, n.Label, n.Parent, n.Problem)
		}
		if !n.Installed {
			return nil, untrustedf("refusing to build an image of %s: delegate %s of %s is not installed and accepted on this machine.\n"+
				"  Fix: 'ynh trust accept %s' installs and accepts the whole tree, then build the image again", id, n.Label, n.Parent, id)
		}
		if st := trustStatusFor(n.ID, n.H, store, nil); st.State != trustAccepted {
			return nil, untrustedf("refusing to build an image of %s: delegate %s of %s (%s) is %s, not accepted.\n"+
				"  Fix: review it with 'ynh trust show %s', accept it with 'ynh trust accept %s', then build the image again", id, n.Label, n.Parent, n.ID, st.State, id, id)
		}
		tree.records[n.ID] = store.Harnesses[n.ID]
	}
	return tree, nil
}
