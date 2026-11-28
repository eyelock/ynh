package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// A sensor's `command` and `version_command` are handed to `/bin/sh -c` with
// the operator's whole environment. That is the feature: a sensor exists to run
// the project's own tools. But a harness is installed from a Git URL into
// ~/.ynh/harnesses/, and its sensors then run against a tree somewhere else, so
// the code that executes lives nowhere near the directory the operator is
// looking at. Running `make check` on a repository at least puts the Makefile
// in front of you.
//
// `ynh trust` closes that gap by showing what a harness will execute, where it
// came from, and at which commit, and by recording that an operator saw it.
//
// It does not block. A recorded decision nobody acts on would be the same
// hollow control this repository spent 2026-09-01 removing elsewhere, so the
// record has to pay for itself immediately: it pins the exact set of commands
// that were reviewed, and `ynh check` reports when a harness's commands have
// changed since. Detecting the change is the value now; refusing to run on it
// is a later decision, and deliberately not this one.

// trustFile is the operator's record, under YNH_HOME.
//
// Operator state, not project state, and pointedly not inside the harness
// directory: a harness carrying its own trust record would be authorising
// itself. That is the same trap SkillSpector's --use-shipped-baseline exists to
// avoid, where a scanned artifact supplies the baseline that clears it.
const trustFile = "trust.json"

// trustRecord is one accepted harness.
type trustRecord struct {
	// Digest covers every string this harness can get executed: sensor commands,
	// hook commands and MCP servers, at the top level and in every profile. It is the
	// whole point of the record: comparing it is how a changed command is
	// noticed.
	Digest string `json:"digest"`
	// Commands is how many executable strings the digest covered, so the
	// report can say "12 commands" without re-reading the manifest.
	Commands int `json:"commands"`
	// Scheme is which digest scheme Digest was computed under. Absent (0) is
	// the original scheme, which covered sensor commands only; a record in it
	// is reported as changed with an explanation, never silently accepted.
	Scheme int `json:"scheme,omitempty"`
	// Via is how the acceptance was recorded: "accept", "install --trust",
	// "run --trust", "update --trust", "include --trust" or "image". Absent on a record written before it existed.
	// It does not change the digest.
	Via string `json:"via,omitempty"`
	// Source, Ref and SHA are copied from provenance at acceptance time.
	// Copied rather than referenced: the point is what the operator was
	// looking at when they accepted, which an update would otherwise rewrite.
	Source     string `json:"source,omitempty"`
	Ref        string `json:"ref,omitempty"`
	SHA        string `json:"sha,omitempty"`
	AcceptedAt string `json:"accepted_at"`
	// Floating lists what `ynh trust accept` accepted although it had no full
	// SHA ref (a remote include or native delegate entry), each with the
	// commit it was at then (ynh #1002). Informational: the digest decides
	// whether what runs changed.
	Floating []floatingPin `json:"floating,omitempty"`
}

type trustStore struct {
	Version   int                    `json:"version"`
	Harnesses map[string]trustRecord `json:"harnesses"`
}

func trustStorePath() string { return filepath.Join(config.HomeDir(), trustFile) }

// loadTrustStore reads the record, returning an empty store when absent.
//
// A missing file is "nothing has been accepted", not an error: every install
// predating this command is in exactly that state, and it must not be an error
// condition for them.
func loadTrustStore() (trustStore, error) {
	s := trustStore{Version: 1, Harnesses: map[string]trustRecord{}}
	data, err := os.ReadFile(trustStorePath())
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("reading trust record: %w", err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parsing %s: %w", trustStorePath(), err)
	}
	if s.Harnesses == nil {
		s.Harnesses = map[string]trustRecord{}
	}
	return s, nil
}

func saveTrustStore(s trustStore) error {
	s.Version = 1
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding trust record: %w", err)
	}
	if err := os.MkdirAll(config.HomeDir(), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", config.HomeDir(), err)
	}
	// 0600: it records what this operator reviewed. Nothing secret, but it is
	// theirs, and a world-writable file inviting edits would defeat it.
	if err := os.WriteFile(trustStorePath(), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing trust record: %w", err)
	}
	return nil
}

// trustScheme is the digest scheme. 1 (recorded as absent) covered sensor
// commands only; 2 adds hook commands and MCP servers; 3 adds the contents of
// the scripts those commands run (see scriptRef). A digest from an older
// scheme can never equal a current one, because the current input is prefixed
// with the scheme, so an old acceptance fails closed as "changed".
const trustScheme = 3

// executable is one sensor string a harness can get run through a shell.
type executable struct {
	// Profile is set when only that profile contributes the sensor, through a
	// profile-level include.
	Profile string `json:"profile,omitempty"`
	Sensor  string `json:"sensor"`
	Field   string `json:"field"` // "command" or "version_command"
	Value   string `json:"value"`
	// Source and HarnessDir are set for a sensor an include contributes: where
	// it came from, and the directory $YNH_HARNESS_DIR resolves to when it
	// runs, which is the publishing harness and not this one.
	Source     string `json:"source,omitempty"`
	HarnessDir string `json:"harness_dir,omitempty"`
	// Script is the script the command runs, when it names one.
	Script *scriptRef `json:"script,omitempty"`
}

// hookExec is one hook command. Profile is empty for the harness's own hooks,
// and names the profile for a hook that only that profile adds or changes.
type hookExec struct {
	Profile string `json:"profile,omitempty"`
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
	// Source names the included harness that supplied the hook, which has to
	// have consented to it with "hooks": true. Empty for the harness's own.
	Source string `json:"source,omitempty"`
	// Script is the script the command runs, when it names one.
	Script *scriptRef `json:"script,omitempty"`
}

// mcpValue is an env or header entry of an MCP server. The raw value feeds the
// digest and is never serialised; Value is what a person may be shown.
type mcpValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	raw   string
}

// mcpExec is one MCP server as the vendor CLI would launch or contact it.
type mcpExec struct {
	Profile string `json:"profile,omitempty"`
	Name    string `json:"name"`
	// Source names the included harness that supplied the server. Empty for
	// the harness's own.
	Source string `json:"source,omitempty"`
	// Delegate names the delegate harness that carries the server, which is
	// connected only while that delegate runs. Empty for the session's own.
	Delegate  string     `json:"delegate,omitempty"`
	Transport string     `json:"transport"`
	Command   string     `json:"command,omitempty"`
	Args      []string   `json:"args,omitempty"`
	URL       string     `json:"url,omitempty"`
	Cwd       string     `json:"cwd,omitempty"`
	Env       []mcpValue `json:"env,omitempty"`
	Headers   []mcpValue `json:"headers,omitempty"`
	// Script is the script a stdio server's command runs, when it names one.
	Script *scriptRef `json:"script,omitempty"`
}

// trustSet is everything the operator is asked to review, grouped by kind.
type trustSet struct {
	Sensors    []executable
	Hooks      []hookExec
	MCPServers []mcpExec
	// Workspace is the folders the harness requests, as written, sorted. They
	// are not executed, but they widen what the vendor CLI can read and write,
	// which is the same question the operator is asked to answer, so the
	// digest covers them. Only the harness's own list: a profile does not
	// change it.
	Workspace []string
	// LaunchFocus is the focus every bare launch wakes with, or nil. Its
	// prompt is the first message of every such session, so it steers the
	// model as much as a command does, and the digest covers it.
	LaunchFocus *launchFocusReport
	// DelegateFocuses are the focuses delegates_to entries select, in entry
	// order. Each prompt is a delegate's standing instructions, so it steers
	// that subagent as launch_focus steers a session, and the digest covers
	// it. delegates_to is not changed by a profile, so they are read once.
	DelegateFocuses []delegateFocusReport
	// DelegateSources are the delegates_to entries the parent may start or
	// run, of either transport (git URL, path, ref or local folder, selection,
	// and the settled transport), sorted. Repointing an entry at another
	// harness that happens to carry the same name and commands, or flipping it
	// between a2a and native, must still change the digest, so the entry itself
	// is covered, not only what it resolves to.
	DelegateSources []string
	// AutoApprove is the level the harness declares for its unattended runs,
	// "" when undeclared (ynh #1058, D8). Only a declared level is digested,
	// so no existing acceptance changes; AutoApproveOf is what is shown.
	AutoApprove string
	// DelegateLevels are the levels delegates_to entries declare, one line
	// each ("<label>: <level>"), in entry order, for the listing. The digest
	// covers them through DelegateSources.
	DelegateLevels []string
	// Problems is why the set could not be read completely: an include that
	// is not cached, a merge that refuses. A set with problems is partial, and
	// a partial set must never be digested or reported as the whole.
	Problems []string
}

func (t trustSet) count() int { return len(t.Sensors) + len(t.Hooks) + len(t.MCPServers) }

// reviewable counts everything there is to review: the executable strings
// and the workspace folders. count() stays executables only, because the
// COMMANDS column and a record's Commands mean commands.
func (t trustSet) reviewable() int {
	n := t.count() + len(t.Workspace) + len(t.DelegateFocuses)
	if t.LaunchFocus != nil {
		n++
	}
	return n
}

// delegateFocusReport is a focus a delegates_to entry selects, as the
// operator reviews it: the delegate, the focus, its profile, and the prompt
// that becomes the delegate's standing instructions.
type delegateFocusReport struct {
	Delegate string `json:"delegate"`
	Name     string `json:"name"`
	Profile  string `json:"profile,omitempty"`
	Prompt   string `json:"prompt"`
}

// launchFocusReport is a harness's launch_focus as the operator reviews it.
type launchFocusReport struct {
	Name    string `json:"name"`
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt"`
}

// launchFocusFor returns the harness's launch focus, or nil when it declares
// none or names one that does not exist (`ynd validate` reports that).
func launchFocusFor(p *harness.Harness) *launchFocusReport {
	if p.LaunchFocus == "" {
		return nil
	}
	f, ok := p.Focuses[p.LaunchFocus]
	if !ok {
		return &launchFocusReport{Name: p.LaunchFocus}
	}
	return &launchFocusReport{Name: p.LaunchFocus, Profile: f.Profile, Prompt: f.Prompt}
}

var envRefRe = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)

// shownValue is what may be printed for an env or header value. A whole-value
// ${VAR} reference is a pointer to a credential, not the credential, and shows
// as written. Anything else may be the secret itself, so it is not shown at
// all; the digest still covers it, so a change to it is noticed.
func shownValue(v string) string {
	if envRefRe.MatchString(v) {
		return v
	}
	return "(literal, not shown)"
}

func mcpValues(m map[string]string) []mcpValue {
	var out []mcpValue
	for _, k := range slices.Sorted(maps.Keys(m)) {
		out = append(out, mcpValue{Name: k, Value: shownValue(m[k]), raw: m[k]})
	}
	return out
}

// runView is one resolved view of a harness: the harness itself (Profile
// empty) or a profile as `ynh run --profile` would resolve it.
type runView struct {
	Profile string
	H       *harness.Harness
	// MCPSources maps each MCP server of H to the harness that supplied it
	// (harness.MCPSourceRoot, or the include's display name).
	MCPSources map[string]string
	// Delegates holds the MCP servers each delegate harness carries on its
	// own, which are not the session's: they are connected only while that
	// delegate runs.
	Delegates []delegateView
	// DelegateEntries is every delegates_to entry the parent may start or run,
	// of either transport, as written plus its settled transport: what the
	// digest covers of an entry whatever it carries (see entryKey).
	DelegateEntries []string
	// HookSources maps each event of H.Hooks to the include chain that
	// supplied each entry, "" for the harness's own.
	HookSources map[string][]string
	// HookDirs runs parallel to HookSources: the directory of the harness that
	// declared each hook, "" for the harness's own. HookScripts maps a rewritten
	// "./" script of an included harness to where it comes from. MCPDirs maps
	// each MCP server of H to the directory of the harness that declared it.
	HookDirs    map[string][]string
	HookScripts map[string]assembler.ScriptOrigin
	MCPDirs     map[string]string
}

// runViews is the single place that says which resolved views of a harness
// can execute, and it resolves them the way a run does: the profile merge,
// then the include merge, lookup-only. Sensors from includes arrive in
// H.Sensors (resolver.WithIncludedSensors); MCP servers, from every harness
// the includes reach, in H.MCPServers (harness.ComposeMCPServersRaw); and the
// hooks of included harnesses whose includes consented to them
// ("hooks": true) in H.Hooks (assembler.ComposeHooks), each with its source.
// collectSensors, collectHooks, collectMCP, the digest, the listing and
// `trust show` follow from H.
//
// Anything that cannot be resolved is returned as a problem rather than
// skipped, so the caller can refuse to call a partial set complete.
// delegateView is one delegate's own MCP servers, with their sources.
type delegateView struct {
	Name string
	// Source is the entry's source, as written (see delegateSource).
	Source  string
	Servers map[string]plugin.MCPServer
	Sources map[string]string
	// Dirs maps each server to the directory of the harness that declared it.
	Dirs map[string]string
	// Focus is the focus the delegates_to entry selects, nil for none. Its
	// prompt is the delegate's standing instructions.
	Focus *harness.DelegateFocus
}

// runSources is where the servers and hooks of a view came from.
type runSources struct {
	mcp       map[string]string
	mcpDirs   map[string]string
	hooks     map[string][]string
	hookDirs  map[string][]string
	scripts   map[string]assembler.ScriptOrigin
	delegates []delegateView
	entries   []string
}

func runViews(p *harness.Harness) (views []runView, problems []string) {
	// fold returns a copy of h carrying what the includes bring under sel: the
	// MCP servers of every harness they reach, composed as a run composes them,
	// with their sources. h itself is not changed.
	fold := func(h *harness.Harness, sel harness.Selection) (*harness.Harness, runSources, []resolver.ResolveResult, error) {
		src := runSources{mcp: map[string]string{}, mcpDirs: map[string]string{}}
		delegates, entries, err := delegateViews(h)
		if err != nil {
			return nil, src, nil, err
		}
		src.delegates = delegates
		src.entries = entries
		if len(h.Includes) == 0 {
			return h, src, nil, nil
		}
		resolved, err := resolver.ResolveIncludesLocal(h, sel)
		if err != nil {
			return nil, src, nil, fmt.Errorf("includes: %w", err)
		}
		servers, provenance, err := harness.ComposeMCPServersRaw(h, resolver.IncludedHarnesses(resolved))
		if err != nil {
			return nil, src, nil, err
		}
		// Hooks of an included harness run only with its include's consent;
		// ComposeHooks keeps the ones that have it.
		hs, err := assembler.ComposeHooks(h.Hooks, resolved)
		if err != nil {
			return nil, src, nil, err
		}
		c := *h
		c.MCPServers = servers
		c.Hooks = hs.Hooks
		src.hooks = hs.Sources
		src.hookDirs = hs.Dirs
		src.scripts = hs.Scripts
		for _, pr := range provenance {
			src.mcp[pr.Server] = pr.Source
			src.mcpDirs[pr.Server] = pr.Dir
		}
		return &c, src, resolved, nil
	}
	add := func(profile string, h *harness.Harness) {
		label := "the harness"
		if profile != "" {
			label = "profile " + profile
		}
		// A copy: the include merge assigns Sensors, and a view must not
		// change the harness it was made from.
		c := *h
		withSensors, err := resolver.WithIncludedSensors(&c)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", label, err))
			return
		}
		merged, src, resolved, err := fold(withSensors, harness.Selection{})
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", label, err))
			return
		}
		views = append(views, runView{Profile: profile, H: merged, MCPSources: src.mcp, MCPDirs: src.mcpDirs, HookSources: src.hooks, HookDirs: src.hookDirs, HookScripts: src.scripts, Delegates: src.delegates, DelegateEntries: src.entries})
		if profile != "" {
			return
		}

		// `--profile ns:name` applies a profile to an included harness alone,
		// and that profile can add servers the harness does not otherwise
		// carry. Each one is a view, listed as what it adds to the harness's.
		seen := map[string]bool{}
		for _, r := range resolved {
			if r.Harness == nil || r.Namespace == "" {
				continue
			}
			for _, name := range slices.Sorted(maps.Keys(r.Harness.Profiles)) {
				qualified := r.Namespace + ":" + name
				if seen[qualified] {
					continue
				}
				seen[qualified] = true
				v, vSrc, _, err := fold(withSensors, harness.Selection{Included: map[string]string{r.Namespace: name}})
				var ambiguous *resolver.AmbiguousNamespaceError
				if errors.As(err, &ambiguous) {
					// Two harnesses answer to the namespace, so a run cannot
					// select it either.
					continue
				}
				if err != nil {
					problems = append(problems, fmt.Sprintf("profile %s: %v", qualified, err))
					continue
				}
				views = append(views, runView{Profile: qualified, H: v, MCPSources: vSrc.mcp, MCPDirs: vSrc.mcpDirs, HookSources: vSrc.hooks, HookDirs: vSrc.hookDirs, HookScripts: vSrc.scripts, Delegates: vSrc.delegates, DelegateEntries: vSrc.entries})
			}
		}
	}
	add("", p)
	for _, name := range slices.Sorted(maps.Keys(p.Profiles)) {
		r, err := harness.ResolveProfile(p, name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("profile %s: %v", name, err))
			continue
		}
		add(name, r)
	}
	return views, problems
}

// delegateViews reads the MCP servers each native delegate of h carries on its
// own, the way a run composes them: the delegate's includes (resolved against
// its own directory, from the cache), then its own servers. A native
// delegate's own delegates are among them, as a run flattens them.
//
// The digest covers what runs inside the parent's process. A native delegate
// is a subagent of the parent's session, so its servers and focus prompt are
// reviewed here. An a2a delegate (the default) is its own ynh session under
// its own trust grant, so its servers and focus prompt are reviewed there,
// and it is neither resolved nor walked. Every entry, of either transport,
// still counts: it is returned among the entries, as written with its
// settled transport, because repointing it, or flipping it between a2a and
// native, changes what the parent may start or run.
//
// Anything unreadable is an error, so the set is never reported complete
// without it.
func delegateViews(h *harness.Harness) ([]delegateView, []string, error) {
	flat, skipped, err := resolver.FlattenDelegatesBy(h.DelegatesTo, h.Dir, h.Name, h.Transport, true, resolver.ResolveDelegateLocal)
	if err != nil {
		return nil, nil, err
	}
	var entries []string
	for _, fd := range flat {
		entries = append(entries, entryKey(fd.Entry))
	}
	for _, e := range skipped {
		entries = append(entries, entryKey(e))
	}
	slices.Sort(entries)
	var out []delegateView
	for _, fd := range flat {
		dh, focus := fd.Harness, fd.Focus
		var included []harness.IncludedHarness
		if len(dh.Includes) > 0 {
			resolved, err := resolver.ResolveIncludesLocal(dh, harness.Selection{})
			if err != nil {
				return nil, nil, fmt.Errorf("delegate %s: includes: %w", dh.Name, err)
			}
			included = resolver.IncludedHarnesses(resolved)
		}
		servers, provenance, err := harness.ComposeMCPServersRaw(dh, included)
		if err != nil {
			return nil, nil, fmt.Errorf("delegate %s: %w", dh.Name, err)
		}
		dv := delegateView{Name: dh.Name, Source: entrySourceKey(fd.Entry), Servers: servers, Sources: map[string]string{}, Dirs: map[string]string{}, Focus: focus}
		for _, pr := range provenance {
			dv.Sources[pr.Server] = pr.Source
			dv.Dirs[pr.Server] = pr.Dir
		}
		out = append(out, dv)
	}
	return out, entries, nil
}

// entrySourceKey is a delegates_to entry's source and selection as written,
// for the digest: where it points, at which ref, and which profile or focus.
func entrySourceKey(e harness.Delegate) string {
	return strings.Join([]string{e.Git, e.Local, e.Path, e.Ref, e.Profile, e.Focus}, "\x00")
}

// entryKey is entrySourceKey with the entry's settled transport, and its
// auto_approve only when declared so an entry without one keeps its digest.
func entryKey(e harness.Delegate) string {
	k := entrySourceKey(e) + "\x00" + e.Transport
	if e.AutoApprove != "" {
		k += "\x00auto_approve=" + e.AutoApprove
	}
	return k
}

func sensorKey(e executable) string {
	return e.Sensor + "\x00" + e.Field + "\x00" + e.Value + "\x00" + e.Source
}

// collectSensors lists the sensor commands of one view, with the same
// profile-delta rule as collectHooks.
func collectSensors(profile string, p *harness.Harness, base map[string]bool, fallbackDir string) []executable {
	var out []executable
	add := func(name, field, value string, s plugin.Sensor) {
		e := executable{Profile: profile, Sensor: name, Field: field, Value: value, Source: s.Origin}
		if s.Origin != "" {
			e.HarnessDir = s.Dir(fallbackDir)
		}
		e.Script = sensorScript(value, s.Dir(fallbackDir))
		if profile != "" && base[sensorKey(e)] {
			return
		}
		out = append(out, e)
	}
	for name, s := range p.Sensors {
		if s.Source.Kind() == "command" && strings.TrimSpace(s.Source.Command) != "" {
			add(name, "command", s.Source.Command, s)
		}
		if strings.TrimSpace(s.VersionCommand) != "" {
			add(name, "version_command", s.VersionCommand, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Profile != out[j].Profile {
			return out[i].Profile < out[j].Profile
		}
		if out[i].Sensor != out[j].Sensor {
			return out[i].Sensor < out[j].Sensor
		}
		return out[i].Field < out[j].Field
	})
	return out
}

func hookKey(h hookExec) string { return h.Event + "\x00" + h.Matcher + "\x00" + h.Command }

// collectHooks lists the hook commands of one resolved view. A profile view
// lists only entries the harness's own hooks do not already carry: the rest
// are reviewed once, at the top level, and repeating them per profile would
// bury what a profile actually adds.
func collectHooks(profile string, v *harness.Harness, vw runView, fallbackDir string, base map[string]bool) []hookExec {
	sources := vw.HookSources
	var out []hookExec
	for _, event := range slices.Sorted(maps.Keys(v.Hooks)) {
		for i, e := range v.Hooks[event] {
			h := hookExec{Profile: profile, Event: event, Matcher: e.Matcher, Command: e.Command}
			if i < len(sources[event]) {
				h.Source = sources[event][i]
			}
			dir := fallbackDir
			if i < len(vw.HookDirs[event]) && vw.HookDirs[event][i] != "" {
				dir = vw.HookDirs[event][i]
			}
			h.Script = hookScriptRef(e.Command, dir, vw.HookScripts)
			if profile != "" && base[hookKey(h)] {
				continue
			}
			out = append(out, h)
		}
	}
	return out
}

func mcpFromServer(profile, name, source, dir string, s plugin.MCPServer) mcpExec {
	return mcpExec{Script: mcpScript(s.Command, s.Args, dir),
		Profile: profile, Name: name, Source: source, Transport: s.Transport(),
		Command: s.Command, Args: s.Args, URL: s.URL, Cwd: s.Cwd,
		Env: mcpValues(s.Env), Headers: mcpValues(s.Headers),
	}
}

func mcpKey(m mcpExec) string {
	m.Profile = ""
	h := sha256.New()
	writeMCP(h, m)
	return hex.EncodeToString(h.Sum(nil))
}

// collectMCP lists the MCP servers of one resolved view, with the same
// profile-delta rule as collectHooks.
func collectMCP(profile string, v *harness.Harness, sources, dirs map[string]string, fallbackDir string, base map[string]bool) []mcpExec {
	var out []mcpExec
	for _, name := range slices.Sorted(maps.Keys(v.MCPServers)) {
		src := sources[name]
		if src == harness.MCPSourceRoot {
			src = ""
		}
		dir := fallbackDir
		if d := dirs[name]; d != "" {
			dir = d
		}
		m := mcpFromServer(profile, name, src, dir, v.MCPServers[name])
		if profile != "" && base[mcpKey(m)] {
			continue
		}
		out = append(out, m)
	}
	return out
}

// collectDelegateMCP lists the servers the delegates of one view carry, with
// the same profile-delta rule as collectMCP.
func collectDelegateMCP(profile string, delegates []delegateView, fallbackDir string, base map[string]bool) []mcpExec {
	var out []mcpExec
	for _, d := range delegates {
		for _, name := range slices.Sorted(maps.Keys(d.Servers)) {
			src := d.Sources[name]
			if src == harness.MCPSourceRoot {
				src = ""
			}
			dir := fallbackDir
			if dd := d.Dirs[name]; dd != "" {
				dir = dd
			}
			m := mcpFromServer(profile, name, src, dir, d.Servers[name])
			m.Delegate = d.Name
			if profile != "" && base[mcpKey(m)] {
				continue
			}
			out = append(out, m)
		}
	}
	return out
}

// harnessExecutables lists everything the harness can get executed, grouped by
// kind and sorted so the digest does not depend on Go's randomised map order:
// an unsorted digest would differ between two runs over an unchanged harness
// and report drift that is not there.
func harnessExecutables(p *harness.Harness) trustSet {
	// Sensors is never nil. A nil slice marshals to null, and a consumer
	// iterating the JSON then has to special-case it. `ynh doctor` shipped
	// exactly this bug with its findings array (#328); the published schema
	// caught it here.
	set := trustSet{Sensors: []executable{}}
	if len(p.Workspace) > 0 {
		set.Workspace = slices.Sorted(slices.Values(p.Workspace))
	}
	set.LaunchFocus = launchFocusFor(p)
	set.AutoApprove = p.AutoApprove
	for _, d := range p.DelegatesTo {
		if d.AutoApprove != "" {
			set.DelegateLevels = append(set.DelegateLevels, d.Label()+": "+d.AutoApprove)
		}
	}
	views, problems := runViews(p)
	set.Problems = problems
	baseSensors, baseHooks, baseMCP := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range views {
		set.Sensors = append(set.Sensors, collectSensors(v.Profile, v.H, baseSensors, p.Dir)...)
		set.Hooks = append(set.Hooks, collectHooks(v.Profile, v.H, v, p.Dir, baseHooks)...)
		set.MCPServers = append(set.MCPServers, collectMCP(v.Profile, v.H, v.MCPSources, v.MCPDirs, p.Dir, baseMCP)...)
		set.MCPServers = append(set.MCPServers, collectDelegateMCP(v.Profile, v.Delegates, p.Dir, baseMCP)...)
		if v.Profile != "" {
			continue
		}
		set.DelegateSources = append(set.DelegateSources, v.DelegateEntries...)
		for _, d := range v.Delegates {
			if d.Focus != nil {
				set.DelegateFocuses = append(set.DelegateFocuses, delegateFocusReport{
					Delegate: d.Name, Name: d.Focus.Name, Profile: d.Focus.Profile, Prompt: d.Focus.Prompt,
				})
			}
		}
		for _, e := range set.Sensors {
			baseSensors[sensorKey(e)] = true
		}
		for _, h := range set.Hooks {
			baseHooks[hookKey(h)] = true
		}
		for _, m := range set.MCPServers {
			baseMCP[mcpKey(m)] = true
		}
	}
	return set
}

// field writes one length-prefixed field, so no value can masquerade as the
// boundary between two.
func field(w io.Writer, s string) { _, _ = fmt.Fprintf(w, "%d:%s\x00", len(s), s) }

func writeMCP(w io.Writer, m mcpExec) {
	for _, s := range []string{m.Profile, m.Name, m.Transport, m.Command, m.URL, m.Cwd} {
		field(w, s)
	}
	field(w, fmt.Sprint(len(m.Args)))
	for _, a := range m.Args {
		field(w, a)
	}
	for _, group := range [][]mcpValue{m.Env, m.Headers} {
		field(w, fmt.Sprint(len(group)))
		for _, e := range group {
			// The raw value, not the shown one: an env value can change what
			// runs (NODE_OPTIONS, LD_PRELOAD, PATH) without any printed text
			// changing. Only the digest sees it, and the digest is a hash.
			field(w, e.Name)
			field(w, e.raw)
		}
	}
	// Only when set, so a harness without includes keeps the digest an
	// earlier ynh recorded for it.
	if m.Source != "" {
		field(w, "from")
		field(w, m.Source)
	}
	if m.Delegate != "" {
		field(w, "delegate")
		field(w, m.Delegate)
	}
	m.Script.write(w)
}

// executablesDigest fingerprints everything the operator reviews.
//
// Names and fields are included, not just the command text: moving the same
// command onto a different sensor changes when it runs and what its failure
// gates, which the operator reviewed and should be asked about again. Each kind
// is tagged, so a hook command cannot collide with a sensor command.
func executablesDigest(set trustSet) string {
	h := sha256.New()
	field(h, fmt.Sprintf("ynh-trust/%d", trustScheme))
	for _, e := range set.Sensors {
		field(h, "sensor")
		field(h, e.Profile)
		field(h, e.Source)
		field(h, e.Sensor)
		field(h, e.Field)
		field(h, e.Value)
		e.Script.write(h)
	}
	for _, e := range set.Hooks {
		field(h, "hook")
		field(h, e.Profile)
		field(h, e.Event)
		field(h, e.Matcher)
		field(h, e.Command)
		e.Script.write(h)
		// Only when set, so a harness without includes keeps the digest an
		// earlier ynh recorded for it.
		if e.Source != "" {
			field(h, "from")
			field(h, e.Source)
		}
	}
	for _, m := range set.MCPServers {
		field(h, "mcp_server")
		writeMCP(h, m)
	}
	// Only when there are entries, so a harness without a workspace keeps the
	// digest it was accepted under: no scheme bump, no spurious "changed".
	for _, w := range set.Workspace {
		field(h, "workspace")
		field(h, w)
	}
	// Likewise only when declared, so a harness without launch_focus keeps its
	// digest.
	if lf := set.LaunchFocus; lf != nil {
		field(h, "launch_focus")
		field(h, lf.Name)
		field(h, lf.Profile)
		field(h, lf.Prompt)
	}
	// Likewise only when a delegates_to entry selects a focus, so a harness
	// whose delegates name none keeps its digest.
	for _, ds := range set.DelegateSources {
		field(h, "delegate_source")
		field(h, ds)
	}
	// Likewise only when declared: an undeclared level is the default, which
	// every earlier acceptance already lived with.
	if set.AutoApprove != "" {
		field(h, "auto_approve")
		field(h, set.AutoApprove)
	}
	for _, df := range set.DelegateFocuses {
		field(h, "delegate_focus")
		field(h, df.Delegate)
		field(h, df.Name)
		field(h, df.Profile)
		field(h, df.Prompt)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))[:16]
}

// undeclaredEnvNames lists variables a sensor inherits that the harness never
// declared, sorted. Names only, never values, matching the trajectory rule that
// a record meant to prevent a leak must not become one.
//
// Sensors are handed os.Environ() whole. The harness manifest already has
// env_passthrough, whose contract says an inheriting process "holds every
// credential the operator holds, which is not a default anyone chose" — but
// that contract names MCP expansion and agent workers, and sensors were never
// wired to it. This reports the gap rather than closing it: scrubbing would
// break any sensor relying on inherited configuration, `gh` reading GH_TOKEN
// among them, and that is a change to make deliberately with the breakage
// measured first.
func undeclaredEnvNames(p *harness.Harness, environ []string) []string {
	declared := map[string]bool{}
	for _, n := range processEnvBaseline {
		declared[n] = true
	}
	for _, n := range p.EnvPassthrough {
		declared[n] = true
	}
	var out []string
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok || declared[name] || strings.HasPrefix(name, "LC_") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// processEnvBaseline mirrors internal/agent's processEnvNames: the variables a
// subprocess needs to run at all. Duplicated rather than exported because the
// agent list is the agent's contract, and coupling a report to it would make
// changing one silently change the other.
var processEnvBaseline = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TZ", "LANG", "TERM",
}

// unavailableMessage says why a harness cannot be reviewed and what to do.
func unavailableMessage(id string, problems []string) string {
	return fmt.Sprintf("%s cannot be reviewed yet: what it executes could not be read in full.\n  %s\n"+
		"Run 'ynh update %s' to fetch its includes, then review it again.",
		id, strings.Join(problems, "\n  "), id)
}

// fromSuffix labels a server an include supplied.
func fromSuffix(source string) string {
	if source == "" {
		return ""
	}
	return " from " + source
}

// delegateSuffix labels a server that only a delegate carries.
func delegateSuffix(delegate string) string {
	if delegate == "" {
		return ""
	}
	return " of delegate " + delegate
}

// scopeSuffix labels a hook or server that only a profile adds or changes.
func scopeSuffix(profile string) string {
	if profile == "" {
		return ""
	}
	return " (profile " + profile + ")"
}

// legacyDigestNote explains the one-time "changed" on a record accepted before
// the digest covered hooks and MCP servers. Those records are not auto-accepted:
// the operator never saw the hooks and servers, so the acceptance does not
// cover them.
const legacyDigestNote = "Your acceptance predates the digest covering hooks and MCP servers (it covered\n" +
	"sensor commands only), so this harness reads as changed even if nothing in it\n" +
	"has. Review the hooks and MCP servers above, then accept again."

// scriptDigestNote explains the one-time "changed" on a record accepted under
// scheme 2, which covered what a command says and not the script it runs.
func scriptDigestNote(kinds string) string {
	if kinds == "" {
		kinds = "its commands"
	}
	return "Your acceptance predates the digest covering the contents of the scripts that\n" +
		kinds + " run (it covered only the command text), so this\n" +
		"harness reads as changed even if nothing in it has. Review the scripts above,\n" +
		"then accept again."
}

// kindsOf names, in the order the gate lists them, the kinds of executable a
// harness declares: "hooks", "MCP servers", "sensors", joined with commas and
// "and". Empty when it declares none.
func kindsOf(sensors, hooks, servers int) string {
	var kinds []string
	if hooks > 0 {
		kinds = append(kinds, "hooks")
	}
	if servers > 0 {
		kinds = append(kinds, "MCP servers")
	}
	if sensors > 0 {
		kinds = append(kinds, "sensors")
	}
	if len(kinds) < 2 {
		return strings.Join(kinds, "")
	}
	return strings.Join(kinds[:len(kinds)-1], ", ") + " and " + kinds[len(kinds)-1]
}

// legacyNote is the explanation for a record accepted under the given scheme,
// naming only the kinds of executable the harness declares.
func legacyNote(scheme int, kinds string) string {
	if scheme < 2 {
		return legacyDigestNote
	}
	return scriptDigestNote(kinds)
}

// trustStatus is the report for one harness.
type trustStatus struct {
	Harness     string       `json:"harness"`
	Source      string       `json:"source,omitempty"`
	SourceType  string       `json:"source_type,omitempty"`
	Ref         string       `json:"ref,omitempty"`
	SHA         string       `json:"sha,omitempty"`
	Digest      string       `json:"digest"`
	Executables []executable `json:"executables"`
	// Hooks and MCPServers are the other things a harness can get executed,
	// as resolved for a run. They are covered by Digest like Executables.
	Hooks      []hookExec `json:"hooks,omitempty"`
	MCPServers []mcpExec  `json:"mcp_servers,omitempty"`
	// State is "unreviewed", "accepted", or "changed".
	State string `json:"state"`
	// AcceptedDigest and AcceptedAt are populated when a record exists.
	AcceptedDigest string `json:"accepted_digest,omitempty"`
	AcceptedAt     string `json:"accepted_at,omitempty"`
	// Problems is why the set could not be read completely. When present the
	// state is "unavailable" and Digest is empty: a digest of a partial set
	// would read as the whole.
	Problems []string `json:"problems,omitempty"`
	// AcceptedVia is how the acceptance was recorded. Present only when a
	// record exists and says.
	AcceptedVia string `json:"accepted_via,omitempty"`
	// LegacyDigest is true when the accepted digest predates hooks and MCP
	// servers being covered, which is why the state is "changed".
	LegacyDigest bool `json:"legacy_digest,omitempty"`
	// UndeclaredEnv names variables the sensors inherit but the harness never
	// declared. Names only.
	UndeclaredEnv []string `json:"undeclared_env,omitempty"`
	// Workspace is the workspace folders the harness requests and what would
	// become of them here. Filled by `trust show` only, which has the
	// operator's allow-list and working directory to hand.
	Workspace *workspaceReport `json:"workspace,omitempty"`
	// LaunchFocus is the focus a bare `ynh run` wakes with. Covered by Digest.
	// Filled by `trust show` only.
	LaunchFocus *launchFocusReport `json:"launch_focus,omitempty"`
	// DelegateFocuses are the focuses delegates_to entries select, whose
	// prompts are delegates' standing instructions. Covered by Digest.
	// Filled by `trust show` only.
	DelegateFocuses []delegateFocusReport `json:"delegate_focuses,omitempty"`
	// delegateFocuses is the set's, for `trust show` to fill the field.
	delegateFocuses []delegateFocusReport
	// AutoApprove is the level the harness opens at when run unattended, and
	// AutoApproveFrom where it comes from ("harness" or "default").
	// DelegateLevels are the levels its delegates_to entries declare. All are
	// covered by Digest when declared. Filled by `trust show` only.
	AutoApprove     string   `json:"auto_approve,omitempty"`
	AutoApproveFrom string   `json:"auto_approve_from,omitempty"`
	DelegateLevels  []string `json:"delegate_levels,omitempty"`
	// Moved is why a record that matches the digest still does not count:
	// the harness is installed from another source or at another commit than
	// the one accepted. Set only when the state is "changed" for that reason.
	Moved string `json:"moved,omitempty"`
	// acceptedScheme is the digest scheme of the record, for the note that
	// explains why a legacy record reads as changed.
	acceptedScheme int
}

const (
	trustUnreviewed = "unreviewed"
	trustAccepted   = "accepted"
	trustChanged    = "changed"
	// trustNoCommands is not a review outcome. A harness that ships only
	// skills executes nothing, so calling it "accepted" would claim an
	// operator approved something they were never shown, and calling it
	// "unreviewed" would put a warning on it forever. Most installed
	// harnesses are in this state.
	trustNoCommands = "no-commands"
	// trustUnavailable means what the harness executes could not be read in
	// full, typically an include that is not cached. It is neither accepted
	// nor "no-commands": the operator cannot review what is not there.
	trustUnavailable = "unavailable"
)

// trustStatusFor builds the report without deciding anything about it.
func trustStatusFor(id string, p *harness.Harness, store trustStore, environ []string) trustStatus {
	set := harnessExecutables(p)
	st := trustStatus{
		Harness:       id,
		Digest:        executablesDigest(set),
		Executables:   set.Sensors,
		Hooks:         set.Hooks,
		MCPServers:    set.MCPServers,
		State:         trustUnreviewed,
		UndeclaredEnv: undeclaredEnvNames(p, environ),

		delegateFocuses: set.DelegateFocuses,
	}
	if pr := p.InstalledFrom; pr != nil {
		st.Source, st.SourceType, st.Ref, st.SHA = pr.Source, pr.SourceType, pr.Ref, pr.SHA
	}
	if len(set.Problems) > 0 {
		st.Digest, st.State, st.Problems = "", trustUnavailable, set.Problems
		return st
	}
	if rec, ok := store.Harnesses[id]; ok {
		st.AcceptedDigest, st.AcceptedAt, st.AcceptedVia = rec.Digest, rec.AcceptedAt, rec.Via
		st.LegacyDigest = rec.Scheme < trustScheme
		st.acceptedScheme = rec.Scheme
		st.Moved = provenanceMoved(rec, st)
		if rec.Digest == st.Digest && st.Moved == "" {
			st.State = trustAccepted
		} else {
			st.State = trustChanged
		}
	}
	// Nothing to review outranks "never reviewed": a harness that executes
	// nothing cannot be a risk, and flagging it forever would train people to
	// ignore the state column that matters.
	if set.reviewable() == 0 && st.State == trustUnreviewed {
		st.State = trustNoCommands
	}
	return st
}

// provenanceMoved says why a record does not cover the harness installed under
// its id, "" when it does. A record is keyed by id alone and outlives an
// uninstall, so a different harness installed under the same id, from another
// source or at another commit, must not read as accepted just because its
// digest matches (ynh #1001). A record written without provenance (a local
// harness) matches only an install without it.
func provenanceMoved(rec trustRecord, st trustStatus) string {
	if rec.Source != st.Source {
		return fmt.Sprintf("it was accepted from %s, and is now installed from %s", describeSource(rec.Source), describeSource(st.Source))
	}
	if rec.SHA != st.SHA {
		return fmt.Sprintf("it was accepted at commit %s, and is now at %s", describeSource(rec.SHA), describeSource(st.SHA))
	}
	return ""
}

func describeSource(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// cmdTrust dispatches `ynh trust <ls|show|accept>`.
func cmdTrust(args []string) error { return cmdTrustTo(args, os.Stdout, os.Stderr) }

func cmdTrustTo(args []string, stdout, stderr io.Writer) error {
	// A leading flag is not a subcommand. `ynh trust --format json` is the
	// listing, and rejecting it as an unknown subcommand is the kind of paper
	// cut that makes a command feel broken on first contact.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return cmdTrustLs(args, stdout, stderr)
	}
	switch args[0] {
	case "ls", "list":
		return cmdTrustLs(args[1:], stdout, stderr)
	case "show":
		return cmdTrustShow(args[1:], stdout, stderr)
	case "accept":
		return cmdTrustAccept(args[1:], stdout, stderr)
	default:
		return cliError(stderr, detectJSONFormat(args), errCodeInvalidInput,
			fmt.Sprintf("unknown trust subcommand: %s", args[0]))
	}
}

// parseTrustArgs pulls --format and at most one positional harness name.
func parseTrustArgs(args []string, stderr io.Writer) (name, format string, structured bool, err error) {
	structured = detectJSONFormat(args)
	format = "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return "", "", structured, cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", structured, cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			if name != "" {
				return "", "", structured, cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			name = args[i]
		}
	}
	if format != "text" && format != "json" {
		return "", "", structured, cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
	return name, format, structured, nil
}

func cmdTrustLs(args []string, stdout, stderr io.Writer) error {
	name, format, structured, err := parseTrustArgs(args, stderr)
	if err != nil {
		return err
	}
	if name != "" {
		return cmdTrustShow(args, stdout, stderr)
	}

	store, err := loadTrustStore()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	entries, err := harness.ListAll()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	environ := os.Environ()
	statuses := make([]trustStatus, 0, len(entries))
	for _, e := range entries {
		id := canonicalIDForEntry(e)
		p, loadErr := harness.LoadQualified(id)
		if loadErr != nil {
			// A harness that will not load cannot be reported on, and this
			// command is not the place to fail the operator's whole listing
			// for it. `ynh doctor` is.
			continue
		}
		statuses = append(statuses, trustStatusFor(id, p, store, environ))
	}

	if format == "json" {
		return writeJSON(stdout, statuses)
	}
	return printTrustList(stdout, statuses)
}

func cmdTrustShow(args []string, stdout, stderr io.Writer) error {
	name, format, structured, err := parseTrustArgs(args, stderr)
	if err != nil {
		return err
	}
	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh trust show <harness|folder> [--format text|json]")
	}
	// A folder (./dir) has no install id and no record of its own: show the
	// remote includes whose commands the gate checks, as accept would record.
	if namespace.Classify(name) == namespace.RefPath {
		return showFolderIncludes(name, format, structured, stdout, stderr)
	}
	p, err := harness.LoadQualified(name)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}
	store, err := loadTrustStore()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	st := trustStatusFor(name, p, store, os.Environ())
	st.Workspace = workspaceReportFor(p)
	st.LaunchFocus = launchFocusFor(p)
	st.DelegateFocuses = st.delegateFocuses
	st.AutoApprove, st.AutoApproveFrom = p.AutoApproveOf(), p.AutoApproveOrigin()
	st.DelegateLevels = harnessExecutables(p).DelegateLevels

	if format == "json" {
		return writeJSON(stdout, st)
	}
	return printTrustDetail(stdout, st)
}

func cmdTrustAccept(args []string, stdout, stderr io.Writer) error {
	name, format, structured, err := parseTrustArgs(args, stderr)
	if err != nil {
		return err
	}
	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh trust accept <harness>")
	}
	// A folder (./dir) has no install id: accepting it records the remote
	// includes' commands, the only part of a folder run the gate checks.
	if namespace.Classify(name) == namespace.RefPath {
		return acceptFolderIncludes(name, format, structured, stdout, stderr)
	}
	p, err := harness.LoadQualified(name)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}
	store, err := loadTrustStore()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	set := harnessExecutables(p)
	if len(set.Problems) > 0 {
		// Accepting a partial set would record "I reviewed it" for commands
		// the operator was never shown.
		return cliError(stderr, structured, errCodeConfigError, unavailableMessage(name, set.Problems))
	}
	// Acceptance is the operator's review of what is installed, so a remote
	// include or native delegate entry that floats (a branch, a tag or no ref)
	// is accepted at the commit it is at now and that commit is recorded, not
	// refused; only --trust, which accepts unreviewed, needs a full SHA
	// (ynh #1002, as for a floating own ref, ynh #1023).
	remote, rerr := remoteIncludes(p, "accept")
	if rerr != nil {
		return cliError(stderr, structured, errCodeConfigError, rerr.Error())
	}
	floating := floatingPins(p, remote)
	rec := trustRecord{
		Digest:     executablesDigest(set),
		Commands:   set.count(),
		Scheme:     trustScheme,
		Via:        viaAccept,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
		Floating:   floating,
	}
	if pr := p.InstalledFrom; pr != nil {
		rec.Source, rec.Ref, rec.SHA = pr.Source, pr.Ref, pr.SHA
	}
	store.Harnesses[name] = rec
	if err := saveTrustStore(store); err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	if format == "json" {
		return writeJSON(stdout, trustStatusFor(name, p, store, os.Environ()))
	}
	workspaceNote := ""
	if n := len(set.Workspace); n > 0 {
		workspaceNote = fmt.Sprintf(", %d workspace folder(s)", n)
	}
	_, err = fmt.Fprintf(stdout, "Accepted %s (%s): %d command(s)%s, %s\n", name, viaAccept, rec.Commands, workspaceNote, rec.Digest)
	if err == nil {
		err = printFloating(stdout, floating)
	}
	return err
}

func printTrustList(w io.Writer, statuses []trustStatus) error {
	if len(statuses) == 0 {
		_, err := fmt.Fprintln(w, "No harnesses installed.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "HARNESS\tSTATE\tVIA\tCOMMANDS\tSOURCE"); err != nil {
		return err
	}
	changed, unreviewed := 0, 0
	for _, s := range statuses {
		switch s.State {
		case trustChanged:
			changed++
		case trustUnreviewed:
			unreviewed++
		}
		src := s.Source
		if src == "" {
			src = "(local)"
		}
		via := s.AcceptedVia
		if via == "" {
			via = "-"
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", s.Harness, s.State, via, len(s.Executables)+len(s.Hooks)+len(s.MCPServers), src); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if changed+unreviewed == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w, "\nRun 'ynh trust show <harness>' to see what a harness will execute.\n")
	return err
}

func printTrustDetail(w io.Writer, s trustStatus) error {
	_, _ = fmt.Fprintf(w, "%s\n", s.Harness)
	if s.Source != "" {
		_, _ = fmt.Fprintf(w, "  source:   %s", s.Source)
		if s.Ref != "" {
			_, _ = fmt.Fprintf(w, " (%s)", s.Ref)
		}
		_, _ = fmt.Fprintln(w)
		if s.SHA != "" {
			_, _ = fmt.Fprintf(w, "  commit:   %s\n", s.SHA)
		}
	} else {
		_, _ = fmt.Fprintf(w, "  source:   (local)\n")
	}
	_, _ = fmt.Fprintf(w, "  state:    %s\n", s.State)
	if s.State == trustUnavailable {
		_, _ = fmt.Fprintf(w, "\n%s\n", unavailableMessage(s.Harness, s.Problems))
		return nil
	}
	_, _ = fmt.Fprintf(w, "  digest:   %s\n", s.Digest)
	if s.State == trustChanged {
		_, _ = fmt.Fprintf(w, "  accepted: %s on %s\n", s.AcceptedDigest, s.AcceptedAt)
	}
	if s.Moved != "" {
		_, _ = fmt.Fprintf(w, "  moved:    %s\n", s.Moved)
	}
	if s.AcceptedVia != "" {
		_, _ = fmt.Fprintf(w, "  via:      %s\n", s.AcceptedVia)
	}

	if len(s.Executables)+len(s.Hooks)+len(s.MCPServers) == 0 && s.Workspace == nil && s.LaunchFocus == nil && len(s.DelegateFocuses) == 0 {
		_, _ = fmt.Fprintln(w, "\nThis harness declares no commands.")
		printAutoApproveSection(w, s)
		return nil
	}

	if len(s.Executables) > 0 {
		_, _ = fmt.Fprintf(w, "\nSensors, executed via /bin/sh -c in the tree being measured:\n\n")
		for _, e := range s.Executables {
			_, _ = fmt.Fprintf(w, "  %s (%s)%s\n    %s\n", e.Sensor, e.Field, scopeSuffix(e.Profile), e.Value)
			if e.Source != "" {
				_, _ = fmt.Fprintf(w, "    from %s; $YNH_HARNESS_DIR is %s\n", e.Source, e.HarnessDir)
			}
			writeScriptLine(w, e.Script)
		}
	}
	if len(s.Hooks) > 0 {
		_, _ = fmt.Fprintf(w, "\nHooks, executed by the vendor CLI with your environment:\n\n")
		for _, h := range s.Hooks {
			m := ""
			if h.Matcher != "" {
				m = " [" + h.Matcher + "]"
			}
			_, _ = fmt.Fprintf(w, "  %s%s%s%s\n    %s\n", h.Event, m, fromSuffix(h.Source), scopeSuffix(h.Profile), h.Command)
			writeScriptLine(w, h.Script)
		}
	}
	if len(s.MCPServers) > 0 {
		_, _ = fmt.Fprintf(w, "\nMCP servers, launched or contacted by the vendor CLI:\n\n")
		for _, m := range s.MCPServers {
			_, _ = fmt.Fprintf(w, "  %s (%s)%s%s\n", m.Name, m.Transport, delegateSuffix(m.Delegate)+fromSuffix(m.Source), scopeSuffix(m.Profile))
			if m.Command != "" {
				_, _ = fmt.Fprintf(w, "    command: %s\n", strings.Join(append([]string{m.Command}, m.Args...), " "))
				writeScriptLine(w, m.Script)
			}
			if m.URL != "" {
				_, _ = fmt.Fprintf(w, "    url:     %s\n", m.URL)
			}
			if m.Cwd != "" {
				_, _ = fmt.Fprintf(w, "    cwd:     %s\n", m.Cwd)
			}
			for _, e := range m.Env {
				_, _ = fmt.Fprintf(w, "    env:     %s=%s\n", e.Name, e.Value)
			}
			for _, e := range m.Headers {
				_, _ = fmt.Fprintf(w, "    header:  %s=%s\n", e.Name, e.Value)
			}
		}
	}

	printWorkspaceSection(w, s.Workspace)

	if lf := s.LaunchFocus; lf != nil {
		_, _ = fmt.Fprintf(w, "\nLaunch focus, the first message of every bare 'ynh run' (a prompt it is given replaces this one):\n\n  %s%s\n    %q\n", lf.Name, scopeSuffix(lf.Profile), lf.Prompt)
	}
	if len(s.DelegateFocuses) > 0 {
		_, _ = fmt.Fprintf(w, "\nDelegate focuses, standing instructions of a delegate's subagent for every task it is given:\n\n")
		for _, df := range s.DelegateFocuses {
			_, _ = fmt.Fprintf(w, "  %s%s%s\n    %q\n", df.Name, delegateSuffix(df.Delegate), scopeSuffix(df.Profile), df.Prompt)
		}
	}

	printAutoApproveSection(w, s)

	if len(s.UndeclaredEnv) > 0 {
		_, _ = fmt.Fprintf(w, "\nThese commands inherit your full environment. %d variable(s) reach them\n"+
			"that this harness never declared in env_passthrough:\n\n", len(s.UndeclaredEnv))
		_, _ = fmt.Fprintf(w, "  %s\n", strings.Join(s.UndeclaredEnv, ", "))
	}

	switch s.State {
	case trustUnreviewed:
		_, _ = fmt.Fprintf(w, "\nNot yet reviewed. 'ynh trust accept %s' records that you have read the above.\n", s.Harness)
	case trustChanged:
		if s.LegacyDigest {
			_, _ = fmt.Fprintf(w, "\n%s\n", legacyNote(s.acceptedScheme, kindsOf(len(s.Executables), len(s.Hooks), len(s.MCPServers))))
		}
		if s.Moved != "" {
			_, _ = fmt.Fprintf(w, "\nThis record does not cover what is installed: %s.\n"+
				"'ynh trust accept %s' records what you review now.\n", s.Moved, s.Harness)
			break
		}
		_, _ = fmt.Fprintf(w, "\nThese commands have changed since you accepted them.\n"+
			"'ynh trust accept %s' records the new set.\n", s.Harness)
	}
	return nil
}

// printAutoApproveSection lists the permission level a one-shot run of the
// harness opens at, which acceptance covers (ynh #1058, D3, N2): an undeclared
// level is "all", said plainly, and each delegates_to entry's declared level
// follows.
func printAutoApproveSection(w io.Writer, s trustStatus) {
	if s.AutoApprove == "" {
		return
	}
	_, _ = fmt.Fprintf(w, "\nPermission level when run unattended (a delegate session, a one-shot 'ynh run' or 'ynh agent run'):\n\n")
	if s.AutoApproveFrom == harness.AutoApproveFromDefault {
		_, _ = fmt.Fprintf(w, "  opens at %s when run unattended (not declared; the default for an accepted harness)\n", s.AutoApprove)
	} else {
		_, _ = fmt.Fprintf(w, "  opens at %s when run unattended (declared by the harness)\n", s.AutoApprove)
	}
	for _, l := range s.DelegateLevels {
		_, _ = fmt.Fprintf(w, "  delegate %s\n", l)
	}
}
