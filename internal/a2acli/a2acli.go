// Package a2acli is the shell route to a harness's A2A delegates: the official
// a2a CLI (github.com/a2aproject/a2a-cli) and its skill. ynh vendors the skill
// unchanged at a pinned commit, writes it with a short companion skill of its
// own into the parent's vendor skill folder when a run has A2A delegates, and
// lets doctor check the binary, the skill and the vendor settings the route
// needs (docs/delegate-sessions.md, "The a2a CLI route").
package a2acli

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The pin. Updating the vendored skill is a deliberate change: replace
// vendored/a2a-cli/SKILL.md, vendored/a2a-cli/UPSTREAM and these together.
const (
	// CLIVersion is the a2a CLI release the route is built and tested against.
	CLIVersion = "0.4.0"
	// SkillCommit is the commit of a2aproject/a2a-cli the skill was vendored from.
	SkillCommit = "5c95123d1c8d50a1cd6dfc2f28a14f5533a383ae"
	// SkillVersion is the skill's own metadata version at that commit.
	SkillVersion = "2026.09.22"
)

// Names of the two skills written into the vendor's skill folder.
const (
	SkillName     = "a2a-cli"
	CompanionName = "ynh-a2a-delegates"
)

//go:embed vendored/a2a-cli/SKILL.md vendored/a2a-cli/LICENSE vendored/a2a-cli/UPSTREAM companion.md
var files embed.FS

func mustRead(name string) []byte {
	b, err := files.ReadFile(name)
	if err != nil {
		panic(err) // embedded at build time
	}
	return b
}

// Skill is the vendored a2a-cli skill file, exactly as upstream has it.
func Skill() []byte { return mustRead("vendored/a2a-cli/SKILL.md") }

// License is the upstream licence, kept beside the vendored skill.
func License() []byte { return mustRead("vendored/a2a-cli/LICENSE") }

// reachPlaceholder is where companion.md takes its "how to reach one" section.
const reachPlaceholder = "{{REACH}}"

// reachGeneric is the section for a skill written before any endpoint is open
// (a baked image, or an install): it can name only the variables, not the
// addresses, which a session's endpoints do not have yet.
const reachGeneric = `## Which delegates, and how to reach one

- ` + "`YNH_DELEGATES`" + ` lists your delegates' names, comma-separated.
- Each one has ` + "`YNH_DELEGATE_<NAME>_CARD`" + ` (its agent card URL) and
  ` + "`YNH_DELEGATE_<NAME>_AUTH`" + ` (its credential). ` + "`<NAME>`" + ` is the delegate's name
  upper-cased, every character outside A-Z and 0-9 turned into ` + "`_`" + `.
- To reach one, prefix the command with its two variables:

  ` + "```bash" + `
  A2ACLI_AGENT_CARD="$YNH_DELEGATE_<NAME>_CARD" A2ACLI_AUTH="$YNH_DELEGATE_<NAME>_AUTH" a2a send --async "<the task>"
  ` + "```" + `

- With exactly one delegate, ` + "`A2ACLI_AGENT_CARD`" + ` and ` + "`A2ACLI_AUTH`" + ` are already
  set and the prefix is not needed. With several they are not set, so the CLI
  never picks one for you.
- Claude Code refuses a command with a variable in it under the allow rule
  ` + "`Bash(a2a:*)`" + `. A live ` + "`ynh run`" + ` writes this skill with each delegate's
  address instead, which needs no variable.
- Never print, echo or copy the credential itself. Pass the variable, not its
  value.
`

// Delegate is one open A2A delegate of a live session: its name and the card
// address the a2a CLI is given with -a.
type Delegate struct {
	Name string
	Card string
}

// reachLive is the section for a live session, whose endpoints are open: it
// lists each delegate's literal card address, so a command needs no variable
// (Claude Code's allow rule Bash(a2a:*) refuses a command that has one), and
// the credential comes from the credential helper ynh set, not the command.
func reachLive(ds []Delegate) string {
	var b strings.Builder
	b.WriteString("## Which delegates, and how to reach one\n\n")
	b.WriteString("Your delegates, and the address of each one's agent card:\n\n")
	b.WriteString("| Delegate | Card address |\n|---|---|\n")
	for _, d := range ds {
		fmt.Fprintf(&b, "| `%s` | `%s` |\n", d.Name, d.Card)
	}
	b.WriteString("\nTo reach one, give its address to `-a`, exactly as written above:\n\n")
	b.WriteString("```bash\na2a send -a <card address> --async \"<the task>\"\n```\n\n")
	b.WriteString("- Use the address, not a variable: a command with a `$VARIABLE` in it is refused\n")
	b.WriteString("  under the allow rule `Bash(a2a:*)`. Every command below takes the same `-a`.\n")
	b.WriteString("- The credential is not yours to pass: ynh set the a2a CLI's credential helper,\n")
	b.WriteString("  which gives each delegate its own. Do not pass `--auth`, and never print,\n")
	b.WriteString("  echo or copy a credential.\n")
	b.WriteString("- The addresses are this session's: they change on every run.\n\n")
	return b.String()
}

// Companion is ynh's own skill about reaching this parent's delegates, in its
// generic form (no endpoints open yet).
func Companion() []byte { return renderCompanion(reachGeneric) }

// CompanionFor is the companion skill for a live session: it lists each open
// delegate's card address.
func CompanionFor(ds []Delegate) []byte { return renderCompanion(reachLive(ds)) }

func renderCompanion(reach string) []byte {
	return []byte(strings.Replace(string(mustRead("companion.md")), reachPlaceholder, strings.TrimRight(reach, "\n"), 1))
}

// WriteCompanion rewrites the companion skill under skillsDir for a live
// session's delegates. WriteSkills has written the generic one before.
func WriteCompanion(skillsDir string, ds []Delegate) error {
	dir := filepath.Join(skillsDir, CompanionName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("writing the %s skill: %w", CompanionName, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), CompanionFor(ds), 0o644); err != nil {
		return fmt.Errorf("writing the %s skill: %w", CompanionName, err)
	}
	return nil
}

// CredentialHelperEnv is the a2a CLI setting that names a credential helper.
const CredentialHelperEnv = "A2ACLI_CREDENTIAL_HELPER"

// HelperMarker is in the name of every helper ynh writes, so a run can tell its
// own helper, inherited from an enclosing session, from the user's.
const HelperMarker = "ynh-a2a-credentials"

// Credential is one endpoint's bearer token, keyed by its card address.
type Credential struct {
	Card  string // http://127.0.0.1:<port>
	Token string
}

// WriteCredentialHelper writes the credential helper for the a2a CLI into a
// new private folder and returns its path and a function that removes it. The
// helper follows the Bazel credential helper protocol the CLI speaks: it is run
// as `<helper> get` with {"uri": "<agent URL>"} on stdin, and answers with the
// headers for that endpoint, or {} for any other. Each delegate has its own
// endpoint, so each gets its own token with no variable in the parent's command.
// The file holds the tokens: 0700 in a 0700 folder, like the MCP config.
func WriteCredentialHelper(creds []Credential) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", HelperMarker+"-")
	if err != nil {
		return "", nil, fmt.Errorf("writing the a2a credential helper: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	var b strings.Builder
	b.WriteString("#!/bin/sh\n# Written by ynh for this session: the a2a CLI's credential helper.\n")
	b.WriteString("[ \"$1\" = get ] || exit 1\nin=$(cat)\ncase \"$in\" in\n")
	for _, c := range creds {
		fmt.Fprintf(&b, "  *'\"%s\"'*|*'\"%s/'*) printf '{\"headers\":{\"Authorization\":[\"Bearer %%s\"]}}\\n' '%s'; exit 0 ;;\n", c.Card, c.Card, c.Token)
	}
	b.WriteString("esac\nprintf '{}\\n'\n")
	path = filepath.Join(dir, "helper")
	if err := os.WriteFile(path, []byte(b.String()), 0o700); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("writing the a2a credential helper: %w", err)
	}
	return path, cleanup, nil
}

// WriteSkills writes the two skills under skillsDir, the vendor's skill folder
// (<run dir>/<config dir>/skills).
func WriteSkills(skillsDir string) error {
	set := []struct {
		dir, name string
		data      []byte
	}{
		{SkillName, "SKILL.md", Skill()},
		{SkillName, "LICENSE", License()},
		{CompanionName, "SKILL.md", Companion()},
	}
	for _, f := range set {
		dir := filepath.Join(skillsDir, f.dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("writing the %s skill: %w", f.dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o644); err != nil {
			return fmt.Errorf("writing the %s skill: %w", f.dir, err)
		}
	}
	return nil
}

// SkillState is what a vendor's skill folder holds of the vendored skill.
type SkillState int

const (
	SkillMissing SkillState = iota
	SkillDiffers
	SkillCurrent
)

// CheckSkill compares the a2a-cli skill in skillsDir with the pin.
func CheckSkill(skillsDir string) SkillState {
	got, err := os.ReadFile(filepath.Join(skillsDir, SkillName, "SKILL.md"))
	if err != nil {
		return SkillMissing
	}
	if !bytes.Equal(got, Skill()) {
		return SkillDiffers
	}
	return SkillCurrent
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// ParseVersion pulls the first major.minor.patch out of `a2a version` output.
func ParseVersion(out string) (string, bool) {
	v := versionRe.FindString(out)
	return v, v != ""
}

// Older reports whether version a is older than b, both major.minor.patch.
func Older(a, b string) bool {
	pa, pb := split(a), split(b)
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}

func split(v string) [3]int {
	var out [3]int
	for i, s := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(s)
	}
	return out
}

// Binary reports the a2a CLI on PATH: whether it is there and what version it
// says. A binary that does not report a version is found with version "".
func Binary() (found bool, version string) {
	path, err := exec.LookPath("a2a")
	if err != nil {
		return false, ""
	}
	out, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return true, ""
	}
	v, _ := ParseVersion(string(out))
	return true, v
}
