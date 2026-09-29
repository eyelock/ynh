package agentplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Transports (§7.2.1). A server declares exactly one.
const (
	TransportStdio          = "stdio"
	TransportStreamableHTTP = "streamable-http"
	TransportSSE            = "sse"
)

// MCPConfig is the closed root mcp.json (§7.2.1).
type MCPConfig struct {
	Schema  string            `json:"$schema"`
	Servers map[string]Server `json:"mcpServers"`
}

// Server is one mcpServers entry. Which fields may be set depends on Type:
// stdio takes command, args, env and cwd; streamable-http and sse take url
// and headers. The schema enforces the split; this type simply holds the
// union.
type Server struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// ReadMCP loads the root mcp.json (§7.2).
//
// An absent file is not an error: nil, nil, nil. A top-level failure (not
// a regular file, not JSON, wrong or mismatched $schema, extra fields,
// mcpServers not an object) returns an error, which means MCP is disabled
// for the plugin while other components still load (§7.2.2 rule 2). A
// server entry that fails its own rules is left out of the result and
// reported as a diagnostic (§7.2.2 rule 3).
func ReadMCP(dir string) (*MCPConfig, []Diagnostic, error) {
	p := filepath.Join(dir, MCPFile)
	info, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("%s: %w", MCPFile, err)
	}
	if err := within(dir, p); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", MCPFile, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if info, err = os.Stat(p); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", MCPFile, err)
		}
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s: not a regular file", MCPFile)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", MCPFile, err)
	}
	return ParseMCP(dir, data)
}

// ParseMCP is ReadMCP over bytes already in hand. dir is the plugin root,
// needed to check that a plugin-relative command or cwd stays inside it.
func ParseMCP(dir string, data []byte) (*MCPConfig, []Diagnostic, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("%s: not a JSON object: %w", MCPFile, err)
	}
	if raw == nil {
		return nil, nil, fmt.Errorf("%s: not a JSON object", MCPFile)
	}
	var schema string
	if s, ok := raw["$schema"]; !ok {
		return nil, nil, fmt.Errorf("%s: missing required field $schema", MCPFile)
	} else if err := json.Unmarshal(s, &schema); err != nil || schema != MCPSchemaID {
		return nil, nil, fmt.Errorf("%s: unsupported Agent Plugins version: $schema must be %q", MCPFile, MCPSchemaID)
	}
	for k := range raw {
		if k != "$schema" && k != "mcpServers" {
			return nil, nil, fmt.Errorf("%s: unknown top-level field %q", MCPFile, k)
		}
	}
	serversRaw, ok := raw["mcpServers"]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing required field mcpServers", MCPFile)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(serversRaw, &entries); err != nil || entries == nil {
		return nil, nil, fmt.Errorf("%s: mcpServers must be an object", MCPFile)
	}

	_, _, serverSchema, err := compiled()
	if err != nil {
		return nil, nil, err
	}

	cfg := &MCPConfig{Schema: schema, Servers: map[string]Server{}}
	var diags []Diagnostic
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := entries[name]
		where := MCPFile + " server " + name
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(entry))
		if err != nil {
			diags = append(diags, Diagnostic{where, "skipped: " + err.Error()})
			continue
		}
		if err := serverSchema.Validate(doc); err != nil {
			diags = append(diags, Diagnostic{where, "skipped: " + describeServerError(entry, err)})
			continue
		}
		var s Server
		if err := json.Unmarshal(entry, &s); err != nil {
			diags = append(diags, Diagnostic{where, "skipped: " + err.Error()})
			continue
		}
		if msgs := checkServer(dir, s); len(msgs) > 0 {
			diags = append(diags, Diagnostic{where, "skipped: " + joinIssues(msgs)})
			continue
		}
		cfg.Servers[name] = s
	}
	return cfg, diags, nil
}

// describeServerError turns the oneOf failure tree for a server entry into
// the message an author can act on. The schema is a closed union keyed by
// type, so the useful branch is the one for the type the entry declares;
// the other branches always fail on their const and only add noise.
func describeServerError(entry json.RawMessage, err error) string {
	var probe struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(entry, &probe)
	switch probe.Type {
	case "":
		return "missing required field type (stdio, streamable-http or sse)"
	case TransportStdio, TransportStreamableHTTP, TransportSSE:
	default:
		return fmt.Sprintf("unknown type %q (stdio, streamable-http or sse)", probe.Type)
	}
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err.Error()
	}
	// Walk to the oneOf branch whose "type" const did not fail.
	var msgs []string
	var walk func(*jsonschema.ValidationError)
	walk = func(v *jsonschema.ValidationError) {
		if len(v.Causes) == 0 {
			loc := strings.Join(v.InstanceLocation, "/")
			if loc == "type" {
				return
			}
			msg := v.ErrorKind.LocalizedString(schemaPrinter)
			if loc != "" {
				msg = loc + ": " + msg
			}
			msgs = append(msgs, msg)
			return
		}
		for _, c := range v.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(msgs) == 0 {
		return err.Error()
	}
	return joinIssues(dedupe(msgs))
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// headerName is an HTTP token (RFC 9110 §5.6.2).
var headerName = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// checkServer applies the rules of §7.2.1 and §9 that JSON Schema cannot
// express. It returns one message per violation; any message makes the
// entry invalid.
func checkServer(dir string, s Server) []string {
	var msgs []string
	switch s.Type {
	case TransportStdio:
		msgs = append(msgs, checkCommand(dir, s.Command)...)
		if s.Cwd != "" {
			msgs = append(msgs, checkCwd(dir, s.Cwd)...)
		}
		if strings.Contains(s.Command, PlaceholderRoot) || strings.Contains(s.Command, PlaceholderData) {
			msgs = append(msgs, "command: placeholders are not expanded in command; use a ./ path for a bundled executable")
		}
	case TransportStreamableHTTP, TransportSSE:
		msgs = append(msgs, checkURL(s.URL)...)
		msgs = append(msgs, checkHeaders(s.Headers)...)
	}
	return msgs
}

// checkCommand: one executable token, either a bare name or a ./ path that
// stays inside the plugin root (§7.2.1).
func checkCommand(dir, command string) []string {
	if command != strings.TrimSpace(command) || strings.ContainsAny(command, " \t\n") {
		return []string{fmt.Sprintf("command %q must be a single executable token, not a shell command string", command)}
	}
	if filepath.IsAbs(command) || strings.HasPrefix(command, "/") {
		return []string{fmt.Sprintf("command %q must be a bare executable name or a plugin-relative path beginning with ./", command)}
	}
	if strings.Contains(command, "/") {
		if !strings.HasPrefix(command, "./") {
			return []string{fmt.Sprintf("command %q must be a bare executable name or a plugin-relative path beginning with ./", command)}
		}
		return checkRelative(dir, "command", command)
	}
	return nil
}

// checkCwd: ./, ${PLUGIN_ROOT} or ${PLUGIN_DATA} rooted, and contained in
// the directory it is rooted at (§7.2.1). The schema already checks the
// prefix; this checks containment. PLUGIN_DATA is a client-chosen directory
// that does not exist here, so only lexical escape can be checked for it.
func checkCwd(dir, cwd string) []string {
	switch {
	case strings.HasPrefix(cwd, "./"):
		return checkRelative(dir, "cwd", cwd)
	case cwd == PlaceholderRoot:
		return nil
	case strings.HasPrefix(cwd, PlaceholderRoot+"/"):
		return checkRelative(dir, "cwd", "./"+strings.TrimPrefix(cwd, PlaceholderRoot+"/"))
	case cwd == PlaceholderData:
		return nil
	case strings.HasPrefix(cwd, PlaceholderData+"/"):
		if escapes(strings.TrimPrefix(cwd, PlaceholderData+"/")) {
			return []string{fmt.Sprintf("cwd %q escapes ${PLUGIN_DATA}", cwd)}
		}
		return nil
	}
	return []string{fmt.Sprintf("cwd %q must begin with ./, ${PLUGIN_ROOT} or ${PLUGIN_DATA}", cwd)}
}

// checkRelative checks a ./ path lexically and then, when the target
// exists, through symlinks (§4.1 point 3).
func checkRelative(dir, field, rel string) []string {
	if escapes(strings.TrimPrefix(rel, "./")) {
		return []string{fmt.Sprintf("%s %q escapes the plugin root", field, rel)}
	}
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if _, err := os.Lstat(target); err != nil {
		return nil // absent is a runtime failure (§7.2.2 rule 5), not invalid configuration
	}
	if err := within(dir, target); err != nil {
		return []string{fmt.Sprintf("%s %q %v", field, rel, err)}
	}
	return nil
}

// escapes reports whether a slash-separated relative path climbs above its
// base once cleaned.
func escapes(rel string) bool {
	clean := path.Clean(rel)
	return clean == ".." || strings.HasPrefix(clean, "../")
}

// checkURL: absolute http or https, no userinfo, no fragment, and https
// unless the host is loopback (§7.2.1).
func checkURL(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return []string{fmt.Sprintf("url %q must be an absolute HTTP or HTTPS URL", raw)}
	}
	var msgs []string
	if u.Scheme != "http" && u.Scheme != "https" {
		return []string{fmt.Sprintf("url %q must use http or https", raw)}
	}
	if u.User != nil {
		msgs = append(msgs, "url must not contain user information")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		msgs = append(msgs, "url must not contain a fragment")
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		msgs = append(msgs, fmt.Sprintf("url %q: a non-loopback endpoint must use https", raw))
	}
	return msgs
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// checkHeaders: valid field names and values, no name repeated under a
// different casing, and no placeholder expansion (§7.2.1).
func checkHeaders(h map[string]string) []string {
	var msgs []string
	seen := map[string]string{}
	names := make([]string, 0, len(h))
	for n := range h {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !headerName.MatchString(n) {
			msgs = append(msgs, fmt.Sprintf("header %q is not a valid HTTP header name", n))
		}
		if strings.ContainsAny(h[n], "\r\n") {
			msgs = append(msgs, fmt.Sprintf("header %q value must not contain line breaks", n))
		}
		lower := strings.ToLower(n)
		if prev, dup := seen[lower]; dup {
			msgs = append(msgs, fmt.Sprintf("header %q repeats %q under different casing", n, prev))
		}
		seen[lower] = n
	}
	return msgs
}
