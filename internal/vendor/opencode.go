package vendor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/eyelock/ynh/internal/plugin"
)

func init() {
	Register(&OpenCode{})
}

// opencodeCLI is OpenCode's binary: see CLIName in adapter.go.
const opencodeCLI = "opencode"

// opencodeConfigDirEnv names the directory OpenCode treats as an extra config
// root: its opencode.json, AGENTS.md, agents/, skills/ and plugins/ all load.
const opencodeConfigDirEnv = "OPENCODE_CONFIG_DIR"

// opencodeSupportedMajor is the only OpenCode major version ynh drives. 1.x
// has a different launch model, config schema and plugin API (the
// vendor-adapters skill, references/opencode.md), so it is refused rather
// than half-supported.
const opencodeSupportedMajor = 2

// opencodeExec replaces the process. A variable so tests can observe the
// launch instead of becoming OpenCode.
var opencodeExec = syscall.Exec

// opencodeVersionTimeout bounds `opencode --version`, which a wedged install
// could otherwise hang forever. A variable so a test can shorten it.
var opencodeVersionTimeout = 10 * time.Second

// opencodeVersionProbe runs `<bin> --version` and returns its output. A
// variable so tests never run the real binary.
var opencodeVersionProbe = func(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opencodeVersionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").Output()
	return string(out), err
}

// OpenCode implements the Adapter interface for OpenCode 2.x
// (github.com/anomalyco/opencode). Every claim below was hand-tested against
// v2.0.27 with the free opencode/nemotron-3-ultra-free model; see the
// vendor-adapters skill, references/opencode.md.
//
// Launch strategy: native bundle loading by environment variable.
// OPENCODE_CONFIG_DIR points at <run dir>/.opencode, and syscall.Exec gives a
// clean process replacement with no symlinks. CONFIRMED: 2.x runs commands
// through a shared background service that ignores the client's environment,
// so the variable has no effect unless the invocation is `--standalone`, which
// starts a private server that does honour it. --standalone is therefore on
// every launch.
//
// CONFIRMED gaps from Claude's model, each worked around below:
//   - the `instructions` config key is accepted and ignored; only AGENTS.md at
//     the root of the config dir carries instructions (see composeOpencodeAgents)
//   - rules are not loaded at all, so they are folded into that AGENTS.md
//   - hooks are not declarative; they are a generated TypeScript plugin
//     (see GenerateHookConfig)
type OpenCode struct{}

func (o *OpenCode) Name() string        { return "opencode" }
func (o *OpenCode) DisplayName() string { return "OpenCode" }
func (o *OpenCode) CLIName() string     { return opencodeCLI }

func (o *OpenCode) ConfigDir() string { return ".opencode" }

func (o *OpenCode) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (o *OpenCode) InstructionsFile() string { return "AGENTS.md" }

func (o *OpenCode) NeedsSymlinks() bool { return false }

func (o *OpenCode) Install(stagingDir string, projectDir string) ([]SymlinkEntry, error) {
	return nil, nil
}

func (o *OpenCode) Clean(entries []SymlinkEntry) error { return nil }

// GenerateSystemPrompt emits the cross-vendor AGENTS.md only. OpenCode reads
// the AGENTS.md at the root of its config dir (CONFIRMED), but that file is
// composed at launch from this one plus the rules and runtime instructions
// (composeOpencodeAgents), so a second copy here would be overwritten at once
// and would leak into every export, where the exporter writes these files for
// a client outside the Agent Plugins format and OpenCode reads nothing from a
// package.
func (o *OpenCode) GenerateSystemPrompt(content []byte) map[string][]byte {
	return map[string][]byte{"AGENTS.md": content}
}

// ApplyRuntimeInstructions appends per-invocation text to
// .opencode/ynh-runtime.md. OpenCode has no system-prompt flag and a file it
// reads is overwritten at launch, so the text lands in a file of its own that
// composeOpencodeAgents folds into AGENTS.md. Returns no args.
func (o *OpenCode) ApplyRuntimeInstructions(runDir, text string) ([]string, error) {
	dir := filepath.Join(runDir, o.ConfigDir())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", o.ConfigDir(), err)
	}
	f, err := os.OpenFile(filepath.Join(dir, opencodeRuntimeFile), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", opencodeRuntimeFile, err)
	}
	if _, err := fmt.Fprintf(f, "\n\n%s\n", text); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("writing runtime instructions: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("closing %s: %w", opencodeRuntimeFile, err)
	}
	return nil, nil
}

// opencodeToolActions maps Claude tool names to the OpenCode permission
// action that governs them. Actions, not tool ids: `edit` covers edit, write
// and patch, and `subagent` is Claude's Task.
var opencodeToolActions = map[string]string{
	"Read": "read", "Write": "edit", "Edit": "edit", "MultiEdit": "edit",
	"NotebookEdit": "edit", "Bash": "shell", "Glob": "glob", "Grep": "grep",
	"WebFetch": "webfetch", "WebSearch": "websearch", "Task": "subagent",
	"Agent": "subagent", "Skill": "skill",
}

// opencodeKnownActions are the actions a Claude tool list can name, in a fixed
// order. An allow-list that must be appended to existing permissions is
// expressed as a deny for each of these it does not mention.
var opencodeKnownActions = []string{"read", "edit", "shell", "glob", "grep", "webfetch", "websearch", "subagent", "skill"}

// opencodeRule is one permission rule.
type opencodeRule struct{ action, resource, effect string }

func (r opencodeRule) yaml(indent string) string {
	action := r.action
	if action == "*" {
		action = strconv.Quote(action)
	}
	return indent + "- action: " + action + "\n" +
		indent + "  resource: " + strconv.Quote(r.resource) + "\n" +
		indent + "  effect: " + r.effect + "\n"
}

// splitToolSpec splits "Bash(git status:*)" into "Bash" and "git status:*".
// A bare name has an empty scope and scoped is false.
func splitToolSpec(spec string) (name, scope string, scoped bool) {
	i := strings.Index(spec, "(")
	if i < 0 || !strings.HasSuffix(spec, ")") {
		return spec, "", false
	}
	return strings.TrimSpace(spec[:i]), spec[i+1 : len(spec)-1], true
}

// shellResource turns a Claude Bash scope into an OpenCode shell resource.
// Claude's prefix form "git status:*" is "git status *" here; OpenCode's
// trailing "*" also matches the command without arguments (CONFIRMED).
func shellResource(scope string) string {
	if p, ok := strings.CutSuffix(scope, ":*"); ok {
		return p + " *"
	}
	return scope
}

// opencodeMCPAction maps an MCP tool "mcp__server__tool" to its OpenCode
// action "server_tool", the id OpenCode gives a tool Code Mode calls.
func opencodeMCPAction(name string) (string, bool) {
	if !strings.HasPrefix(name, "mcp__") || strings.Count(name, "__") < 2 {
		return "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2)
	return parts[0] + "_" + parts[1], true
}

// opencodeAllows maps an allow-list to allow rules. Only Bash has a scope that
// carries over (a command pattern); the path and URL scopes of the other tools
// use Claude's own pattern syntax, which OpenCode's whole-value globs do not
// reproduce, so a scoped Read(...) gets no allow and stays denied. An MCP tool
// is allowed by the same action opencodeDenies denies it by, so a granted MCP
// tool is not denied by the deny-everything rule. Any other unmapped name gets
// no allow.
func opencodeAllows(tools []string) []opencodeRule {
	var rules []opencodeRule
	seen := map[opencodeRule]bool{}
	for _, t := range tools {
		name, scope, scoped := splitToolSpec(t)
		action, ok := opencodeToolActions[name]
		if !ok && !scoped {
			action, ok = opencodeMCPAction(name)
		}
		if !ok {
			continue
		}
		r := opencodeRule{action, "*", "allow"}
		if scoped {
			if action != "shell" || scope == "" {
				continue
			}
			r.resource = shellResource(scope)
		}
		if !seen[r] {
			seen[r] = true
			rules = append(rules, r)
		}
	}
	return rules
}

// opencodeDenies maps a disallowed list to deny rules, which cannot be left
// out without widening access. A scoped Bash(...) keeps its command pattern;
// any other scoped form is denied outright (over-restricting is safe). An MCP
// tool "mcp__server__tool" is the action "server_tool". Unmapped names are
// skipped, as there is no action to name.
func opencodeDenies(tools []string) []opencodeRule {
	var rules []opencodeRule
	for _, t := range tools {
		name, scope, scoped := splitToolSpec(t)
		r := opencodeRule{resource: "*", effect: "deny"}
		switch action, ok := opencodeToolActions[name]; {
		case ok:
			r.action = action
			if scoped && action == "shell" && scope != "" {
				r.resource = shellResource(scope)
			}
		default:
			mcp, isMCP := opencodeMCPAction(name)
			if !isMCP {
				continue
			}
			r.action = mcp
		}
		rules = append(rules, r)
	}
	return rules
}

// claudeToolList reads a `tools:` or `disallowedTools:` entry: a YAML list, or
// Claude's comma-separated string. Commas inside parentheses belong to a scope
// ("Bash(git add -A, x)") and do not split.
func claudeToolList(e frontmatterEntry) []string {
	items := e.list()
	if len(items) == 1 && !strings.HasPrefix(e.value, "[") {
		items = splitOutsideParens(items[0])
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it = strings.TrimSpace(it); it != "" {
			out = append(out, it)
		}
	}
	return out
}

func splitOutsideParens(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// opencodePermissions renders the rules a Claude agent's tool lists translate
// to, or "" when it has none. Dropping a list would widen access, so:
//
//   - no existing permissions: a non-empty allow-list is deny-everything, then
//     the allows; disallowed tools are denies after them (CONFIRMED: rules
//     are ordered and the last match wins, over a base policy that allows
//     everything). Without an allow-list only the denies are written.
//   - existing permissions are kept as the author wrote them and only denies
//     are appended: the disallowed tools, plus, for an allow-list, a deny for
//     every known action it does not allow. That never widens, but it cannot
//     deny a tool this adapter has no action for.
//
// The caller has already refused an inline (flow) permissions value that
// could not be appended to, and turned an empty "[]" into no permissions.
func opencodePermissions(tools, disallowed []string, existing *frontmatterEntry) string {
	var rules []opencodeRule
	allows := opencodeAllows(tools)
	switch {
	case existing == nil && len(tools) > 0:
		rules = append(rules, opencodeRule{"*", "*", "deny"})
		rules = append(rules, allows...)
	case existing != nil && len(tools) > 0:
		allowed := map[string]bool{}
		for _, r := range allows {
			allowed[r.action] = true
		}
		for _, a := range opencodeKnownActions {
			if !allowed[a] {
				rules = append(rules, opencodeRule{a, "*", "deny"})
			}
		}
	}
	rules = append(rules, opencodeDenies(disallowed)...)
	if len(rules) == 0 {
		return ""
	}
	indent := "  "
	var b strings.Builder
	if existing == nil {
		b.WriteString("permissions:\n")
	} else {
		// Match the indentation of the author's block list.
		for _, l := range existing.cont {
			if t := strings.TrimLeft(l, " \t"); strings.HasPrefix(t, "-") {
				indent = l[:len(l)-len(t)]
				break
			}
		}
	}
	for _, r := range rules {
		b.WriteString(r.yaml(indent))
	}
	return b.String()
}

// TransformArtifact rewrites an agent's frontmatter so OpenCode loads it.
// CONFIRMED by bisecting `opencode run --agent <name>` against v2.0.27: an
// agent file is "Agent not found" when it has no description, or has Claude's
// comma-string `tools:`, a bare-alias `model: sonnet` (OpenCode wants
// provider/model) or `color: blue`; unknown keys such as `name:` or
// `allowed-tools:` are harmless. A whitelist is kept rather than a blacklist
// of those three, because the next Claude-only key would silently drop an
// agent. A missing description is humanised from the file name.
//
// Claude's `tools:` and `disallowedTools:` (or `disallowed-tools:`) are
// translated to `permissions` by opencodePermissions, never dropped: dropping
// an allow-list would hand the agent every tool. Absent or empty lists mean
// all tools on Claude and add nothing here. Other artifact types pass through.
//
// An agent whose `permissions:` is written inline ("permissions: [...]") and
// that also has a tool list is refused: the rules the lists translate to
// cannot be appended to an inline value, so the agent would run without its
// tool restrictions. An empty "permissions: []" is no permissions at all.
func (o *OpenCode) TransformArtifact(artifactType, name string, data []byte) (string, []byte, error) {
	if artifactType != "agents" || !strings.HasSuffix(name, ".md") {
		return name, data, nil
	}
	var entries []frontmatterEntry
	body := data
	if lines, rest, ok := splitFrontmatter(data); ok {
		entries, body = frontmatterEntries(lines), rest
	}
	var b strings.Builder
	b.WriteString("---\n")
	hasDescription := false
	var tools, disallowed []string
	var existing *frontmatterEntry
	for i, e := range entries {
		keep := false
		switch e.key {
		case "description":
			if e.scalar() != "" {
				keep, hasDescription = true, true
			}
		case "mode":
			keep = true
		case "permissions":
			existing = &entries[i]
		case "model":
			keep = strings.Contains(e.scalar(), "/")
		case "tools":
			tools = claudeToolList(e)
		case "disallowedTools", "disallowed-tools":
			disallowed = append(disallowed, claudeToolList(e)...)
		}
		if keep {
			for _, l := range e.raw {
				b.WriteString(l + "\n")
			}
		}
	}
	if existing != nil && existing.value != "" {
		switch {
		case strings.Join(strings.Fields(existing.value), "") == "[]":
			existing = nil
		case len(tools) > 0 || len(disallowed) > 0:
			return name, nil, errOpencodeInlinePermissions
		}
	}
	if !hasDescription {
		b.WriteString("description: " + strconv.Quote(humanizeRuleName(strings.TrimSuffix(name, ".md"))) + "\n")
	}
	if existing != nil {
		for _, l := range existing.raw {
			b.WriteString(l + "\n")
		}
	}
	// With existing permissions this lands directly after the author's rules.
	b.WriteString(opencodePermissions(tools, disallowed, existing))
	b.WriteString("---\n")
	b.Write(bytes.TrimLeft(body, "\r\n"))
	return name, []byte(b.String()), nil
}

// errOpencodeInlinePermissions refuses an agent whose inline permissions
// would leave its tools: / disallowedTools unenforced on OpenCode.
var errOpencodeInlinePermissions = errors.New("opencode cannot enforce this agent's tools: / disallowedTools: " +
	"its permissions: is written inline (permissions: [...]), and the rules those lists translate to " +
	"cannot be appended to an inline value, so the agent would run with every tool. " +
	"Write permissions: as a block list (one \"- action: ...\" item per line), or remove it")

// opencodeRuntimeFile holds the text ApplyRuntimeInstructions appends.
const opencodeRuntimeFile = "ynh-runtime.md"

// composeOpencodeAgents writes <configPath>/.opencode/AGENTS.md as the run
// dir's AGENTS.md, then each .opencode/rules/*.md (sorted, frontmatter
// stripped), then .opencode/ynh-runtime.md. OpenCode reads only that one file
// (CONFIRMED: no rules concept, and the `instructions` key is ignored), so
// this is the only way rules and runtime text reach the model.
//
// Idempotent: the sources are never modified, only the composed output is
// rewritten, so repeated launches do not append twice. Rules are always-on;
// any path scoping in their frontmatter is dropped with the frontmatter.
// Writes nothing when there is nothing to compose.
func composeOpencodeAgents(configPath string) error {
	var parts []string
	if base, err := os.ReadFile(filepath.Join(configPath, "AGENTS.md")); err == nil {
		if s := strings.TrimSpace(string(base)); s != "" {
			parts = append(parts, s)
		}
	}

	rulesDir := filepath.Join(configPath, ".opencode", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		var names []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			data, err := os.ReadFile(filepath.Join(rulesDir, n))
			if err != nil {
				return fmt.Errorf("reading rule %s: %w", n, err)
			}
			if _, body, ok := splitFrontmatter(data); ok {
				data = body
			}
			if s := strings.TrimSpace(string(data)); s != "" {
				parts = append(parts, s)
			}
		}
	}

	if rt, err := os.ReadFile(filepath.Join(configPath, ".opencode", opencodeRuntimeFile)); err == nil {
		if s := strings.TrimSpace(string(rt)); s != "" {
			parts = append(parts, s)
		}
	}

	if len(parts) == 0 {
		return nil
	}
	out := filepath.Join(configPath, ".opencode", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(out), err)
	}
	if err := os.WriteFile(out, []byte(strings.Join(parts, "\n\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing composed AGENTS.md: %w", err)
	}
	return nil
}

func (o *OpenCode) LaunchInteractive(configPath string, extraArgs []string) error {
	return launchOpencode(configPath, extraArgs)
}

// LaunchNonInteractive runs `opencode run --standalone <extra> -- <prompt>`.
// CONFIRMED: `run` takes the message positionally and exits when the turn
// finishes, and after `--` a prompt starting with a dash is sent as the
// message, not read as a flag.
func (o *OpenCode) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	args := append([]string{"run", "--standalone"}, extraArgs...)
	return launchOpencodeArgs(configPath, append(args, "--", prompt))
}

// LaunchWithInitialPrompt passes the prompt with --prompt, which pre-fills the
// first message of an interactive session.
func (o *OpenCode) LaunchWithInitialPrompt(configPath string, prompt string, extraArgs []string) error {
	return launchOpencode(configPath, append([]string{"--prompt", prompt}, extraArgs...))
}

func (o *OpenCode) SupportsInitialPrompt() bool { return true }

func (o *OpenCode) SupportsResume() bool { return true }

// ResolveLastSession cannot resolve an id: OpenCode's session store is SQLite
// (~/.local/share/opencode/opencode.db) and ynh takes no driver, the same
// reason Codex gives. Callers resume with an empty id, which LaunchResume turns
// into -c.
func (o *OpenCode) ResolveLastSession(cwd string, notBefore time.Time) (string, error) {
	return "", ErrSessionLookupUnavailable
}

// LaunchResume continues a session: -s <id> resumes that session and -c the
// last one, and neither opens a picker. A prompt is delivered both ways,
// CONFIRMED by hand against v2.2.3 (2026-10-30), with the model answering from
// the resumed session's earlier turns:
//   - one-shot: `opencode run --standalone -s <id> -- <prompt>` (and -c)
//   - interactive: `opencode --standalone -s <id> --prompt <prompt>`, which
//     submits the prompt into the resumed session
func (o *OpenCode) LaunchResume(configPath, sessionID, prompt string, interactive bool, extraArgs []string) error {
	return launchOpencodeArgs(configPath, opencodeResumeArgs(sessionID, prompt, interactive, extraArgs))
}

func opencodeResumeArgs(sessionID, prompt string, interactive bool, extraArgs []string) []string {
	resume := []string{"-c"}
	if sessionID != "" {
		resume = []string{"-s", sessionID}
	}
	if prompt != "" && !interactive {
		args := append([]string{"run", "--standalone"}, resume...)
		args = append(args, extraArgs...)
		return append(args, "--", prompt)
	}
	args := append([]string{"--standalone"}, resume...)
	if prompt != "" {
		args = append(args, "--prompt", prompt)
	}
	return append(args, extraArgs...)
}

// opencodeHookEventMap maps ynh's canonical events to what the generated
// plugin installs for each: a tool or session hook, or an event of the event
// stream. The plugin's HOOKS table is keyed by the canonical name.
var opencodeHookEventMap = map[string]string{
	"before_tool":      "execute.before",
	"after_tool":       "execute.after",
	"before_prompt":    "prompt",
	"on_stop":          "session.execution.succeeded/failed",
	"on_session_start": "session.created",
}

// opencodeUnsupportedHookEvents names the canonical events OpenCode cannot
// express, each with the reason.
var opencodeUnsupportedHookEvents = map[string]string{
	"on_session_end": "OpenCode 2 has no session-end hook and its event stream has no session-end event (sessions persist to be resumed; session.deleted and session.archived are not the end of a run)",
}

// opencodeMatcherEvents are the native hooks whose matcher the generated
// plugin honours: the tool hooks, tested against the tool. The session hook
// and the event-stream subscribers have no tool, so a matcher there is ignored.
var opencodeMatcherEvents = map[string]bool{"execute.before": true, "execute.after": true}

// Per-hook timeout, in seconds, carried as "timeout" in the generated
// plugin's HOOKS table, which runs the command with spawnSync and that many
// seconds (60 when none is set). The plugin enforces it, not OpenCode, so no
// event has a cap.
var opencodeHookTimeoutField = "timeout"
var opencodeHookTimeoutUnsupported = ""

var opencodeHookTimeoutCaps = map[string]int{}

// How on_session_start output reaches the model: it cannot. The hook runs
// from an event-stream subscriber (session.created), which has no way to add
// context, and the plugin discards its stdout.
var opencodeSessionStartContext = "unsupported:on_session_start runs from OpenCode's event stream, which cannot add model context, so its output is discarded"

// opencodeHookEntry is one hook as the generated plugin carries it.
type opencodeHookEntry struct {
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// GenerateHookConfig returns .opencode/plugins/ynh-hooks.ts, or nil when no
// hook maps to an event. OpenCode 2 has no declarative hooks (CONFIRMED), so
// ynh generates a plugin that runs the harness's commands with Claude-shaped
// JSON on stdin. Commands are embedded as a JSON literal produced by
// encoding/json, never concatenated into the source.
//
// Semantics, all CONFIRMED against v2.0.27 unless noted:
//   - before_tool: exit 2 throws, which blocks the call; the hook's stderr (or
//     stdout) is the tool error the model sees
//   - after_tool: runs after the call; its output cannot reject the result
//   - before_prompt: exit 2 throws; see the reference for what that does
//   - on_session_start, on_stop: event-stream subscribers (session.created,
//     session.execution.succeeded/failed), so they cannot block. on_stop fires
//     once per turn, as on Claude Code. A matcher is ignored on both.
//   - a "./" command is a script the harness ships; see SessionHookScriptDir
//   - only exit 2 blocks, as on Claude Code; the harness is cross-vendor, so a
//     hook that cannot run (spawn error, killed, timed out after its timeout,
//     60s by default) lets the
//     call proceed, but is reported on stderr with its command, as is any exit
//     other than 0 and 2
//   - a matcher is an anchored JavaScript regex, checked here at generate
//     time (see validateOpencodeMatcher); a bad one is an error naming it
//   - MCP tools: the model calls them inside Code Mode's "execute" tool, and
//     OpenCode fires the tool hooks for the wrapper AND for each nested tool,
//     the latter under the id "<server>_<tool>" (CONFIRMED: echo_ynh_ping). A
//     matcher written "mcp__server__tool" matches it (CONFIRMED to block the
//     call before the server sees it; re-CONFIRMED on v2.2.3). The block throws
//     inside the wrapper's code, so later tool calls in the same "execute" do
//     not run. A hook with no matcher skips the "execute" wrapper, so it runs
//     once per real tool, not twice; a matcher that names "execute" still sees
//     it. The payload's tool_name is the OpenCode id, not mcp__server__tool
func (o *OpenCode) GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	doc := map[string][]opencodeHookEntry{}
	for _, event := range slices.Sorted(maps.Keys(opencodeHookEventMap)) {
		for _, h := range hooks[event] {
			if event == "before_tool" || event == "after_tool" {
				if err := validateOpencodeMatcher(h.Matcher); err != nil {
					return nil, fmt.Errorf("%s hook %q: %w", event, h.Command, err)
				}
			}
			doc[event] = append(doc[event], opencodeHookEntry{
				Matcher: h.Matcher,
				Command: pluginRootCommand("YNH_HOOK_ROOT")(h.Command),
				Timeout: hookTimeoutValue("opencode", event, h.Timeout),
			})
		}
	}
	if len(doc) == 0 {
		return nil, nil
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling hooks: %w", err)
	}
	src := strings.Replace(opencodeHooksPlugin, "__HOOKS__", string(data), 1)
	return map[string][]byte{filepath.Join(".opencode", "plugins", "ynh-hooks.ts"): []byte(src)}, nil
}

// validateOpencodeMatcher rejects a tool matcher that is not a valid regular
// expression, at generate time. The plugin tests it as an anchored JavaScript
// regex (no flags). RE2 rejects what JavaScript adds (lookaround,
// backreferences), but it also accepts syntax JavaScript either rejects or,
// worse, reads differently without throwing. So a matcher must pass RE2 and
// also keep to the syntax both read the same way (see opencodeMatcherNotJS).
// The plugin also compiles every matcher once at setup, so one that throws
// there anyway is reported and matches nothing rather than blocking every
// tool.
func validateOpencodeMatcher(m string) error {
	if m == "" || m == "*" {
		return nil
	}
	if _, err := regexp.Compile("^(?:" + m + ")$"); err != nil {
		return fmt.Errorf("invalid matcher %q: %w", m, err)
	}
	if i, why := opencodeMatcherNotJS(m); i >= 0 {
		return fmt.Errorf("invalid matcher %q: at offset %d, %s", m, i, why)
	}
	return nil
}

// opencodeSameEscapes are the letters that, escaped, mean the same in RE2 and
// in a JavaScript regex without the u flag. Escaped punctuation is a literal in
// both. Every other escape RE2 accepts means something else in JavaScript,
// which reads an unknown escaped letter as the letter itself: \A, \z (text
// anchors), \Q...\E (quoting), \a (bell), \pL and \p{Greek} (Unicode
// classes), octal \01, and \x{41}, where JavaScript reads "x" and then the
// quantifier {41}.
const opencodeSameEscapes = "dDsSwWfnrtv"

// opencodeMatcherNotJS returns the offset of the first syntax in m that
// JavaScript rejects or reads differently from RE2, and why; or -1. m has
// already compiled as RE2, so it is well formed. It rejects:
//   - a "(?" group other than "(?:": inline flags such as (?i), and Go's
//     (?P<name>...), throw "Invalid group" in JavaScript;
//   - an escape outside opencodeSameEscapes and punctuation, except \b and \B
//     outside a class, and \x with two hex digits;
//   - a POSIX class such as [[:alpha:]]: JavaScript reads "[[:alph]" and then
//     a literal "]";
//   - a "]" first in a class, as in []a] or [^]a]: RE2 reads it as a literal,
//     JavaScript as the end of an empty class ([] matches nothing, [^] matches
//     anything).
func opencodeMatcherNotJS(m string) (int, string) {
	inClass := false
	for i := 0; i < len(m); i++ {
		switch c := m[i]; {
		case c == '\\':
			if i+1 >= len(m) {
				return -1, ""
			}
			e := m[i+1]
			switch {
			case !isASCIIAlnum(e),
				strings.IndexByte(opencodeSameEscapes, e) >= 0,
				!inClass && (e == 'b' || e == 'B'):
			case e == 'x' && i+3 < len(m) && isHex(m[i+2]) && isHex(m[i+3]):
				i += 2
			default:
				return i, fmt.Sprintf("the escape \\%c means something else in JavaScript", e)
			}
			i++ // the escaped byte
		case inClass:
			switch {
			case c == ']':
				inClass = false
			case c == '[' && i+1 < len(m) && m[i+1] == ':':
				return i, "a POSIX class such as [:alpha:] is not JavaScript; write the range, such as [a-zA-Z]"
			}
		case c == '[':
			inClass = true
			j := i + 1
			if j < len(m) && m[j] == '^' {
				j++
			}
			if j < len(m) && m[j] == ']' {
				return i, `a "]" first in a class ends an empty class in JavaScript; write \]`
			}
			i = j - 1
		case c == '(' && i+1 < len(m) && m[i+1] == '?':
			if i+2 >= len(m) || m[i+2] != ':' {
				return i, "the group is not JavaScript; only (?:...) is allowed"
			}
		}
	}
	return -1, ""
}

func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// SessionHookScriptDir puts a hook's "./" script in .opencode/, beside the
// plugins/ directory the generated plugin lives in. The plugin anchors the
// command to its own location (YNH_HOOK_ROOT), not to the working directory,
// which is the user's project: OpenCode is launched there, not in the run dir.
func (o *OpenCode) SessionHookScriptDir() string { return ".opencode" }

// opencodeHooksPlugin is the plugin source; __HOOKS__ is replaced by the JSON
// hook table. Module shape CONFIRMED: no npm install, the runtime provides
// "@opencode/plugin". Hook domains are singular (ctx.tool, ctx.session): the
// plural names in the docs' table are undefined.
//
// Tool ids are OpenCode's (shell, read, write, edit, glob, grep, webfetch,
// subagent: CONFIRMED by probe; apply_patch and todowrite are in the binary
// but a free model never called them, so they are unprobed). Harness authors write Claude names, so a
// matcher is a regex tested against the OpenCode id and every Claude name
// that maps to it.
const opencodeHooksPlugin = `// Generated by ynh. Edits are overwritten on the next run.
import { Plugin } from "@opencode/plugin"
import { spawnSync } from "node:child_process"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const HOOKS: Record<string, { matcher?: string; command: string; timeout?: number }[]> = __HOOKS__

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..")

// OpenCode tool ids to the Claude names harness authors write in a matcher.
const CLAUDE_NAMES: Record<string, string[]> = {
  shell: ["Bash"],
  read: ["Read"],
  write: ["Write"],
  edit: ["Edit", "MultiEdit", "NotebookEdit"],
  apply_patch: ["Edit", "Write"],
  glob: ["Glob"],
  grep: ["Grep"],
  webfetch: ["WebFetch"],
  websearch: ["WebSearch"],
  subagent: ["Task", "Agent"],
  skill: ["Skill"],
  todowrite: ["TodoWrite"],
}

// Names a tool answers to: its own id, its Claude names, and for an MCP tool
// (id "<server>_<tool>") every mcp__<server>__<tool> split of it.
function namesFor(tool: string): string[] {
  const names = [tool, ...(CLAUDE_NAMES[tool] ?? [])]
  const parts = tool.split("_")
  for (let i = 1; i < parts.length; i++) {
    names.push("mcp__" + parts.slice(0, i).join("_") + "__" + parts.slice(i).join("_"))
  }
  return names
}

// Tool matchers, compiled once at setup. ynh checks them when it generates
// this file, but one JavaScript still cannot compile would throw inside the
// tool hook and block every call; instead it is reported once and matches
// nothing, so its hook never runs.
const MATCHERS = new Map<string, RegExp | null>()

function compileMatchers() {
  for (const event of ["before_tool", "after_tool"]) {
    for (const h of HOOKS[event] ?? []) {
      if (!h.matcher || h.matcher === "*" || MATCHERS.has(h.matcher)) continue
      try {
        MATCHERS.set(h.matcher, new RegExp("^(?:" + h.matcher + ")$"))
      } catch (e: any) {
        MATCHERS.set(h.matcher, null)
        console.error("[ynh hook] " + h.command + ": matcher " + JSON.stringify(h.matcher) + " is not a valid JavaScript regex, so the hook never runs (" + (e?.message ?? e) + ")")
      }
    }
  }
}

function matches(matcher: string | undefined, tool: string): boolean {
  // "execute" is Code Mode's wrapper: the tools it runs fire their own hooks,
  // so a hook with no matcher is not also run for the wrapper.
  if (!matcher || matcher === "*") return tool !== "execute"
  const re = MATCHERS.get(matcher)
  return !!re && namesFor(tool).some((n) => re.test(n))
}

function claudeInput(input: any): any {
  const out: any = { ...(input ?? {}) }
  if (out.path !== undefined && out.file_path === undefined) out.file_path = out.path
  if (out.oldString !== undefined) out.old_string = out.oldString
  if (out.newString !== undefined) out.new_string = out.newString
  return out
}

// Only exit 2 blocks, as on Claude Code: any other failure lets the call
// proceed. It is reported rather than swallowed, so a hook that never ran is
// visible.
function run(command: string, payload: any, cwd: string, timeout?: number): { code: number | null; text: string } {
  const r = spawnSync("sh", ["-c", command], {
    cwd,
    input: JSON.stringify(payload),
    encoding: "utf8",
    env: { ...process.env, YNH_HOOK_ROOT: ROOT },
    timeout: (timeout ?? 60) * 1000,
  })
  const text = ((r.stderr || "") || (r.stdout || "")).trim()
  if (r.error || r.status === null) {
    console.error("[ynh hook] " + command + ": did not run to completion (" + (r.error ? r.error.message : "killed by " + r.signal) + ")")
  } else if (r.status !== 0 && r.status !== 2) {
    console.error("[ynh hook] " + command + ": exited " + r.status + (text ? ": " + text : ""))
  }
  return { code: r.status, text }
}

function fire(event: string, tool: string | undefined, payload: any, cwd: string, block: boolean) {
  for (const h of HOOKS[event] ?? []) {
    if (tool !== undefined && !matches(h.matcher, tool)) continue
    const r = run(h.command, payload, cwd, h.timeout)
    if (block && r.code === 2) throw new Error(r.text || "blocked by a ynh hook")
  }
}

export default Plugin.define({
  id: "ynh.hooks",
  setup(ctx: any) {
    compileMatchers()
    const cwd: string = ctx.location?.directory ?? process.cwd()
    const toolPayload = (x: any) => ({
      session_id: x.sessionID,
      cwd,
      tool_name: (CLAUDE_NAMES[x.tool] ?? [x.tool])[0],
      tool_input: claudeInput(x.input),
    })

    if (HOOKS.before_tool?.length) {
      ctx.tool.hook("execute.before", (x: any) => {
        fire("before_tool", x.tool, { hook_event_name: "PreToolUse", ...toolPayload(x) }, cwd, true)
      })
    }
    if (HOOKS.after_tool?.length) {
      ctx.tool.hook("execute.after", (x: any) => {
        fire("after_tool", x.tool, {
          hook_event_name: "PostToolUse",
          ...toolPayload(x),
          tool_response: x.result?.output ?? x.error ?? x.result,
        }, cwd, false)
      })
    }
    if (HOOKS.before_prompt?.length) {
      ctx.session.hook("prompt", (x: any) => {
        fire("before_prompt", undefined, {
          hook_event_name: "UserPromptSubmit",
          session_id: x.sessionID,
          cwd,
          prompt: x.prompt?.text,
        }, cwd, true)
      })
    }

    const ac = new AbortController()
    if (HOOKS.on_session_start?.length || HOOKS.on_stop?.length) {
      ;(async () => {
        try {
          for await (const ev of ctx.event.subscribe({ signal: ac.signal })) {
            try {
              const sid = ev.durable?.aggregateID ?? ev.data?.sessionID
              if (ev.type === "session.created") {
                fire("on_session_start", undefined, { hook_event_name: "SessionStart", session_id: sid, cwd }, cwd, false)
              } else if (ev.type === "session.execution.succeeded" || ev.type === "session.execution.failed") {
                fire("on_stop", undefined, { hook_event_name: "Stop", session_id: sid, cwd }, cwd, false)
              }
            } catch {}
          }
        } catch {}
      })()
    }
    return () => ac.abort()
  },
})
`

// opencodeEnvRef matches the ${NAME} references other vendors expand.
var opencodeEnvRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// opencodeEnv rewrites ${NAME} to {env:NAME}: OpenCode expands only the
// latter ("$NAME is not expanded in JSON strings", v2 MCP docs).
func opencodeEnv(s string) string { return opencodeEnvRef.ReplaceAllString(s, "{env:$1}") }

func opencodeEnvMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = opencodeEnv(v)
	}
	return out
}

// opencodeMCPServer is OpenCode 2's MCP server schema: servers sit under
// mcp.servers (v2 does not put names directly under mcp), local commands are
// one array, and env is "environment". Remote servers use OAuth by default, so
// a server that sends its own Authorization header opts out. Other headers
// (a tenant id, an API-version) do not carry credentials, so OAuth stays on.
type opencodeMCPServer struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	OAuth       *bool             `json:"oauth,omitempty"`
}

// GenerateMCPConfig writes .opencode/opencode.json, the one config file the
// bundle carries (CONFIRMED loaded from OPENCODE_CONFIG_DIR; `config.json` is
// not). Streamable HTTP and SSE both become type "remote": the v2 docs name
// only Streamable HTTP, so an sse server is mapped on a best-effort basis.
func (o *OpenCode) GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	out := make(map[string]opencodeMCPServer, len(servers))
	for name, s := range servers {
		var srv opencodeMCPServer
		if s.Transport() == plugin.MCPTypeStdio {
			args := make([]string, 0, len(s.Args)+1)
			args = append(args, opencodeEnv(s.Command))
			for _, a := range s.Args {
				args = append(args, opencodeEnv(a))
			}
			srv = opencodeMCPServer{Type: "local", Command: args, Cwd: s.Cwd, Environment: opencodeEnvMap(s.Env)}
		} else {
			srv = opencodeMCPServer{Type: "remote", URL: opencodeEnv(s.URL), Headers: opencodeEnvMap(s.Headers)}
			for k := range srv.Headers {
				if strings.EqualFold(k, "Authorization") {
					off := false
					srv.OAuth = &off
				}
			}
		}
		out[name] = srv
	}
	doc := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp":     map[string]any{"servers": out},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling MCP config: %w", err)
	}
	return map[string][]byte{filepath.Join(".opencode", "opencode.json"): append(data, '\n')}, nil
}

// OpenCode has no plugin manifest and no marketplace format, and does not read
// agent-plugins.org packages.
func (o *OpenCode) GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	return nil, nil
}

func (o *OpenCode) PluginManifestDir() string { return "" }

func (o *OpenCode) ExportArtifactDirs() map[string]string { return nil }

func (o *OpenCode) SupportsExportDelegates() bool { return false }

func (o *OpenCode) MarketplaceManifestDir() string { return "" }

func (o *OpenCode) GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error) {
	return nil, nil
}

// AgentPluginLayout: a compatibility package with nothing client-specific to
// carry. OpenCode reads neither the format nor a legacy manifest of ynh's.
func (o *OpenCode) AgentPluginLayout() AgentPluginLayout {
	return AgentPluginLayout{}
}

// errOpencodeVersion is returned when the installed OpenCode is not 2.x.
var errOpencodeVersion = errors.New("ynh supports OpenCode 2.x only")

// opencodeVersionRe finds the version in the first line of `--version`
// output ("opencode v2.0.27"). The major must be followed by a dot, so "v2-rc"
// and garbage are refused rather than guessed at.
var opencodeVersionRe = regexp.MustCompile(`\bv?(\d+)\.\d+`)

// checkOpencodeVersion refuses anything but OpenCode 2.x. 1.x also installs an
// `opencode` binary and the two clash (references/opencode.md).
func checkOpencodeVersion(bin string) error {
	out, err := opencodeVersionProbe(bin)
	if err != nil {
		return fmt.Errorf("running %s --version: %w", opencodeCLI, err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if first == "" {
		return fmt.Errorf("%w: %s --version printed nothing", errOpencodeVersion, opencodeCLI)
	}
	m := opencodeVersionRe.FindStringSubmatch(first)
	if m == nil {
		return fmt.Errorf("%w: found %q (install 2.x with: brew install anomalyco/tap/opencode-v2)", errOpencodeVersion, first)
	}
	if n, _ := strconv.Atoi(m[1]); n != opencodeSupportedMajor {
		return fmt.Errorf("%w: found %q (install 2.x with: brew install anomalyco/tap/opencode-v2)", errOpencodeVersion, first)
	}
	return nil
}

// PrepareOpenCode is everything that must hold before OpenCode starts on the
// harness assembled at configPath: bin is OpenCode 2.x, and
// <configPath>/.opencode/AGENTS.md is composed from the instructions, rules and
// runtime text (composeOpencodeAgents). Every way of starting OpenCode, the
// adapter's launches and the `ynh agent run` worker, goes through it; a worker
// that skipped it ran without the harness's instructions and rules and against
// whatever version was installed. An empty configPath only checks the version.
func PrepareOpenCode(configPath, bin string) error {
	if err := checkOpencodeVersion(bin); err != nil {
		return err
	}
	if configPath == "" {
		return nil
	}
	return composeOpencodeAgents(configPath)
}

// opencodeEnviron returns env with OPENCODE_CONFIG_DIR set to dir. Any earlier
// value is dropped: getenv returns the first match, so a later duplicate loses.
func opencodeEnviron(env []string, dir string) []string {
	prefix := opencodeConfigDirEnv + "="
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return append(out, prefix+dir)
}

func launchOpencode(configPath string, args []string) error {
	return launchOpencodeArgs(configPath, append([]string{"--standalone"}, args...))
}

func launchOpencodeArgs(configPath string, args []string) error {
	bin, err := exec.LookPath(opencodeCLI)
	if err != nil {
		return err
	}
	if err := PrepareOpenCode(configPath, bin); err != nil {
		return err
	}
	dir := filepath.Join(configPath, ".opencode")
	return opencodeExec(bin, append([]string{opencodeCLI}, args...), opencodeEnviron(os.Environ(), dir))
}

// OpenCode 2 has no flag that grants a folder beside the project: the
// positional <directory> names the one directory it starts in, and --help
// (v2.2.3) lists no add-dir equivalent.
var opencodeWorkspaceFlag = ""
var opencodeWorkspaceUnsupported = "the OpenCode CLI has no flag that adds a directory to a session (its <directory> argument names the single project)"
