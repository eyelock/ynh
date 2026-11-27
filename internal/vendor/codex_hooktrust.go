package vendor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Codex runs a hook only when two things are trusted, and neither is true of
// ynh's run directory, so a hook ynh writes there never fires on its own
// (ynh #682):
//
//  1. the project layer: Codex loads <project>/.codex/ (hooks.json among it)
//     only for a trusted project (projects."<dir>".trust_level), and
//  2. the hook itself: a non-managed hook runs only when
//     hooks.state."<key>".trusted_hash holds the hash Codex computes for its
//     current definition (learn.chatgpt.com/docs/hooks, "Codex records trust
//     against the hook's current hash"). Persisted trust is recorded in the
//     user's ~/.codex/config.toml by the /hooks screen, never by ynh.
//
// The run directory is ynh's own, so ynh grants both for the one invocation
// with session -c overrides and persists nothing in the user's Codex config,
// but only when the installed harness is accepted and current in ynh trust:
// cmd/ynh decides that (codexHookTrustLaunchArgs) before calling this.
//
// The hash is Codex's own and undocumented. It was established live
// (codex-cli 0.166.0) by comparing hooks/list's currentHash with the SHA-256 of
// the canonical JSON below, and TestCodexHookHashMatchesCodex pins it. Should
// a Codex release change the algorithm, the hooks go back to being skipped
// (the state before this fix), never run untrusted: Codex compares the hash.

// codexHookEventSnake is each Codex hook event's name as Codex spells it in a
// hook's key and hash.
var codexHookEventSnake = map[string]string{
	"PreToolUse":       "pre_tool_use",
	"PostToolUse":      "post_tool_use",
	"UserPromptSubmit": "user_prompt_submit",
	"Stop":             "stop",
	"SessionStart":     "session_start",
	"SessionEnd":       "session_end",
}

// codexDefaultHookTimeout is the timeout Codex assumes for a hook that sets
// none, in seconds, and so what its hash holds: 600, but 1 for SessionEnd
// (learn.chatgpt.com/docs/hooks; confirmed by hooks/list's timeoutSec).
func codexDefaultHookTimeout(snakeEvent string) int {
	if snakeEvent == "session_end" {
		return 1
	}
	return 600
}

// codexHookIdentity is what Codex hashes for one hook handler, as canonical
// JSON (keys sorted, no spaces). Matcher is left out when empty.
type codexHookIdentity struct {
	EventName string
	Matcher   string
	Command   string
	Timeout   int
}

func (h codexHookIdentity) hash() string {
	var b strings.Builder
	b.WriteString(`{"event_name":`)
	b.WriteString(jsonString(h.EventName))
	b.WriteString(`,"hooks":[{"async":false,"command":`)
	b.WriteString(jsonString(h.Command))
	b.WriteString(`,"timeout":`)
	b.WriteString(strconv.Itoa(h.Timeout))
	b.WriteString(`,"type":"command"}]`)
	if h.Matcher != "" {
		b.WriteString(`,"matcher":`)
		b.WriteString(jsonString(h.Matcher))
	}
	b.WriteString(`}`)
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// jsonString is s as a JSON string literal without HTML escaping, as Codex's
// serializer writes it.
func jsonString(s string) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	out := strings.TrimSuffix(sb.String(), "\n")
	// Go escapes these two; serde_json, which Codex hashes with, does not.
	out = strings.ReplaceAll(out, `\u2028`, "\u2028")
	return strings.ReplaceAll(out, `\u2029`, "\u2029")
}

// codexHookFile is one hook handler of a hooks.json, in file order.
type codexHookFile struct {
	Event, Snake, Matcher, Command string
	Timeout                        int // as declared; 0 when the hook sets none
	Group, Index                   int
}

// readCodexHooks reads the command hooks of dir/.codex/hooks.json, events
// sorted, and the file's path. It returns none when the file is missing,
// unreadable or unexpected, or holds no hook ynh can name.
func readCodexHooks(runDir string) ([]codexHookFile, string, string) {
	dir, err := filepath.EvalSymlinks(runDir)
	if err != nil {
		return nil, "", ""
	}
	file := filepath.Join(dir, ".codex", "hooks.json")
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, "", ""
	}
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil, "", ""
	}
	events := make([]string, 0, len(doc.Hooks))
	for e := range doc.Hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	var out []codexHookFile
	for _, event := range events {
		snake, ok := codexHookEventSnake[event]
		if !ok {
			continue
		}
		for gi, g := range doc.Hooks[event] {
			for hi, h := range g.Hooks {
				if h.Type != "command" {
					continue
				}
				out = append(out, codexHookFile{event, snake, g.Matcher, h.Command, h.Timeout, gi, hi})
			}
		}
	}
	return out, dir, file
}

func (h codexHookFile) identity() codexHookIdentity {
	timeout := h.Timeout
	if timeout == 0 {
		timeout = codexDefaultHookTimeout(h.Snake)
	}
	return codexHookIdentity{EventName: h.Snake, Matcher: h.Matcher, Command: h.Command, Timeout: timeout}
}

// CodexHookTrustArgs is the session overrides that let Codex run the hooks
// ynh wrote to runDir/.codex/hooks.json: the run directory as a trusted
// project, and each hook's current hash as trusted. It returns nil when there
// is no hook file or it holds no hook. An unreadable or unexpected hook file
// also yields nil: the hooks are then skipped, as before.
func CodexHookTrustArgs(runDir string) []string {
	hooks, dir, file := readCodexHooks(runDir)
	var state []string
	for _, h := range hooks {
		key := fmt.Sprintf("%s:%s:%d:%d", file, h.Snake, h.Group, h.Index)
		state = append(state, tomlString(key)+"={trusted_hash="+tomlString(h.identity().hash())+"}")
	}
	if len(state) == 0 {
		return nil
	}
	return []string{
		"-c", "projects={" + tomlString(dir) + "={trust_level=\"trusted\"}}",
		"-c", "hooks.state={" + strings.Join(state, ",") + "}",
	}
}

// codexSessionFlagsSource is the source Codex names for hooks given as -c
// overrides, in a hook's key.
const codexSessionFlagsSource = "/<session-flags>/config.toml"

// CodexSessionHookArgs is the overrides that give a Codex worker the hooks
// ynh wrote to runDir/.codex/hooks.json, and trust them. A worker runs in the
// task's worktree, not in runDir, so Codex never loads runDir's project layer;
// the hooks are declared inline as -c overrides instead (as the worker's agents
// and MCP servers are), under the key Codex gives a session-flags hook, with
// each one's current hash trusted. It returns nil when there is no hook.
func CodexSessionHookArgs(runDir string) []string {
	hooks, _, _ := readCodexHooks(runDir)
	if len(hooks) == 0 {
		return nil
	}
	// Hooks of an event keep their file order, and an event's groups and
	// handlers are indexed from zero, as Codex keys them.
	var args, state []string
	for i := 0; i < len(hooks); {
		j := i
		for j < len(hooks) && hooks[j].Event == hooks[i].Event {
			j++
		}
		var groups []string
		for k := i; k < j; {
			l := k
			for l < j && hooks[l].Group == hooks[k].Group {
				l++
			}
			var handlers []string
			for hi, h := range hooks[k:l] {
				key := fmt.Sprintf("%s:%s:%d:%d", codexSessionFlagsSource, h.Snake, len(groups), hi)
				state = append(state, tomlString(key)+"={trusted_hash="+tomlString(h.identity().hash())+"}")
				d := `{type="command",command=` + tomlString(h.Command)
				if h.Timeout != 0 {
					d += ",timeout=" + strconv.Itoa(h.Timeout)
				}
				handlers = append(handlers, d+"}")
			}
			g := "{"
			if hooks[k].Matcher != "" {
				g += "matcher=" + tomlString(hooks[k].Matcher) + ","
			}
			groups = append(groups, g+"hooks=["+strings.Join(handlers, ",")+"]}")
			k = l
		}
		args = append(args, "-c", "hooks."+hooks[i].Event+"=["+strings.Join(groups, ",")+"]")
		i = j
	}
	return append(args, "-c", "hooks.state={"+strings.Join(state, ",")+"}")
}
