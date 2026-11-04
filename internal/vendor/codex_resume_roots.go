package vendor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// codexWritableRootsKey is the config key `codex exec resume` takes granted
// folders through: it rejects --add-dir (codex-cli 0.166.0: "unexpected
// argument --add-dir found") but accepts -c.
const codexWritableRootsKey = "sandbox_workspace_write.writable_roots"

// codexUserConfigPath is the user's Codex config.toml: $CODEX_HOME, else ~/.codex.
func codexUserConfigPath() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return filepath.Join(h, "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "config.toml")
}

// codexExecResumeExtraArgs rewrites the caller's args for `codex exec resume`:
// each "--add-dir <dir>" pair becomes one entry of a single
// -c sandbox_workspace_write.writable_roots=[...] override. Every other arg
// passes through in order. This is the one place a Codex subcommand's arg
// dialect is translated; a further per-subcommand rewrite belongs beside it.
//
// -c replaces the key, where --add-dir adds to the list (checked live against
// codex-cli 0.166.0: a config.toml root vanishes once -c names another). So the
// override carries the user's own roots from config.toml and any roots in a
// caller's earlier -c on the key, then the granted folders.
//
// writable_roots applies only under sandbox_mode workspace-write; under any
// other mode the grant does nothing, so ynh says so on warn.
func codexExecResumeExtraArgs(extraArgs []string, userConfig string, warn io.Writer) []string {
	var granted []string
	rest := make([]string, 0, len(extraArgs))
	for i := 0; i < len(extraArgs); i++ {
		if extraArgs[i] == codexWorkspaceFlag && i+1 < len(extraArgs) {
			granted = append(granted, extraArgs[i+1])
			i++
			continue
		}
		rest = append(rest, extraArgs[i])
	}
	if len(granted) == 0 {
		return rest
	}

	var cfg string
	if userConfig != "" {
		if data, err := os.ReadFile(userConfig); err == nil {
			cfg = string(data)
		}
	}

	roots, ok := codexTOMLStringArray(cfg, "sandbox_workspace_write", "writable_roots")
	if !ok && warn != nil && strings.Contains(cfg, "writable_roots") {
		_, _ = fmt.Fprintf(warn, "ynh: could not read writable_roots in %s; the folders ynh grants replace it for this resume\n", userConfig)
	}

	// A caller's earlier -c on the key would be overridden by ours: fold it in.
	out := make([]string, 0, len(rest)+2)
	for i := 0; i < len(rest); i++ {
		if (rest[i] == "-c" || rest[i] == "--config") && i+1 < len(rest) {
			if v, found := strings.CutPrefix(rest[i+1], codexWritableRootsKey+"="); found {
				if r, ok := parseTOMLStringArray(v); ok {
					roots = append(roots, r...)
					i++
					continue
				}
				if warn != nil {
					_, _ = fmt.Fprintf(warn, "ynh: could not read the caller's -c %s; the folders ynh grants replace it for this resume\n", codexWritableRootsKey)
				}
			}
		}
		out = append(out, rest[i])
	}

	if mode := codexSandboxMode(rest, cfg); mode != "workspace-write" && mode != "danger-full-access" && warn != nil {
		_, _ = fmt.Fprintf(warn, "ynh: workspace folders only take effect under Codex's workspace-write sandbox; %s, so %s is not writable on this resume\n",
			codexSandboxDescription(mode), strings.Join(granted, ", "))
	}

	seen := map[string]bool{}
	var enc []string
	for _, r := range append(roots, granted...) {
		if !seen[r] {
			seen[r] = true
			enc = append(enc, tomlBasicString(r))
		}
	}
	return append(out, "-c", codexWritableRootsKey+"=["+strings.Join(enc, ",")+"]")
}

func codexSandboxDescription(mode string) string {
	if mode == "" {
		return "no sandbox_mode is set, so `codex exec` defaults to read-only"
	}
	return "the sandbox is " + mode
}

// codexSandboxMode resolves the sandbox a resume will run under: the caller's
// flags first, then the user's config.toml, else "" (unset).
func codexSandboxMode(args []string, cfg string) string {
	mode := ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case (a == "-s" || a == "--sandbox") && i+1 < len(args):
			mode = args[i+1]
			i++
		case strings.HasPrefix(a, "--sandbox="):
			mode = strings.TrimPrefix(a, "--sandbox=")
		case a == "--full-auto":
			mode = "workspace-write"
		case a == "--dangerously-bypass-approvals-and-sandbox" || a == "--yolo":
			mode = "danger-full-access"
		case (a == "-c" || a == "--config") && i+1 < len(args):
			if v, ok := strings.CutPrefix(args[i+1], "sandbox_mode="); ok {
				mode = strings.Trim(strings.TrimSpace(v), `"'`)
			}
			i++
		}
	}
	if mode != "" {
		return mode
	}
	for _, line := range strings.Split(cfg, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			break // top level only
		}
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "sandbox_mode" {
			return strings.Trim(strings.TrimSpace(stripTOMLComment(v)), `"'`)
		}
	}
	return ""
}

func stripTOMLComment(s string) string {
	if i := strings.Index(s, "#"); i >= 0 {
		return s[:i]
	}
	return s
}

// tomlBasicString encodes s as a TOML basic string: it escapes the quote, the
// backslash and every control character, the last as \b \t \n \f \r or \uXXXX
// (TOML has no \a, \v or \xNN, which strconv.Quote would write).
func tomlBasicString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		case r == utf8.RuneError:
			b.WriteString(`�`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// codexTOMLStringArray reads an array of strings at table.key from TOML text,
// as either [table] key = [...] or a top-level dotted table.key = [...]. ok is
// false when the key is absent or is not an array of strings.
func codexTOMLStringArray(doc, table, key string) ([]string, bool) {
	lines := strings.Split(doc, "\n")
	inTable := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "[") {
			inTable = strings.TrimSpace(strings.Trim(line, "[]")) == table
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		k = strings.TrimSpace(k)
		if (!inTable || k != key) && k != table+"."+key {
			continue
		}
		// The array may span lines: gather until the brackets balance.
		val := strings.TrimSpace(v)
		for !strings.Contains(val, "]") && i+1 < len(lines) {
			i++
			val += "\n" + lines[i]
		}
		return parseTOMLStringArray(val)
	}
	return nil, false
}

// parseTOMLStringArray parses `["a", 'b', ...]` (comments and a trailing comma
// allowed). ok is false for anything else.
func parseTOMLStringArray(s string) ([]string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") {
		return nil, false
	}
	s = s[1:]
	var out []string
	for {
		s = strings.TrimLeft(s, " \t\r\n,")
		for strings.HasPrefix(s, "#") {
			nl := strings.IndexByte(s, '\n')
			if nl < 0 {
				return nil, false
			}
			s = strings.TrimLeft(s[nl:], " \t\r\n,")
		}
		if strings.HasPrefix(s, "]") {
			return out, true
		}
		str, n, ok := readTOMLString(s)
		if !ok {
			return nil, false
		}
		out = append(out, str)
		s = s[n:]
	}
}

// readTOMLString reads one single-line basic ("...") or literal ('...') string
// from the start of s, returning it and the bytes consumed.
func readTOMLString(s string) (string, int, bool) {
	if s == "" {
		return "", 0, false
	}
	switch s[0] {
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 || strings.ContainsAny(s[1:1+end], "\n") {
			return "", 0, false
		}
		return s[1 : 1+end], end + 2, true
	case '"':
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; c {
			case '"':
				return b.String(), i + 1, true
			case '\n':
				return "", 0, false
			case '\\':
				i++
				if i >= len(s) {
					return "", 0, false
				}
				switch e := s[i]; e {
				case 'b':
					b.WriteByte('\b')
				case 't':
					b.WriteByte('\t')
				case 'n':
					b.WriteByte('\n')
				case 'f':
					b.WriteByte('\f')
				case 'r':
					b.WriteByte('\r')
				case '"', '\\':
					b.WriteByte(e)
				case 'u', 'U':
					w := 4
					if e == 'U' {
						w = 8
					}
					if i+w >= len(s) {
						return "", 0, false
					}
					n, err := strconv.ParseUint(s[i+1:i+1+w], 16, 32)
					if err != nil {
						return "", 0, false
					}
					b.WriteRune(rune(n))
					i += w
				default:
					return "", 0, false
				}
			default:
				b.WriteByte(c)
			}
		}
	}
	return "", 0, false
}
