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
	// Source, Ref and SHA are copied from provenance at acceptance time.
	// Copied rather than referenced: the point is what the operator was
	// looking at when they accepted, which an update would otherwise rewrite.
	Source     string `json:"source,omitempty"`
	Ref        string `json:"ref,omitempty"`
	SHA        string `json:"sha,omitempty"`
	AcceptedAt string `json:"accepted_at"`
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
// commands only; 2 adds hook commands and MCP servers. A digest from an older
// scheme can never equal a current one, because the current input is prefixed
// with the scheme, so an old acceptance fails closed as "changed".
const trustScheme = 2

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
	n := t.count() + len(t.Workspace)
	if t.LaunchFocus != nil {
		n++
	}
	return n
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
	// HookSources maps each event of H.Hooks to the include chain that
	// supplied each entry, "" for the harness's own.
	HookSources map[string][]string
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
	Name    string
	Servers map[string]plugin.MCPServer
	Sources map[string]string
}

// runSources is where the servers and hooks of a view came from.
type runSources struct {
	mcp       map[string]string
	hooks     map[string][]string
	delegates []delegateView
}

func runViews(p *harness.Harness) (views []runView, problems []string) {
	// fold returns a copy of h carrying what the includes bring under sel: the
	// MCP servers of every harness they reach, composed as a run composes them,
	// with their sources. h itself is not changed.
	fold := func(h *harness.Harness, sel harness.Selection) (*harness.Harness, runSources, []resolver.ResolveResult, error) {
		src := runSources{mcp: map[string]string{}}
		delegates, err := delegateViews(h)
		if err != nil {
			return nil, src, nil, err
		}
		src.delegates = delegates
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
		for _, pr := range provenance {
			src.mcp[pr.Server] = pr.Source
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
		views = append(views, runView{Profile: profile, H: merged, MCPSources: src.mcp, HookSources: src.hooks, Delegates: src.delegates})
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
				views = append(views, runView{Profile: qualified, H: v, MCPSources: vSrc.mcp, HookSources: vSrc.hooks, Delegates: vSrc.delegates})
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

// delegateViews reads the MCP servers each delegate of h carries on its own,
// the way a run composes them: the delegate's includes (resolved against its
// own directory, from the cache), then its own servers. A delegate's own
// delegates are among them, as a run flattens them. Anything unreadable is
// an error, so the set is never reported complete without it.
func delegateViews(h *harness.Harness) ([]delegateView, error) {
	flat, err := resolver.FlattenDelegates(h.DelegatesTo, h.Dir, h.Name, resolver.ResolveDelegateLocal)
	if err != nil {
		return nil, err
	}
	var out []delegateView
	for _, fd := range flat {
		dh := fd.Harness
		var included []harness.IncludedHarness
		if len(dh.Includes) > 0 {
			resolved, err := resolver.ResolveIncludesLocal(dh, harness.Selection{})
			if err != nil {
				return nil, fmt.Errorf("delegate %s: includes: %w", dh.Name, err)
			}
			included = resolver.IncludedHarnesses(resolved)
		}
		servers, provenance, err := harness.ComposeMCPServersRaw(dh, included)
		if err != nil {
			return nil, fmt.Errorf("delegate %s: %w", dh.Name, err)
		}
		dv := delegateView{Name: dh.Name, Servers: servers, Sources: map[string]string{}}
		for _, pr := range provenance {
			dv.Sources[pr.Server] = pr.Source
		}
		out = append(out, dv)
	}
	return out, nil
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
func collectHooks(profile string, v *harness.Harness, sources map[string][]string, base map[string]bool) []hookExec {
	var out []hookExec
	for _, event := range slices.Sorted(maps.Keys(v.Hooks)) {
		for i, e := range v.Hooks[event] {
			h := hookExec{Profile: profile, Event: event, Matcher: e.Matcher, Command: e.Command}
			if i < len(sources[event]) {
				h.Source = sources[event][i]
			}
			if profile != "" && base[hookKey(h)] {
				continue
			}
			out = append(out, h)
		}
	}
	return out
}

func mcpFromServer(profile, name, source string, s plugin.MCPServer) mcpExec {
	return mcpExec{
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
func collectMCP(profile string, v *harness.Harness, sources map[string]string, base map[string]bool) []mcpExec {
	var out []mcpExec
	for _, name := range slices.Sorted(maps.Keys(v.MCPServers)) {
		src := sources[name]
		if src == harness.MCPSourceRoot {
			src = ""
		}
		m := mcpFromServer(profile, name, src, v.MCPServers[name])
		if profile != "" && base[mcpKey(m)] {
			continue
		}
		out = append(out, m)
	}
	return out
}

// collectDelegateMCP lists the servers the delegates of one view carry, with
// the same profile-delta rule as collectMCP.
func collectDelegateMCP(profile string, delegates []delegateView, base map[string]bool) []mcpExec {
	var out []mcpExec
	for _, d := range delegates {
		for _, name := range slices.Sorted(maps.Keys(d.Servers)) {
			src := d.Sources[name]
			if src == harness.MCPSourceRoot {
				src = ""
			}
			m := mcpFromServer(profile, name, src, d.Servers[name])
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
	views, problems := runViews(p)
	set.Problems = problems
	baseSensors, baseHooks, baseMCP := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, v := range views {
		set.Sensors = append(set.Sensors, collectSensors(v.Profile, v.H, baseSensors, p.Dir)...)
		set.Hooks = append(set.Hooks, collectHooks(v.Profile, v.H, v.HookSources, baseHooks)...)
		set.MCPServers = append(set.MCPServers, collectMCP(v.Profile, v.H, v.MCPSources, baseMCP)...)
		set.MCPServers = append(set.MCPServers, collectDelegateMCP(v.Profile, v.Delegates, baseMCP)...)
		if v.Profile != "" {
			continue
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
	}
	for _, e := range set.Hooks {
		field(h, "hook")
		field(h, e.Profile)
		field(h, e.Event)
		field(h, e.Matcher)
		field(h, e.Command)
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
	}
	if pr := p.InstalledFrom; pr != nil {
		st.Source, st.SourceType, st.Ref, st.SHA = pr.Source, pr.SourceType, pr.Ref, pr.SHA
	}
	if len(set.Problems) > 0 {
		st.Digest, st.State, st.Problems = "", trustUnavailable, set.Problems
		return st
	}
	if rec, ok := store.Harnesses[id]; ok {
		st.AcceptedDigest, st.AcceptedAt = rec.Digest, rec.AcceptedAt
		st.LegacyDigest = rec.Scheme < trustScheme
		if rec.Digest == st.Digest {
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
			"usage: ynh trust show <harness> [--format text|json]")
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
	rec := trustRecord{
		Digest:     executablesDigest(set),
		Commands:   set.count(),
		Scheme:     trustScheme,
		AcceptedAt: time.Now().UTC().Format(time.RFC3339),
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
	_, err = fmt.Fprintf(stdout, "Recorded %s: %d command(s)%s, %s\n", name, rec.Commands, workspaceNote, rec.Digest)
	return err
}

func printTrustList(w io.Writer, statuses []trustStatus) error {
	if len(statuses) == 0 {
		_, err := fmt.Fprintln(w, "No harnesses installed.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "HARNESS\tSTATE\tCOMMANDS\tSOURCE"); err != nil {
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
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", s.Harness, s.State, len(s.Executables)+len(s.Hooks)+len(s.MCPServers), src); err != nil {
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

	if len(s.Executables)+len(s.Hooks)+len(s.MCPServers) == 0 && s.Workspace == nil && s.LaunchFocus == nil {
		_, _ = fmt.Fprintln(w, "\nThis harness declares no commands.")
		return nil
	}

	if len(s.Executables) > 0 {
		_, _ = fmt.Fprintf(w, "\nSensors, executed via /bin/sh -c in the tree being measured:\n\n")
		for _, e := range s.Executables {
			_, _ = fmt.Fprintf(w, "  %s (%s)%s\n    %s\n", e.Sensor, e.Field, scopeSuffix(e.Profile), e.Value)
			if e.Source != "" {
				_, _ = fmt.Fprintf(w, "    from %s; $YNH_HARNESS_DIR is %s\n", e.Source, e.HarnessDir)
			}
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
		}
	}
	if len(s.MCPServers) > 0 {
		_, _ = fmt.Fprintf(w, "\nMCP servers, launched or contacted by the vendor CLI:\n\n")
		for _, m := range s.MCPServers {
			_, _ = fmt.Fprintf(w, "  %s (%s)%s%s\n", m.Name, m.Transport, delegateSuffix(m.Delegate)+fromSuffix(m.Source), scopeSuffix(m.Profile))
			if m.Command != "" {
				_, _ = fmt.Fprintf(w, "    command: %s\n", strings.Join(append([]string{m.Command}, m.Args...), " "))
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
			_, _ = fmt.Fprintf(w, "\n%s\n", legacyDigestNote)
		}
		_, _ = fmt.Fprintf(w, "\nThese commands have changed since you accepted them.\n"+
			"'ynh trust accept %s' records the new set.\n", s.Harness)
	}
	return nil
}

// warnIfTrustChanged reports on stderr when a harness's commands differ from
// what the operator accepted.
//
// Only "changed" warns. An unreviewed harness is the state of every install
// predating this command, so warning on it would put a line on almost every
// check, and a warning that is always there is one nobody reads. That is the
// same reasoning the stale-claim workflow records for notifying rather than
// gating: the false-positive rate decides whether anyone keeps looking.
//
// It never fails the check. A harness whose commands changed may well be fine,
// and this release does not claim to know which. Blocking is a later decision.
func warnIfTrustChanged(stderr io.Writer, id string, p *harness.Harness) {
	if stderr == nil || p == nil {
		return
	}
	store, err := loadTrustStore()
	if err != nil {
		// A trust record that cannot be read must not stop a gate from running.
		return
	}
	rec, ok := store.Harnesses[id]
	if !ok {
		return
	}
	set := harnessExecutables(p)
	if len(set.Problems) > 0 {
		_, _ = fmt.Fprintf(stderr,
			"warning: %s cannot be checked against what you accepted on %s: its includes could not be read.\n"+
				"  Run 'ynh update %s'.\n", id, rec.AcceptedAt, id)
		return
	}
	digest := executablesDigest(set)
	if digest == rec.Digest {
		return
	}
	_, _ = fmt.Fprintf(stderr,
		"warning: %s declares different commands than you accepted on %s.\n"+
			"  accepted %s, now %s. Review with 'ynh trust show %s'.\n",
		id, rec.AcceptedAt, rec.Digest, digest, id)
	if rec.Scheme < trustScheme {
		_, _ = fmt.Fprintf(stderr, "  %s\n", strings.ReplaceAll(legacyDigestNote, "\n", "\n  "))
	}
}
