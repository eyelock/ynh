package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/plugin"
)

// scriptRef is the script a hook, MCP server or sensor command runs, as the
// trust digest sees it (digest scheme 3).
//
// Covered is true when the script is a file of the harness or include that
// declares the command, named by "./x" or "$YNH_HARNESS_DIR/x": its contents
// are then part of the digest, and so is every file in its folder, subfolders
// included (a script that builds or reads code beside it is only as reviewed as
// that code), so changing any of them alone changes the digest. A script
// anywhere else (`bash scripts/wake.sh`, an absolute path) is project code, as
// `make check` is: it is reported as not covered and the digest leaves it
// out. What a covered script reaches outside its own folder is not covered.
type scriptRef struct {
	// Path is the script as the command names it.
	Path    string `json:"path"`
	Covered bool   `json:"covered"`
	// Folder is the folder the digest reads: the script's own, relative to the
	// declaring harness ("." for the harness's root). Files is how many files
	// it holds, subfolders included. Present only when Covered.
	Folder string `json:"folder,omitempty"`
	Files  int    `json:"files,omitempty"`
	// Skipped lists what the walk refused to read or follow, each with why: a
	// link out of the harness, a device, a link that loops. Never read, never
	// silently dropped.
	Skipped []string `json:"skipped,omitempty"`
	// Digest is "sha256:" and the hex of every file in Folder (names and
	// contents), "missing" when the script's folder is not there and
	// "unreadable" when it cannot be read. Present only when Covered.
	Digest string `json:"digest,omitempty"`
}

// scriptInterpreters are the commands whose first non-flag argument is the
// script they run.
var scriptInterpreters = map[string]bool{"sh": true, "bash": true, "zsh": true, "python": true, "python3": true, "node": true}

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
	fields = skipEnvWords(fields)
	// A leading env, with its options and NAME=value words, is a wrapper, not
	// the command.
	for len(fields) > 0 && filepath.Base(unquoteToken(fields[0])) == "env" {
		var ok bool
		if fields, ok = skipEnvOptions(fields[1:]); !ok {
			return "", false
		}
		fields = skipEnvWords(fields)
	}
	if len(fields) == 0 {
		return "", false
	}
	first := unquoteToken(fields[0])
	base := filepath.Base(first)
	if !scriptInterpreters[base] {
		return first, false
	}
	// An interpreter, with or without a path (/bin/bash), then its options
	// (bash -e, bash -o pipefail): the script is the first word after them.
	// An option that takes code or a module (-c, -m) names no file; one that
	// takes a value consumes it, so the value is never taken for the script.
	rest := fields[1:]
	for i := 0; i < len(rest); i++ {
		f := unquoteToken(rest[i])
		switch {
		case f == "--":
			if i+1 < len(rest) {
				return unquoteToken(rest[i+1]), true
			}
			return "", true
		case interpCodeFlags[base][f] || clusterIsCode(base, f):
			return "", true
		case interpValueFlags[base][f] || clusterTakesValue(base, f):
			i++
		case strings.HasPrefix(f, "-") || strings.HasPrefix(f, "+"):
		default:
			return f, true
		}
	}
	return "", true
}

// interpValueFlags are the options of each interpreter that take the next word
// as their value, and interpCodeFlags those that take code or a module to run
// instead of a file. Keyed by the interpreter's own name.
var (
	shValueFlags     = map[string]bool{"-o": true, "+o": true, "-O": true, "+O": true, "--rcfile": true, "--init-file": true}
	pyValueFlags     = map[string]bool{"-W": true, "-X": true}
	nodeValueFlags   = map[string]bool{"-r": true, "--require": true, "--import": true, "--loader": true, "--experimental-loader": true}
	interpValueFlags = map[string]map[string]bool{
		"sh": shValueFlags, "bash": shValueFlags, "zsh": shValueFlags,
		"python": pyValueFlags, "python3": pyValueFlags, "node": nodeValueFlags,
	}
	shCodeFlags     = map[string]bool{"-c": true}
	pyCodeFlags     = map[string]bool{"-c": true, "-m": true}
	nodeCodeFlags   = map[string]bool{"-e": true, "--eval": true, "-p": true, "--print": true}
	interpCodeFlags = map[string]map[string]bool{
		"sh": shCodeFlags, "bash": shCodeFlags, "zsh": shCodeFlags,
		"python": pyCodeFlags, "python3": pyCodeFlags, "node": nodeCodeFlags,
	}
)

// shCluster reports whether f is a cluster of single-letter options of a shell
// (-eu, -euo, +eo), and its letters.
func shCluster(base, f string) (string, bool) {
	if base != "sh" && base != "bash" && base != "zsh" {
		return "", false
	}
	if len(f) < 3 || (f[0] != '-' && f[0] != '+') || f[1] == '-' {
		return "", false
	}
	return f[1:], true
}

// clusterTakesValue reports whether a cluster of shell options ends in o or O,
// which then takes the next word as its value (bash -euo pipefail ./x).
func clusterTakesValue(base, f string) bool {
	letters, ok := shCluster(base, f)
	return ok && !strings.Contains(letters, "c") && (strings.HasSuffix(letters, "o") || strings.HasSuffix(letters, "O"))
}

// clusterIsCode reports whether a cluster of shell options carries -c (sh -ec
// "code"), which names code and not a file.
func clusterIsCode(base, f string) bool {
	letters, ok := shCluster(base, f)
	return ok && strings.Contains(letters, "c")
}

// skipEnvOptions drops env's own options (-i, -0, -v, -u NAME, -C DIR and
// their long forms), leaving the NAME=value words and the command. ok is
// false when an option hands env a command line to split (-S), which names no
// file we can read.
func skipEnvOptions(fields []string) ([]string, bool) {
	for len(fields) > 0 {
		f := unquoteToken(fields[0])
		switch {
		case f == "--":
			return fields[1:], true
		case f == "-S" || f == "--split-string" || strings.HasPrefix(f, "-S") || strings.HasPrefix(f, "--split-string="):
			return nil, false
		case f == "-u" || f == "-C" || f == "--unset" || f == "--chdir":
			if len(fields) < 2 {
				return nil, true
			}
			fields = fields[2:]
		case strings.HasPrefix(f, "-"):
			fields = fields[1:]
		case isEnvWord(f):
			// A NAME=value word may come between options, quoted value and
			// all, and a following option must still be seen.
			fields = skipOneEnvWord(fields)
		default:
			return fields, true
		}
	}
	return fields, true
}

// isEnvWord reports whether w is a NAME=value assignment.
func isEnvWord(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// skipEnvWords drops the NAME=value words a shell command starts with
// (CI=1 ./run.sh), including a quoted value that holds spaces, so the script
// word is the one that runs.
func skipEnvWords(fields []string) []string {
	for len(fields) > 0 && isEnvWord(fields[0]) {
		fields = skipOneEnvWord(fields)
	}
	return fields
}

// skipOneEnvWord drops the NAME=value word at the front of fields, with the
// rest of a quoted value that holds spaces (FOO="a b").
func skipOneEnvWord(fields []string) []string {
	_, v, _ := strings.Cut(fields[0], "=")
	i := 1
	if v != "" && (v[0] == '"' || v[0] == '\'') && (len(v) == 1 || v[len(v)-1] != v[0]) {
		for i < len(fields) && !strings.HasSuffix(fields[i-1], string(v[0])) {
			i++
		}
	}
	if i > len(fields) {
		i = len(fields)
	}
	return fields[i:]
}

// pluginRootVars are the spellings of the MCP placeholder for the declaring
// harness's directory.
var pluginRootVars = []string{plugin.MCPPlaceholderRoot + "/", `"` + plugin.MCPPlaceholderRoot + `"/`}

// harnessRelative reports the path inside the declaring harness that tok
// names, when it is "./x" (allowDot) or, for an MCP server only, ${PLUGIN_ROOT}/x (allowPluginRoot,
// the placeholder for the same directory) or "$YNH_HARNESS_DIR/x".
func harnessRelative(tok string, allowDot, allowPluginRoot bool) (string, bool) {
	if allowDot && strings.HasPrefix(tok, "./") {
		return tok[2:], true
	}
	if allowPluginRoot {
		for _, v := range pluginRootVars {
			if strings.HasPrefix(tok, v) {
				return tok[len(v):], true
			}
		}
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

// hashScript reads the folder of rel under dir and returns the covered
// reference. A path that climbs out of dir, or no known dir, is left uncovered
// rather than read.
//
// The digest is over every file under the script's folder, subfolders
// included, by path and contents, in sorted order; a .git folder is left out,
// since a fetch changes it without changing anything that runs.
//
// Only regular files inside the harness are read. A symlink is followed only
// to a regular file that is itself inside the harness; one that points
// outside, at a device or other non-regular file, that loops or dangles, is
// never opened and is listed in Skipped (and in the digest, so a change to it
// is noticed). A symlinked directory is not followed: its target text is
// digested. If the script itself is refused this way the script is not
// covered.
func hashScript(dir, rel, shown string) *scriptRef {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if dir == "" || !filepath.IsLocal(clean) {
		return &scriptRef{Path: shown}
	}
	folder := filepath.Dir(clean)
	ref := &scriptRef{Path: shown, Covered: true, Folder: filepath.ToSlash(folder)}
	realRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		ref.Digest = "missing"
		return ref
	}
	root := filepath.Join(dir, folder)
	realFolder, err := filepath.EvalSymlinks(root)
	if err != nil {
		ref.Digest = "missing"
		return ref
	}
	if fi, err := os.Stat(realFolder); err != nil || !fi.IsDir() {
		ref.Digest = "missing"
		return ref
	}
	if !within(realRoot, realFolder) {
		ref.Covered = false
		ref.Skipped = append(ref.Skipped, folder2(folder)+": the folder is a link that leads outside the harness")
		return ref
	}
	scriptName := filepath.Base(clean)
	h := sha256.New()
	field(h, "script")
	field(h, scriptName)
	// The walk goes through the resolved folder: a folder that is itself a
	// link to a folder inside the harness is read as that folder, and one that
	// leaves the harness was refused above.
	root = realFolder
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		r, _ := filepath.Rel(root, path)
		name := filepath.ToSlash(r)
		field(h, name)
		skip := func(reason string) {
			field(h, "skipped")
			field(h, reason)
			ref.Skipped = append(ref.Skipped, name+": "+reason)
			if r == scriptName {
				ref.Covered = false
			}
		}
		src := path
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				skip("a link that does not resolve (dangling or looping), not read")
				return nil
			}
			if !within(realRoot, real) {
				skip("a link that leads outside the harness, not read")
				return nil
			}
			fi, err := os.Stat(real)
			switch {
			case err != nil:
				skip("a link that cannot be read, not read")
				return nil
			case fi.IsDir():
				target, _ := os.Readlink(path)
				field(h, "dir-link")
				field(h, target)
				ref.Skipped = append(ref.Skipped, name+": a link to a folder, not followed")
				return nil
			case !fi.Mode().IsRegular():
				skip("a link to something that is not a regular file, not read")
				return nil
			}
			src = real
		case !d.Type().IsRegular():
			skip("not a regular file, not read")
			return nil
		}
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		fh := sha256.New()
		if _, err := io.Copy(fh, f); err != nil {
			return err
		}
		field(h, hex.EncodeToString(fh.Sum(nil)))
		ref.Files++
		return nil
	})
	if err != nil {
		// Never a digest of a part, which could equal the whole.
		ref.Digest = "unreadable"
		ref.Files = 0
		return ref
	}
	if ref.Files == 0 {
		// Nothing was hashed, so nothing is covered: never a digest of an
		// empty set.
		ref.Digest = "missing"
		return ref
	}
	ref.Digest = "sha256:" + hex.EncodeToString(h.Sum(nil))
	sort.Strings(ref.Skipped)
	return ref
}

func folder2(f string) string { return filepath.ToSlash(f) }

// within reports whether path is root or below it, both already resolved.
func within(root, path string) bool {
	r, err := filepath.Rel(root, path)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
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
	field(w, s.Folder)
	field(w, s.Digest)
}

// sensorScript is the script a sensor command runs. A sensor's command runs
// in the measured tree, so only $YNH_HARNESS_DIR names a file of the harness;
// "./x" is the tree's own.
func sensorScript(command, dir string) *scriptRef {
	tok, interp := scriptToken(strings.Fields(command))
	if rel, ok := harnessRelative(tok, false, false); ok {
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
	if rel, ok := harnessRelative(tok, true, false); ok {
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
	if rel, ok := harnessRelative(tok, true, true); ok {
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
		if len(s.Skipped) > 0 {
			_, _ = fmt.Fprintf(w, "    script:  %s (NOT covered: refused)\n", s.Path)
			writeSkipped(w, s)
			return
		}
		_, _ = fmt.Fprintf(w, "    script:  %s (not covered: project code, outside what this digest reads)\n", s.Path)
		return
	}
	if s.Digest == "missing" || s.Digest == "unreadable" {
		_, _ = fmt.Fprintf(w, "    script:  %s (covered, but its folder %s is %s)\n", s.Path, s.Folder, s.Digest)
		return
	}
	_, _ = fmt.Fprintf(w, "    script:  %s (covered with its folder %s: %d file(s), %s)\n", s.Path, s.Folder, s.Files, shortDigest(s.Digest))
	writeSkipped(w, s)
}

// writeSkipped says what the folder walk did not read.
func writeSkipped(w io.Writer, s *scriptRef) {
	for _, k := range s.Skipped {
		_, _ = fmt.Fprintf(w, "             skipped %s\n", k)
	}
}
