package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
)

// scriptRef is the script a hook, MCP server or sensor command runs, as the
// trust digest sees it (digest scheme 3).
//
// Covered is true when the script is a file of the harness or include that
// declares the command, named by "./x" or "$YNH_HARNESS_DIR/x": its contents
// are then part of the digest, so changing the file alone changes it. A script
// anywhere else (`bash scripts/wake.sh`, an absolute path) is project code, as
// `make check` is: it is reported as not covered and the digest leaves it
// out. Whatever a covered script sources or runs in turn is not covered either.
type scriptRef struct {
	// Path is the script as the command names it.
	Path    string `json:"path"`
	Covered bool   `json:"covered"`
	// Digest is "sha256:" and the hex of the file's contents, "missing" when
	// the file is not there and "unreadable" when it cannot be read. Present
	// only when Covered.
	Digest string `json:"digest,omitempty"`
}

// scriptInterpreters are the commands whose first argument is the script they
// run.
var scriptInterpreters = map[string]bool{"bash": true, "sh": true, "python3": true, "node": true}

// harnessDirVars are the spellings of the variable that resolves to the
// declaring harness's directory.
var harnessDirVars = []string{"$YNH_HARNESS_DIR/", "${YNH_HARNESS_DIR}/"}

func unquoteToken(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// scriptToken is the word that names the script a command line runs: its first
// word, or its first argument when the first word is an interpreter. fromInterp
// says which, because an interpreter's argument is a file whether or not it
// holds a slash.
func scriptToken(fields []string) (tok string, fromInterp bool) {
	if len(fields) == 0 {
		return "", false
	}
	first := unquoteToken(fields[0])
	if scriptInterpreters[first] {
		if len(fields) < 2 {
			return "", true
		}
		return unquoteToken(fields[1]), true
	}
	return first, false
}

// harnessRelative reports the path inside the declaring harness that tok
// names, when it is "./x" (allowDot) or "$YNH_HARNESS_DIR/x".
func harnessRelative(tok string, allowDot bool) (string, bool) {
	if allowDot && strings.HasPrefix(tok, "./") {
		return tok[2:], true
	}
	for _, v := range harnessDirVars {
		if strings.HasPrefix(tok, v) {
			return tok[len(v):], true
		}
	}
	return "", false
}

// otherScript is the reference for a script the digest does not cover, or nil
// when tok does not look like a script at all (a command found on PATH, a flag).
func otherScript(tok string, fromInterp bool) *scriptRef {
	if tok == "" {
		return nil
	}
	if fromInterp {
		if strings.HasPrefix(tok, "-") {
			return nil
		}
	} else if !strings.Contains(tok, "/") {
		return nil
	}
	return &scriptRef{Path: tok}
}

// hashScript reads rel under dir and returns the covered reference. A path
// that climbs out of dir, or no known dir, is left uncovered rather than read.
func hashScript(dir, rel, shown string) *scriptRef {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if dir == "" || !filepath.IsLocal(clean) {
		return &scriptRef{Path: shown}
	}
	ref := &scriptRef{Path: shown, Covered: true}
	f, err := os.Open(filepath.Join(dir, clean))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			ref.Digest = "missing"
		} else {
			ref.Digest = "unreadable"
		}
		return ref
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		// A directory, or a read that failed part way: never a digest of a
		// part, which could equal the whole.
		ref.Digest = "unreadable"
		return ref
	}
	ref.Digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return ref
}

// shortDigest is a script digest as shown to a person.
func shortDigest(d string) string {
	if strings.HasPrefix(d, "sha256:") && len(d) > len("sha256:")+16 {
		return d[:len("sha256:")+16]
	}
	return d
}

// write adds a covered script to a digest. An uncovered one adds nothing, so a
// harness whose commands name no covered script keeps the digest it would
// have had without this field.
func (s *scriptRef) write(w io.Writer) {
	if s == nil || !s.Covered {
		return
	}
	field(w, "script")
	field(w, s.Path)
	field(w, s.Digest)
}

// sensorScript is the script a sensor command runs. A sensor's command runs
// in the measured tree, so only $YNH_HARNESS_DIR names a file of the harness;
// "./x" is the tree's own.
func sensorScript(command, dir string) *scriptRef {
	tok, interp := scriptToken(strings.Fields(command))
	if rel, ok := harnessRelative(tok, false); ok {
		return hashScript(dir, rel, tok)
	}
	return otherScript(tok, interp)
}

// hookScriptRef is the script a hook command runs. The "./" scripts of an
// included harness are rewritten to a place in the session (scripts), so the
// original is looked up there; anything else resolves against dir, the
// directory of the harness that declared the hook.
func hookScriptRef(command, dir string, scripts map[string]assembler.ScriptOrigin) *scriptRef {
	tok, interp := scriptToken(strings.Fields(command))
	if o, ok := scripts[tok]; ok {
		return hashScript(o.Dir, strings.TrimPrefix(o.Script, "./"), o.Script)
	}
	if rel, ok := harnessRelative(tok, true); ok {
		return hashScript(dir, rel, tok)
	}
	return otherScript(tok, interp)
}

// mcpScript is the script a stdio MCP server's command runs.
func mcpScript(command string, args []string, dir string) *scriptRef {
	if command == "" {
		return nil
	}
	tok, interp := scriptToken(append([]string{command}, args...))
	if rel, ok := harnessRelative(tok, true); ok {
		return hashScript(dir, rel, tok)
	}
	return otherScript(tok, interp)
}

// writeScriptLine prints a command's script under it in `trust show`.
func writeScriptLine(w io.Writer, s *scriptRef) {
	if s == nil {
		return
	}
	if !s.Covered {
		_, _ = fmt.Fprintf(w, "    script:  %s (not covered: project code, outside what this digest reads)\n", s.Path)
		return
	}
	_, _ = fmt.Fprintf(w, "    script:  %s (contents %s)\n", s.Path, shortDigest(s.Digest))
}
