package agentplugin

import (
	"bytes"
	"embed"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Canonical identifiers for Agent Plugins 1.0.0. A plugin declares which
// specification version it targets through these exact strings (§5.2,
// §7.2.1); a client selects its validation rules from them and never fetches
// them.
const (
	Version        = "1.0.0"
	PluginSchemaID = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	MCPSchemaID    = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

//go:embed schema/1.0.0/plugin.schema.json schema/1.0.0/mcp.schema.json
var schemaFS embed.FS

var (
	compileOnce   sync.Once
	pluginSchema  *jsonschema.Schema
	mcpSchema     *jsonschema.Schema
	serverSchema  *jsonschema.Schema
	compileFailed error
)

func compiled() (plugin, mcp, server *jsonschema.Schema, err error) {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.UseRegexpEngine(regexpEngine)
		for id, path := range map[string]string{
			PluginSchemaID: "schema/1.0.0/plugin.schema.json",
			MCPSchemaID:    "schema/1.0.0/mcp.schema.json",
		} {
			data, rerr := schemaFS.ReadFile(path)
			if rerr != nil {
				compileFailed = fmt.Errorf("reading %s: %w", path, rerr)
				return
			}
			doc, perr := jsonschema.UnmarshalJSON(bytes.NewReader(data))
			if perr != nil {
				compileFailed = fmt.Errorf("parsing %s: %w", path, perr)
				return
			}
			if aerr := c.AddResource(id, doc); aerr != nil {
				compileFailed = fmt.Errorf("adding %s: %w", id, aerr)
				return
			}
		}
		var cerr error
		if pluginSchema, cerr = c.Compile(PluginSchemaID); cerr != nil {
			compileFailed = fmt.Errorf("compiling plugin schema: %w", cerr)
			return
		}
		if mcpSchema, cerr = c.Compile(MCPSchemaID); cerr != nil {
			compileFailed = fmt.Errorf("compiling mcp schema: %w", cerr)
			return
		}
		// The MCP schema exposes #/$defs/server so a client can validate
		// each entry on its own and keep the per-server failure boundary
		// of §7.2.2 (an invalid entry is skipped, not the whole file).
		if serverSchema, cerr = c.Compile(MCPSchemaID + "#/$defs/server"); cerr != nil {
			compileFailed = fmt.Errorf("compiling mcp server schema: %w", cerr)
			return
		}
	})
	return pluginSchema, mcpSchema, serverSchema, compileFailed
}

// negativeLookahead matches a pattern of the form ^(?!.*(?:X))REST. The
// official name pattern is written that way, and Go's RE2 engine has no
// lookahead. The form means "REST matches, and X occurs nowhere in the
// string", which two RE2 expressions can say between them.
var negativeLookahead = regexp.MustCompile(`^\^\(\?!\.\*(\(\?:[^)]*\))\)(.*)$`)

// regexpEngine is the pattern engine handed to the schema compiler. It is
// Go's regexp, plus a translation of the one lookahead form the official
// schemas use. Any other pattern RE2 cannot compile is still an error: the
// point is to validate what the schema says, not to guess at it.
func regexpEngine(pattern string) (jsonschema.Regexp, error) {
	if re, err := regexp.Compile(pattern); err == nil {
		return re, nil
	}
	m := negativeLookahead.FindStringSubmatch(pattern)
	if m == nil {
		_, err := regexp.Compile(pattern)
		return nil, err
	}
	forbidden, err := regexp.Compile(m[1])
	if err != nil {
		return nil, fmt.Errorf("lookahead body: %w", err)
	}
	rest, err := regexp.Compile("^" + m[2])
	if err != nil {
		return nil, fmt.Errorf("pattern body: %w", err)
	}
	return &excludingRegexp{pattern: pattern, rest: rest, forbidden: forbidden}, nil
}

type excludingRegexp struct {
	pattern   string
	rest      *regexp.Regexp
	forbidden *regexp.Regexp
}

func (r *excludingRegexp) MatchString(s string) bool {
	return r.rest.MatchString(s) && !r.forbidden.MatchString(s)
}

func (r *excludingRegexp) String() string { return r.pattern }

// schemaErrors flattens a validation error tree into one message per leaf,
// each prefixed with its instance location so an author can find the field.
func schemaErrors(err error) []string {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []string{err.Error()}
	}
	var out []string
	var walk func(*jsonschema.ValidationError)
	walk = func(v *jsonschema.ValidationError) {
		if len(v.Causes) == 0 {
			msg := v.ErrorKind.LocalizedString(schemaPrinter)
			if loc := strings.Join(v.InstanceLocation, "/"); loc != "" {
				msg = loc + ": " + msg
			}
			out = append(out, msg)
			return
		}
		for _, c := range v.Causes {
			walk(c)
		}
	}
	walk(ve)
	return out
}
