package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// HarnessJSON represents the .harness.json manifest — single source of truth.
type HarnessJSON struct {
	Schema        string                 `json:"$schema,omitempty"`
	Name          string                 `json:"name"`
	Version       string                 `json:"version"`
	Description   string                 `json:"description,omitempty"`
	Author        *AuthorInfo            `json:"author,omitempty"`
	Keywords      []string               `json:"keywords,omitempty"`
	DefaultVendor string                 `json:"default_vendor,omitempty"`
	Includes      []IncludeMeta          `json:"includes,omitempty"`
	DelegatesTo   []DelegateMeta         `json:"delegates_to,omitempty"`
	Hooks         map[string][]HookEntry `json:"hooks,omitempty"`
	MCPServers    map[string]MCPServer   `json:"mcp_servers,omitempty"`
	// Agent carries loop defaults a harness can set once rather than every
	// caller passing flags.
	Agent *AgentConfig `json:"agent,omitempty"`
	// EnvPassthrough names the environment variables a harness may see:
	// which ${VAR} references its MCP declarations may resolve, and which
	// variables reach an agent worker's process. Empty means none — a worker
	// inheriting the operator's whole environment holds every credential the
	// operator holds, which is not a default anyone chose.
	EnvPassthrough []string           `json:"env_passthrough,omitempty"`
	Profiles       map[string]Profile `json:"profiles,omitempty"`
	Focuses        map[string]Focus   `json:"focuses,omitempty"`
	Sensors        map[string]Sensor  `json:"sensors,omitempty"`
	// Reads declares, per artifact, the harness-root-relative paths that
	// artifact tells an agent to open. Keys are artifact paths
	// ("skills/<name>", "agents/<name>.md"); values are the paths read.
	//
	// It exists because nothing else can tell the two cases apart. A skill
	// body mentions paths constantly for good reasons, and a path that
	// resolves in the authoring repo but not in what ships is
	// indistinguishable from one that resolves in both. Three shipped
	// defects came from that gap, so the author states the intent and
	// `ynd lint` checks it.
	Reads         map[string][]string `json:"reads,omitempty"`
	InstalledFrom *ProvenanceMeta     `json:"installed_from,omitempty"`
}

// Sensor declares an observation surface — a feedforward signal a loop
// driver consumes between agent turns. ynh declares; the loop driver runs.
type Sensor struct {
	Category string `json:"category,omitempty"`
	Role     string `json:"role,omitempty"`
	// Tolerance declares how a failing result is treated by `ynh check`.
	// Empty means "blocking" — the safe default for a gate. This is the one
	// piece of pass/fail policy ynh owns; everything richer (thresholds,
	// severity filters, convergence) still belongs to a loop driver.
	Tolerance string `json:"tolerance,omitempty"`
	// VersionCommand prints the version of the tool this sensor runs, e.g.
	// "golangci-lint --version".
	//
	// Declared rather than inferred. Guessing `<first token> --version` is
	// wrong for a wrapper — `make lint` would report make's version, not the
	// linter's — and hangs outright on commands that do not support the flag.
	// A corpus graded across weeks cannot defend a yield number without it: a
	// change in findings and a change in the tool are otherwise the same
	// observation.
	VersionCommand string `json:"version_command,omitempty"`
	// Reference names a fixture this sensor must produce a known result on.
	//
	// Nothing else verifies that a sensor still detects what it claims to. A
	// sensor is a command plus an expectation about its exit code; if the
	// command quietly stops examining anything — a config change excluding a
	// directory, an upgrade renaming a rule, a path that no longer matches —
	// it exits 0 and the gate reports green. Everything else depends on
	// sensors telling the truth: the ratchet forgives against their output,
	// the loop converges on their verdicts, and any yield figure derives from
	// them.
	Reference *SensorReference `json:"reference,omitempty"`
	// Ratchet selects how the baseline forgives this sensor's failures.
	//
	// "fingerprint" (the default) matches individual findings, so a fixed one
	// stops being forgiven and a new one is flagged wherever it appears.
	//
	// "count" ratchets the total instead. It exists because fingerprints
	// normalise line numbers and deduplicate, so a second identical finding in
	// a file that already has one produces no new fingerprint and no change in
	// the distinct-line count — it is invisible. That is the wrong answer for
	// anything whose *quantity* is the finding, suppression directives above
	// all: the gaming vector for a ratchet is suppression, not relocation, and
	// an agent that adds `//nolint` beside an existing one must not pass.
	Ratchet string `json:"ratchet,omitempty"`
	// Observes names the paths a `files` sensor's artifact actually depends
	// on, so ynh can tell whether that artifact still describes the tree.
	//
	// A files sensor reads a result some other process left behind. ynh cannot
	// judge what it says, but it can judge whether it still applies — and an
	// artifact produced against different code is an observation of something
	// else, not a weaker observation of this one.
	//
	// Empty means the whole tracked tree, which is deliberately strict: a
	// harness that will not say what its artifact depends on gets the only
	// honest assumption, which is everything. Declaring the real inputs is one
	// line and makes the sensor quiet.
	//
	// Ignored for command and focus sensors, which observe by running.
	Observes []string     `json:"observes,omitempty"`
	Source   SensorSource `json:"source"`
	Output   SensorOutput `json:"output"`
}

// SensorReference is a fixture with a known answer, used by
// `ynh check --calibrate` to prove a sensor still observes.
type SensorReference struct {
	// Path is the fixture directory the sensor runs against, relative to the
	// harness. It must live outside the agent's write path: a reference an
	// agent can edit calibrates nothing.
	Path string `json:"path"`
	// Expect is the result the fixture must produce. "fail" is the case that
	// matters — a sensor that passes a fixture designed to trip it has stopped
	// observing. "pass" catches the opposite failure, a sensor that fires on
	// clean input.
	Expect string `json:"expect"`
}

// ValidSensorRatchets lists how a sensor's baseline forgives failures.
var ValidSensorRatchets = map[string]bool{
	"fingerprint": true,
	"count":       true,
}

// EffectiveRatchet returns the ratchet mode, defaulting to fingerprint.
func (s Sensor) EffectiveRatchet() string {
	if s.Ratchet == "" {
		return "fingerprint"
	}
	return s.Ratchet
}

// ValidSensorExpectations lists the answers a reference fixture may declare.
var ValidSensorExpectations = map[string]bool{
	"fail": true,
	"pass": true,
}

// ValidSensorTolerances lists how `ynh check` treats a failing sensor.
// Maps onto the three enforcement loops: blocking sensors gate a merge,
// advisory sensors report loudly without failing the run, report sensors
// are pure observation.
var ValidSensorTolerances = map[string]bool{
	"blocking": true,
	"advisory": true,
	"report":   true,
}

// Tolerance returns the effective tolerance, defaulting to "blocking".
func (s Sensor) EffectiveTolerance() string {
	if s.Tolerance == "" {
		return "blocking"
	}
	return s.Tolerance
}

// ValidSensorRoles lists the role hints loop drivers can use to discover
// which sensor plays which role. Loop-driver policy, not ynh policy —
// ynh just stores the hint.
var ValidSensorRoles = map[string]bool{
	"regular":              true,
	"convergence-verifier": true,
	"stuck-recovery":       true,
}

// SensorSource is a strict one-of: files, command, or focus. Exactly one
// must be set. Discriminated by structure, not labels.
type SensorSource struct {
	Files        []string            `json:"files,omitempty"`
	Command      string              `json:"command,omitempty"`
	Focus        *FocusRef           `json:"focus,omitempty"`
	GitHubStatus *GitHubStatusSource `json:"github_status,omitempty"`
	GitHubCheck  *GitHubCheckSource  `json:"github_check,omitempty"`
}

// GitHubStatusSource observes a commit status: the older Status API, which is
// what most external services still post to (coverage bots, security scanners,
// deploy pipelines).
//
// It exists because these verdicts are not derivable locally. A command sensor
// can run your linter; it cannot know what a third-party scanner concluded
// about this commit on infrastructure you do not control.
type GitHubStatusSource struct {
	// Context is the status context to select, e.g. "security/snyk".
	// Required — a sensor that observes "whatever statuses exist" reports on a
	// set that changes underneath it, which is not an observation.
	Context string `json:"context"`

	// Require is the state that counts as passing. Default "success".
	Require string `json:"require,omitempty"`

	// Repo is "owner/name". Default: inferred from the origin remote of the
	// directory under test, so a sensor stays portable across forks.
	Repo string `json:"repo,omitempty"`

	// Ref is the commit to ask about. Default "HEAD", resolved in the
	// directory under test, which is what makes this work under --cwd.
	Ref string `json:"ref,omitempty"`

	// OnMissing is the verdict when the context is absent or still pending.
	// One of "broken", "fail", "pass". Default "broken".
	OnMissing string `json:"on_missing,omitempty"`
}

// GitHubCheckSource observes a check run: the Checks API, which is what a
// GitHub App posts (CodeQL, Dependabot, and anything built on Actions).
//
// Kept separate from GitHubStatusSource deliberately. The two APIs disagree
// about vocabulary — a status has a state, a check run has a status and a
// separate conclusion — and collapsing them into one shape would mean guessing
// which the author meant.
type GitHubCheckSource struct {
	// Name is the check run name, e.g. "CodeQL". Required, for the same
	// reason Context is.
	Name string `json:"name"`

	// App filters by the GitHub App slug that posted the run, e.g. "github-actions".
	// Optional. Use it when two apps post runs of the same name.
	App string `json:"app,omitempty"`

	// Require is the conclusion that counts as passing. Default "success".
	Require string `json:"require,omitempty"`

	Repo      string `json:"repo,omitempty"`
	Ref       string `json:"ref,omitempty"`
	OnMissing string `json:"on_missing,omitempty"`
}

// ValidGitHubStatusStates are the states the Status API can report.
var ValidGitHubStatusStates = map[string]bool{
	"success": true, "pending": true, "failure": true, "error": true,
}

// ValidGitHubCheckConclusions are the conclusions the Checks API can report.
// A run that has not concluded has no conclusion at all, which is why
// "pending" is not a member here and is handled by OnMissing instead.
var ValidGitHubCheckConclusions = map[string]bool{
	"success": true, "failure": true, "neutral": true, "cancelled": true,
	"timed_out": true, "action_required": true, "stale": true, "skipped": true,
}

// ValidSensorOnMissing are the verdicts available when the observation is
// absent. "broken" is the default on purpose: a sensor that cannot see is not
// a sensor that passed.
var ValidSensorOnMissing = map[string]bool{
	"broken": true, "fail": true, "pass": true,
}

// Kind reports which source variant is set. Returns "" if none or
// (impossibly, for a validated sensor) more than one.
func (s SensorSource) Kind() string {
	count := 0
	kind := ""
	if s.Files != nil {
		count++
		kind = "files"
	}
	if s.Command != "" {
		count++
		kind = "command"
	}
	if s.Focus != nil {
		count++
		kind = "focus"
	}
	if s.GitHubStatus != nil {
		count++
		kind = "github_status"
	}
	if s.GitHubCheck != nil {
		count++
		kind = "github_check"
	}
	if count != 1 {
		return ""
	}
	return kind
}

// UnmarshalJSON enforces that exactly one source variant is set.
func (s *SensorSource) UnmarshalJSON(data []byte) error {
	type alias SensorSource
	var raw alias
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	*s = SensorSource(raw)
	if s.Kind() == "" {
		return fmt.Errorf("source must have exactly one of files, command, focus, github_status, github_check")
	}
	return nil
}

// FocusRef is either a string (referring to a named top-level focus) or
// an inline focus object.
type FocusRef struct {
	Name   string       // set when source.focus is a string reference
	Inline *InlineFocus // set when source.focus is an inline object
}

// InlineFocus is a focus declared inline inside a sensor's source.
// Inline focuses are not exposed via --focus or YNH_FOCUS — they live
// only as the sensor's source.
type InlineFocus struct {
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt"`
}

// UnmarshalJSON accepts either a string ref or an inline focus object.
func (f *FocusRef) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if s == "" {
			return fmt.Errorf("focus reference must not be empty")
		}
		f.Name = s
		return nil
	}
	var inline InlineFocus
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&inline); err != nil {
		return err
	}
	if inline.Prompt == "" {
		return fmt.Errorf("inline focus must have a non-empty prompt")
	}
	f.Inline = &inline
	return nil
}

// MarshalJSON emits the variant that is set.
func (f FocusRef) MarshalJSON() ([]byte, error) {
	if f.Inline != nil {
		return json.Marshal(f.Inline)
	}
	return json.Marshal(f.Name)
}

// SensorOutput declares where a sensor's result lives and what shape it
// has. format is a freeform pass-through identifier; ynh maintains no
// vocabulary.
type SensorOutput struct {
	Format  string `json:"format"`
	Channel string `json:"channel,omitempty"`
	Path    string `json:"path,omitempty"`
	// Match selects which of a sensor's output lines are findings.
	//
	// Without it every non-blank line is treated as a finding, so a sensor
	// wrapping a real tool records that tool's decoration as accepted debt:
	// headers, source context, caret markers and summary counts. Fix one real
	// finding and `7 issues:` becomes `6 issues:`, a line the baseline has
	// never seen, and the gate reports a correct repair as a new finding
	// (#338).
	//
	// A regular expression, matched against each line. ynh does not interpret
	// it beyond that, which keeps it out of the business of knowing any tool's
	// output format. Empty means every line counts, which stays the default so
	// existing sensors are unaffected.
	Match string `json:"match,omitempty"`
}

// OutputMatcher compiles the sensor's line filter. A nil result means every
// non-blank line is a finding.
//
// Compile once per sensor and pass the result down: compiling per line is
// wasted work on output that can run to thousands of lines.
func (s Sensor) OutputMatcher() (*regexp.Regexp, error) {
	if s.Output.Match == "" {
		return nil, nil
	}
	re, err := regexp.Compile(s.Output.Match)
	if err != nil {
		return nil, fmt.Errorf("output.match %q: %w", s.Output.Match, err)
	}
	return re, nil
}

// ValidSensorCategories lists the Fowler buckets a sensor's category may use.
var ValidSensorCategories = map[string]bool{
	"maintainability": true,
	"architecture":    true,
	"behaviour":       true,
}

// ValidateSensors checks each sensor's source/output/category and resolves
// focus references against the supplied profile and focus name sets.
// profileNames and focusNames may be nil if no cross-reference data is
// available (callers should pass both to enable full validation).
func ValidateSensors(sensors map[string]Sensor, profileNames, focusNames map[string]bool) []string {
	var issues []string
	for name, s := range sensors {
		if name == "" {
			issues = append(issues, "sensor name must not be empty")
			continue
		}
		prefix := fmt.Sprintf("sensor %q:", name)
		if s.Category != "" && !ValidSensorCategories[s.Category] {
			issues = append(issues, fmt.Sprintf("%s category %q must be one of maintainability, architecture, behaviour", prefix, s.Category))
		}
		if s.Role != "" && !ValidSensorRoles[s.Role] {
			issues = append(issues, fmt.Sprintf("%s role %q must be one of regular, convergence-verifier, stuck-recovery", prefix, s.Role))
		}
		// A convergence verifier decides that a run is finished, so it must be
		// able to produce a verdict. No verdict is mechanically derivable from
		// a file glob: such a sensor would end the run because a path exists,
		// with contents never read — and the path sits inside the agent's own
		// write path, so the run could manufacture its own convergence.
		//
		// The loop refuses this at runtime because a files sensor is
		// StatusReported and never StatusPass. Refusing it here as well means
		// the author is told at `ynd validate` time rather than discovering it
		// as a run that silently never converges.
		if s.Ratchet != "" && !ValidSensorRatchets[s.Ratchet] {
			issues = append(issues, fmt.Sprintf("%s ratchet %q must be one of fingerprint, count", prefix, s.Ratchet))
		}
		if s.Ratchet == "count" && s.Source.Kind() != "" && s.Source.Kind() != "command" {
			issues = append(issues, fmt.Sprintf(
				"%s ratchet count requires a command source: only a command sensor produces countable findings", prefix))
		}
		if s.Reference != nil {
			if s.Reference.Path == "" {
				issues = append(issues, fmt.Sprintf("%s reference.path must be non-empty", prefix))
			}
			if strings.HasPrefix(s.Reference.Path, "/") || strings.Contains(s.Reference.Path, "..") {
				issues = append(issues, fmt.Sprintf(
					"%s reference.path %q must be a relative path inside the harness", prefix, s.Reference.Path))
			}
			if !ValidSensorExpectations[s.Reference.Expect] {
				issues = append(issues, fmt.Sprintf(
					"%s reference.expect %q must be one of fail, pass", prefix, s.Reference.Expect))
			}
			// Calibration compares an exit code against a declared
			// expectation, and only a command source produces one.
			//
			// Not the same rule that stops a files sensor converging a run,
			// though the two are easy to conflate. A files sensor does now
			// produce a verdict — freshness — and it does gate on it. It
			// still cannot be calibrated (no exit code) and still cannot
			// converge (freshness says whether an artifact is current, never
			// whether the work is done). Three separate limits, three reasons.
			if k := s.Source.Kind(); k != "" && k != "command" {
				issues = append(issues, fmt.Sprintf(
					"%s reference requires a command source: no verdict about a %s sensor's output is derivable", prefix, k))
			}
		}
		if s.Role == "convergence-verifier" && s.Source.Kind() == "files" {
			issues = append(issues, fmt.Sprintf(
				"%s role convergence-verifier requires a command source: a files sensor's freshness says whether its artifact is current, never whether the work is done", prefix))
		}
		if s.Tolerance != "" && !ValidSensorTolerances[s.Tolerance] {
			issues = append(issues, fmt.Sprintf("%s tolerance %q must be one of blocking, advisory, report", prefix, s.Tolerance))
		}
		if s.Source.Kind() == "" {
			issues = append(issues, fmt.Sprintf("%s source must have exactly one of files, command, focus, github_status, github_check", prefix))
		} else {
			switch s.Source.Kind() {
			case "github_status":
				g := s.Source.GitHubStatus
				if g.Context == "" {
					issues = append(issues, fmt.Sprintf("%s source.github_status.context is required: a sensor that selects no particular status observes a set that changes underneath it", prefix))
				}
				if g.Require != "" && !ValidGitHubStatusStates[g.Require] {
					issues = append(issues, fmt.Sprintf("%s source.github_status.require %q must be one of success, pending, failure, error", prefix, g.Require))
				}
				issues = append(issues, validateGitHubCommon(prefix+" source.github_status", g.Repo, g.OnMissing)...)
			case "github_check":
				g := s.Source.GitHubCheck
				if g.Name == "" {
					issues = append(issues, fmt.Sprintf("%s source.github_check.name is required: a sensor that selects no particular check run observes a set that changes underneath it", prefix))
				}
				if g.Require != "" && !ValidGitHubCheckConclusions[g.Require] {
					issues = append(issues, fmt.Sprintf("%s source.github_check.require %q must be a check conclusion (success, failure, neutral, cancelled, timed_out, action_required, stale, skipped). A run that has not finished has no conclusion — use on_missing for that case", prefix, g.Require))
				}
				issues = append(issues, validateGitHubCommon(prefix+" source.github_check", g.Repo, g.OnMissing)...)
			case "files":
				if len(s.Source.Files) == 0 {
					issues = append(issues, fmt.Sprintf("%s source.files must be a non-empty array", prefix))
				}
				for i, p := range s.Source.Files {
					if p == "" {
						issues = append(issues, fmt.Sprintf("%s source.files[%d] must be non-empty", prefix, i))
					}
				}
			case "command":
				if s.Source.Command == "" {
					issues = append(issues, fmt.Sprintf("%s source.command must be a non-empty string", prefix))
				}
			case "focus":
				if s.Source.Focus.Inline != nil {
					if s.Source.Focus.Inline.Prompt == "" {
						issues = append(issues, fmt.Sprintf("%s source.focus.prompt must not be empty", prefix))
					}
					if p := s.Source.Focus.Inline.Profile; p != "" && profileNames != nil && !profileNames[p] {
						issues = append(issues, fmt.Sprintf("%s source.focus references unknown profile %q", prefix, p))
					}
				} else if s.Source.Focus.Name != "" && focusNames != nil && !focusNames[s.Source.Focus.Name] {
					issues = append(issues, fmt.Sprintf("%s source.focus references undefined focus %q", prefix, s.Source.Focus.Name))
				}
			}
		}
		if s.Output.Format == "" {
			issues = append(issues, fmt.Sprintf("%s output.format must not be empty", prefix))
		}
		// A match that does not compile would silently select nothing, which
		// turns the sensor's whole output into accepted debt on the next
		// baseline write. Caught here instead.
		if _, err := s.OutputMatcher(); err != nil {
			issues = append(issues, fmt.Sprintf("%s %v", prefix, err))
		}
	}
	return issues
}

// Focus is a named combination of profile + prompt for repeatable AI execution.
type Focus struct {
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt"`
}

// Profile is a named configuration variant. When selected, its fields
// are merged with top-level values:
//   - `hooks`: per-event replace. If the profile declares an event,
//     it replaces the default; absent events are inherited.
//   - `mcp_servers`: deep merge (profile keys win on collision;
//     absent keys inherited; nil pointer removes an inherited server).
//   - `includes`: appended to the harness's base includes. Profile-only
//     includes let a single harness carry multiple artifact sets and
//     swap them based on the active profile.
type Profile struct {
	Hooks      map[string][]HookEntry `json:"hooks,omitempty"`
	MCPServers map[string]*MCPServer  `json:"mcp_servers,omitempty"`
	Includes   []IncludeMeta          `json:"includes,omitempty"`
	// EnvPassthrough replaces the harness-level list when the profile is
	// selected, so a posture can widen or narrow what the agent sees without
	// editing the base manifest. Replacement rather than union: a profile
	// meant to restrict must be able to.
	EnvPassthrough []string `json:"env_passthrough,omitempty"`
}

// AgentConfig holds harness-level defaults for `ynh agent run`. Flags win over
// these; these win over the built-in defaults.
type AgentConfig struct {
	MaxTurns  int    `json:"max_turns,omitempty"`
	MaxTokens int64  `json:"max_tokens,omitempty"`
	MaxWall   string `json:"max_wall,omitempty"` // Go duration, e.g. "45m"
}

// AuthorInfo holds harness author information.
type AuthorInfo struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// MCPServer defines an MCP server dependency.
type MCPServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// ValidateMCPServers checks that each MCP server has either Command or URL (not both, not neither).
func ValidateMCPServers(servers map[string]MCPServer) []string {
	var issues []string
	for name, server := range servers {
		hasCommand := server.Command != ""
		hasURL := server.URL != ""
		if !hasCommand && !hasURL {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s: must have either command or url", name))
		}
		if hasCommand && hasURL {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s: must have command or url, not both", name))
		}
	}
	return issues
}

// HookEntry defines a single hook action.
type HookEntry struct {
	Matcher string `json:"matcher,omitempty"` // tool name pattern (optional)
	Command string `json:"command"`           // shell command to run
}

// ValidHookEvents lists the canonical hook event names.
var ValidHookEvents = map[string]bool{
	"before_tool":      true,
	"after_tool":       true,
	"before_prompt":    true,
	"on_stop":          true,
	"on_session_start": true,
}

// ValidateHooks checks that hook event names are valid and commands are non-empty.
func ValidateHooks(hooks map[string][]HookEntry) []string {
	var issues []string
	for event, entries := range hooks {
		if !ValidHookEvents[event] {
			issues = append(issues, fmt.Sprintf("unknown hook event %q (valid: before_tool, after_tool, before_prompt, on_stop, on_session_start)", event))
		}
		for i, entry := range entries {
			if entry.Command == "" {
				issues = append(issues, fmt.Sprintf("hooks.%s[%d]: command must not be empty", event, i))
			}
		}
	}
	return issues
}

// ValidateProfiles validates hooks and mcp_servers within each profile.
// Nil MCPServer entries (JSON null) are skipped — they signal removal of
// an inherited server during profile merge.
func ValidateProfiles(profiles map[string]Profile) []string {
	var issues []string
	for name, profile := range profiles {
		for _, issue := range ValidateHooks(profile.Hooks) {
			issues = append(issues, fmt.Sprintf("profile %q: %s", name, issue))
		}
		// Filter out nil entries (null removals) before validating
		servers := make(map[string]MCPServer)
		for k, v := range profile.MCPServers {
			if v != nil {
				servers[k] = *v
			}
		}
		for _, issue := range ValidateMCPServers(servers) {
			issues = append(issues, fmt.Sprintf("profile %q: %s", name, issue))
		}
	}
	return issues
}

// ValidateFocus checks that each focus entry has a non-empty prompt.
func ValidateFocus(focuses map[string]Focus) []string {
	var issues []string
	for name, f := range focuses {
		if f.Prompt == "" {
			issues = append(issues, fmt.Sprintf("focus.%s: prompt must not be empty", name))
		}
	}
	return issues
}

// ProvenanceMeta records where a harness was installed from.
type ProvenanceMeta struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Path         string `json:"path,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	InstalledAt  string `json:"installed_at"`
}

// IncludeMeta is the JSON representation of an include source. Exactly one
// of `git` (remote) or `local` (path-based) must be set. For both forms
// `path` scopes into a subdirectory of the source and `pick` filters paths.
// `ref` is Git-only.
type IncludeMeta struct {
	Git   string   `json:"git,omitempty"`
	Local string   `json:"local,omitempty"`
	Ref   string   `json:"ref,omitempty"`
	Path  string   `json:"path,omitempty"`
	Pick  []string `json:"pick,omitempty"`
}

// DelegateMeta is the JSON representation of a delegate reference.
type DelegateMeta struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

// HarnessFile is the manifest filename used in harness directories.
const HarnessFile = ".harness.json"

// PluginDir is the manifest directory for the 0.2+ format.
const PluginDir = ".ynh-plugin"

// PluginFile is the manifest filename inside PluginDir.
const PluginFile = "plugin.json"

// InstalledFile holds install-time provenance inside PluginDir.
// Authors never write this file — ynh install writes it at install time.
const InstalledFile = "installed.json"

// InstalledJSON records where a harness was installed from.
// It lives at .ynh-plugin/installed.json, separate from the author-controlled plugin.json.
type InstalledJSON struct {
	SourceType   string               `json:"source_type"`
	Source       string               `json:"source"`
	Ref          string               `json:"ref,omitempty"`
	SHA          string               `json:"sha,omitempty"`
	Path         string               `json:"path,omitempty"`
	Namespace    string               `json:"namespace,omitempty"`
	RegistryName string               `json:"registry_name,omitempty"`
	InstalledAt  string               `json:"installed_at"`
	ForkedFrom   *ForkedFromJSON      `json:"forked_from,omitempty"`
	Resolved     []ResolvedSourceJSON `json:"resolved,omitempty"`
}

// ResolvedSourceJSON records the resolved commit SHA for an include or
// delegate at install/update time. Identity is the (Git, Ref, Path) triple
// matching the manifest entry. Floating-ref entries (Ref == "" or branch
// name) get their SHA filled here so consumers can compare against
// ls-remote without re-resolving.
type ResolvedSourceJSON struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
	SHA  string `json:"sha"`
}

// ForkedFromJSON records the upstream a local harness was forked from.
// Populated by `ynh fork`; absent on installs that were not forked.
type ForkedFromJSON struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SHA          string `json:"sha,omitempty"`
	Path         string `json:"path,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	Version      string `json:"version,omitempty"`
}

// IsHarnessDir returns true if the directory contains a .harness.json manifest.
func IsHarnessDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, HarnessFile))
	return err == nil
}

// IsPluginDir returns true if the directory contains a .ynh-plugin/plugin.json manifest.
func IsPluginDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, PluginDir, PluginFile))
	return err == nil
}

// LoadPluginJSON reads and parses .ynh-plugin/plugin.json from dir.
// Unknown fields are rejected. The migration chain must run before this
// so callers can assume the new format exists.
func LoadPluginJSON(dir string) (*HarnessJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, PluginDir, PluginFile))
	if err != nil {
		return nil, fmt.Errorf("reading plugin.json: %w", err)
	}

	var hj HarnessJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&hj); err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}

	if hj.Name == "" {
		return nil, fmt.Errorf("plugin.json missing required field: name")
	}

	return &hj, nil
}

// MCPJSONFile is the vendor-convention (Claude Code) filename for MCP server
// declarations at the plugin root, as an alternative to mcp_servers in
// plugin.json.
const MCPJSONFile = ".mcp.json"

// LoadMCPJSON reads a root .mcp.json (Claude Code convention:
// {"mcpServers": {...}}) from dir, for plugins that declare MCP servers that
// way instead of via mcp_servers in plugin.json. Returns nil, nil if the
// file does not exist.
func LoadMCPJSON(dir string) (map[string]MCPServer, error) {
	data, err := os.ReadFile(filepath.Join(dir, MCPJSONFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", MCPJSONFile, err)
	}

	var doc struct {
		MCPServers map[string]MCPServer `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", MCPJSONFile, err)
	}

	return doc.MCPServers, nil
}

// SavePluginJSON writes hj to .ynh-plugin/plugin.json in dir.
// InstalledFrom is stripped — provenance belongs in installed.json.
func SavePluginJSON(dir string, hj *HarnessJSON) error {
	if err := os.MkdirAll(filepath.Join(dir, PluginDir), 0o755); err != nil {
		return fmt.Errorf("creating .ynh-plugin dir: %w", err)
	}

	clean := *hj
	clean.InstalledFrom = nil

	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling plugin.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, PluginDir, PluginFile), data, 0o644); err != nil {
		return fmt.Errorf("writing plugin.json: %w", err)
	}

	return nil
}

// LoadInstalledJSON reads .ynh-plugin/installed.json from dir.
func LoadInstalledJSON(dir string) (*InstalledJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, PluginDir, InstalledFile))
	if err != nil {
		return nil, fmt.Errorf("reading installed.json: %w", err)
	}

	var ins InstalledJSON
	if err := json.Unmarshal(data, &ins); err != nil {
		return nil, fmt.Errorf("invalid installed.json: %w", err)
	}

	return &ins, nil
}

// SaveInstalledJSON writes ins to .ynh-plugin/installed.json in dir.
func SaveInstalledJSON(dir string, ins *InstalledJSON) error {
	if err := os.MkdirAll(filepath.Join(dir, PluginDir), 0o755); err != nil {
		return fmt.Errorf("creating .ynh-plugin dir: %w", err)
	}

	data, err := json.MarshalIndent(ins, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling installed.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, PluginDir, InstalledFile), data, 0o644); err != nil {
		return fmt.Errorf("writing installed.json: %w", err)
	}

	return nil
}

// IsLegacyPluginDir returns true if the directory contains a legacy .claude-plugin/plugin.json.
func IsLegacyPluginDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil
}

// LoadHarnessJSON reads and parses harness.json from dir.
// Unknown fields are rejected via DisallowUnknownFields.
func LoadHarnessJSON(dir string) (*HarnessJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, HarnessFile))
	if err != nil {
		return nil, fmt.Errorf("reading .harness.json: %w", err)
	}

	var hj HarnessJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&hj); err != nil {
		return nil, fmt.Errorf("invalid .harness.json: %w", err)
	}

	if hj.Name == "" {
		return nil, fmt.Errorf(".harness.json missing required field: name")
	}

	return &hj, nil
}

// LoadHarnessFile reads and parses a .harness.json from a file path directly.
// Unlike LoadHarnessJSON, the name field is not required (for inline config).
func LoadHarnessFile(path string) (*HarnessJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var hj HarnessJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&hj); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}

	return &hj, nil
}

// SaveHarnessJSON writes a HarnessJSON manifest to dir/.harness.json.
func SaveHarnessJSON(dir string, hj *HarnessJSON) error {
	data, err := json.MarshalIndent(hj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .harness.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, HarnessFile), data, 0o644); err != nil {
		return fmt.Errorf("writing .harness.json: %w", err)
	}

	return nil
}

// EnvPassthroughField is the manifest key declaring which environment
// variables a harness may see. It governs two things at once, deliberately:
// which variables an MCP server declaration may interpolate, and which reach
// an agent worker's process. One declaration, one place to review.
const EnvPassthroughField = "env_passthrough"

// envRef matches a ${VAR} reference. Bare $VAR is deliberately not supported —
// it collides with ordinary shell and path content in a manifest, and a
// credential mechanism should not depend on guessing intent.
var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandMCPEnv resolves ${VAR} references in every server's env values and
// headers, drawing only from allowed.
//
// Without this an MCP server's credentials have to be literal values in the
// manifest, which means committing them. With it the manifest declares which
// variable carries the secret and the secret itself never enters the repo.
//
// Two rules keep it from becoming a hole of its own:
//
//   - A reference to a variable outside the allowlist is an error, not an
//     empty string. Otherwise any manifest could name any variable in the
//     operator's environment and quietly copy it into a config file.
//   - A reference to an allowed-but-unset variable is also an error. Emitting
//     an empty credential produces a failure far from its cause, and a control
//     that silently degrades is the failure this whole area exists to avoid.
//
// UndeclaredMCPEnvRefs lists ${VAR} references in MCP env and headers that the
// harness does not declare in env_passthrough.
//
// It checks *declaration* only, never whether a variable is set. An unset
// variable is legitimately a run-time condition; an undeclared one never is.
// That is what makes this safe to run before assembly, where ExpandMCPEnv
// cannot: the same rule, minus the part that needs a live environment.
//
// It returns nothing when the harness declares no env_passthrough at all.
// Such a harness may be authored purely for distribution: `ynd export`
// deliberately leaves ${VAR} literal so the exporter's credentials are not
// baked into a shared bundle, and strips env_passthrough from the artifact
// entirely — it is a local-assembly allowlist and never reaches the consumer.
// Flagging that case would redden a harness that works, and demand an
// allowlist with no effect on the thing it ships.
//
// A harness that declares an allowlist and misses one entry is the realistic
// mistake, and that is what this catches.
func UndeclaredMCPEnvRefs(servers map[string]MCPServer, allowed []string) []string {
	if len(servers) == 0 || len(allowed) == 0 {
		return nil
	}
	allow := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allow[name] = true
	}

	var issues []string
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		s := servers[name]
		for _, field := range []struct {
			label string
			m     map[string]string
		}{{"env", s.Env}, {"headers", s.Headers}} {
			keys := make([]string, 0, len(field.m))
			for k := range field.m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				for _, match := range envRef.FindAllStringSubmatch(field.m[k], -1) {
					if v := match[1]; !allow[v] {
						issues = append(issues, fmt.Sprintf(
							"mcp server %q: %s.%s references ${%s}, which is not in %s",
							name, field.label, k, v, EnvPassthroughField))
					}
				}
			}
		}
	}
	return issues
}

func ExpandMCPEnv(servers map[string]MCPServer, allowed []string, lookup func(string) (string, bool)) (map[string]MCPServer, error) {
	if len(servers) == 0 {
		return servers, nil
	}
	allow := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allow[name] = true
	}

	out := make(map[string]MCPServer, len(servers))
	for name, s := range servers {
		expandMap := func(field string, in map[string]string) (map[string]string, error) {
			if len(in) == 0 {
				return in, nil
			}
			res := make(map[string]string, len(in))
			for k, v := range in {
				var bad error
				res[k] = envRef.ReplaceAllStringFunc(v, func(match string) string {
					varName := envRef.FindStringSubmatch(match)[1]
					if !allow[varName] {
						bad = fmt.Errorf("mcp server %q: %s.%s references ${%s}, which is not in %s",
							name, field, k, varName, EnvPassthroughField)
						return ""
					}
					val, ok := lookup(varName)
					if !ok {
						bad = fmt.Errorf("mcp server %q: %s.%s references ${%s}, which is not set",
							name, field, k, varName)
						return ""
					}
					return val
				})
				if bad != nil {
					return nil, bad
				}
			}
			return res, nil
		}

		env, err := expandMap("env", s.Env)
		if err != nil {
			return nil, err
		}
		headers, err := expandMap("headers", s.Headers)
		if err != nil {
			return nil, err
		}
		s.Env = env
		s.Headers = headers
		out[name] = s
	}
	return out, nil
}

// validateGitHubCommon checks the fields both GitHub sensor sources share.
func validateGitHubCommon(prefix, repo, onMissing string) []string {
	var issues []string
	if repo != "" {
		// "owner/name", nothing else. A bare name or a URL both silently
		// address the wrong repository, which a sensor must never do.
		parts := strings.Split(repo, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			issues = append(issues, fmt.Sprintf("%s.repo %q must be \"owner/name\"", prefix, repo))
		}
	}
	if onMissing != "" && !ValidSensorOnMissing[onMissing] {
		issues = append(issues, fmt.Sprintf("%s.on_missing %q must be one of broken, fail, pass", prefix, onMissing))
	}
	return issues
}
